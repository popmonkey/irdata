package irdata

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/gob"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

const loginURL = "https://members-ng.iracing.com/auth"
const testUrl = "https://members-ng.iracing.com/data/constants/event_types"

type authDataT struct {
	Username        string
	EncodedPassword string
}

var additionalContext = []byte("irdata.auth")

// AuthWithCredsFromFile loads the username and password from a file
// at authFilename and encrypted with the key in keyFilename.
func (i *Irdata) AuthWithCredsFromFile(keyFilename string, authFilename string) error {
	authData, err := readCreds(keyFilename, authFilename)
	if err != nil {
		return err
	}

	return i.auth(authData)
}

// AuthWithProvideCreds calls the provided function for the username and password
func (i *Irdata) AuthWithProvideCreds(authSource CredsProvider) error {
	log.WithFields(log.Fields{"authSource": authSource}).Debug("Calling CredsProvider")

	username, password, err := authSource.GetCreds()
	if err != nil {
		return err
	}

	var authData authDataT

	authData.Username = string(username)
	authData.EncodedPassword, err = encodePassword(username, password)
	if err != nil {
		return err
	}

	return i.auth(authData)
}

// SaveProvidedCredsToFile calls the provided function for the
// username and password and then saves these credentials to authFilename
// using the key within the keyFilename
//
// This function will panic out on errors
func SaveProvidedCredsToFile(keyFilename string, authFilename string, authSource CredsProvider) error {
	log.WithFields(log.Fields{"authSource": authSource}).Debug("Calling CredsProvider")

	username, password, err := authSource.GetCreds()
	if err != nil {
		return err
	}

	var authData authDataT

	authData.Username = string(username)
	authData.EncodedPassword, err = encodePassword(username, password)
	if err != nil {
		return err
	}

	return writeCreds(keyFilename, authFilename, authData)
}

func writeCreds(keyFilename string, authFilename string, authData authDataT) error {
	key, err := getKey(keyFilename)
	if err != nil {
		return err
	}

	block, err := aes.NewCipher(key)

	// not a defer because we want to do this right away
	shred(&key)

	if err != nil {
		if errors.Is(err, aes.KeySizeError(0)) {
			return makeErrorf("key must be 16, 24, or 32 bytes long")
		} else {
			return makeErrorf("unable to intialize AES cipher [%v]", err)
		}
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return makeErrorf("unable to initialice GCM [%v]", err)
	}

	nonce, err := makeNonce(aesgcm)
	if err != nil {
		return err
	}

	buf := bytes.Buffer{}

	enc := gob.NewEncoder(&buf)

	err = enc.Encode(authData)
	if err != nil {
		return makeErrorf("uanble to gob encode auth data %v", err)
	}

	data := aesgcm.Seal(nonce, nonce, buf.Bytes(), additionalContext)

	base64data := base64.StdEncoding.Strict().EncodeToString(data)

	if err := os.WriteFile(authFilename, []byte(base64data), os.ModePerm); err != nil {
		return makeErrorf("unable to write %s [%v]", authFilename, err)
	}

	return nil
}

func readCreds(keyFilename string, authFilename string) (authDataT, error) {
	var authData authDataT

	key, err := getKey(keyFilename)
	if err != nil {
		return authData, err
	}

	block, err := aes.NewCipher(key)

	// not a defer because we want to do this right away
	shred(&key)

	if err != nil {
		if errors.Is(err, aes.KeySizeError(0)) {
			return authData, makeErrorf("key must be 16, 24, or 32 bytes long")
		} else {
			return authData, makeErrorf("unable to intialize AES cipher [%v]", err)
		}
	}

	aesgcm, err := cipher.NewGCM(block)

	if err != nil {
		return authData, makeErrorf("unable to initialice GCM [%v]", err)
	}

	base64data, err := os.ReadFile(authFilename)
	if err != nil {
		return authData, makeErrorf("unable to read file %s [%v]", authFilename, err)
	}

	data, err := base64.StdEncoding.Strict().DecodeString(string(base64data))
	if err != nil {
		return authData, makeErrorf("unable to decode base64 creds [%v]", err)
	}

	authGob, err := aesgcm.Open(nil, data[:aesgcm.NonceSize()], data[aesgcm.NonceSize():], additionalContext)
	if err != nil {
		return authData, makeErrorf("unable to open aesgcm [%v]", err)
	}

	buf := bytes.NewReader(authGob)

	dec := gob.NewDecoder(buf)

	err = dec.Decode(&authData)
	if err != nil {
		return authData, makeErrorf("unable to gob decode [%v]", err)
	}

	return authData, nil
}

// auth client
func (i *Irdata) auth(authData authDataT) error {
	if i.isAuthed {
		return nil
	}

	if authData.EncodedPassword == "" {
		return makeErrorf("must provide credentials before calling")
	}

	log.Info("Authenticating")

	retries := 5

	var err error
	var resp *http.Response

	for retries > 0 {
		resp, err = i.httpClient.Post(loginURL, "application/json",
			strings.NewReader(
				fmt.Sprintf("{\"email\": \"%s\" ,\"password\": \"%s\"}", authData.Username, authData.EncodedPassword),
			),
		)

		if resp.StatusCode < 500 {
			break
		}

		retries--

		backoff := time.Duration((6-retries)*5) * time.Second

		log.WithFields(log.Fields{"resp.StatusCode": resp.StatusCode, "backoff": backoff}).Warn(" *** Retrying Authentication due to error")

		time.Sleep(backoff)
	}

	if err != nil {
		return makeErrorf("post to login failed %v", err)
	}

	if resp.StatusCode != 200 {
		log.WithFields(log.Fields{
			"resp.Status":     resp.Status,
			"resp.StatusCode": resp.StatusCode,
		}).Warn("Failed to authenticate")

		return makeErrorf("unexpected auth failure [%v]", resp.Status)
	}

	// test we are really auth'ed
	resp, err = i.retryingGet(testUrl)
	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		if resp.StatusCode == 401 {
			return makeErrorf("login failed, check creds")
		} else {
			log.WithFields(log.Fields{
				"resp.Status":     resp.Status,
				"resp.StatusCode": resp.StatusCode,
				"testUrl":         testUrl,
			}).Warn("Unexpected status")

			return makeErrorf("unexpected auth failure %v", resp.Status)
		}
	}

	log.Info("Login succeeded")

	i.isAuthed = true

	return nil
}

// See: https://forums.iracing.com/discussion/22109/login-form-changes/p1
func encodePassword(username []byte, password []byte) (string, error) {
	hasher := sha256.New()

	_, err := hasher.Write(password)
	if err != nil {
		return "", makeErrorf("unable to hash password to sha256 [%v]", err)
	}

	_, err = hasher.Write([]byte(strings.ToLower(string(username))))
	if err != nil {
		return "", makeErrorf("unable to hash username to sha256 [%v]", err)
	}

	return base64.StdEncoding.Strict().EncodeToString(hasher.Sum(nil)), nil
}

// nonce generator
func makeNonce(gcm cipher.AEAD) ([]byte, error) {
	nonce := make([]byte, gcm.NonceSize())

	_, err := rand.Read(nonce)

	return nonce, err
}

// read secret key
func getKey(keyFilename string) ([]byte, error) {
	stat, err := os.Stat(keyFilename)
	if err != nil {
		return nil, makeErrorf("unable to stat %s [%v]", keyFilename, err)
	}

	if (stat.Mode() & os.ModePerm) != 0400 {
		return nil, makeErrorf("key file %v must have perms set to 0400", keyFilename)
	}

	content, err := os.ReadFile(keyFilename)
	if err != nil {
		return nil, makeErrorf("unable to read %s [%v]", keyFilename, err)
	}

	key, err := base64.StdEncoding.Strict().DecodeString(string(content))
	if err != nil {
		return nil, makeErrorf("unabled to base64 decode %s [%v]", keyFilename, err)
	}

	return key, nil
}

func shred(key *[]byte) {
	for i := range *key {
		(*key)[i] = 0x69
	}
}
