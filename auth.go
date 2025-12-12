package irdata

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

var TokenURL = "https://oauth.iracing.com/oauth2/token"

type authDataT struct {
	Username       string
	MaskedPassword string
	ClientID       string
	ClientSecret   string
}

type AuthTokenT struct {
	AccessToken  string
	RefreshToken string
	TokenExpiry  time.Time
	ClientID     string
	ClientSecret string
}

// TokenResponse maps the JSON response from the /token endpoint
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

var additionalContext = []byte("irdata.auth")

// AuthWithCredsFromFile loads the username and password from a file
// at authFilename and encrypted with the key in keyFilename.
func (i *Irdata) AuthWithCredsFromFile(keyFilename string, authFilename string) error {
	authData, err := readCreds(keyFilename, authFilename)
	if err != nil {
		return err
	}

	return i.auth(authData, keyFilename)
}

// AuthWithProvideCreds calls the provided function for the credentials
func (i *Irdata) AuthWithProvideCreds(authSource CredsProvider) error {
	log.WithFields(log.Fields{"authSource": authSource}).Debug("Calling CredsProvider")

	username, password, clientID, clientSecret, err := authSource.GetCreds()
	if err != nil {
		return err
	}

	var authData authDataT
	authData.Username = string(username)
	authData.ClientID = string(clientID)
	authData.ClientSecret = string(clientSecret)

	// Mask the password if it is not already masked
	pass := string(password)
	if isMasked(pass) {
		authData.MaskedPassword = pass
	} else {
		authData.MaskedPassword, err = maskSecret(pass, string(username))
		if err != nil {
			return err
		}
	}

	return i.auth(authData, "")
}

// AuthAndSaveProvidedCredsToFile calls the provided function for the
// credentials, verifies auth, and then saves them to authFilename using the key in keyFilename
func (i *Irdata) AuthAndSaveProvidedCredsToFile(keyFilename string, authFilename string, authSource CredsProvider) error {
	log.WithFields(log.Fields{"authSource": authSource}).Debug("Calling CredsProvider")

	// check that the keyfile exists before collecting creds
	_, err := getKey(keyFilename)
	if err != nil {
		return err
	}

	username, password, clientID, clientSecret, err := authSource.GetCreds()
	if err != nil {
		return err
	}

	var authData authDataT
	authData.Username = string(username)
	authData.ClientID = string(clientID)
	authData.ClientSecret = string(clientSecret)

	// Mask the password if it is not already masked
	pass := string(password)
	if isMasked(pass) {
		authData.MaskedPassword = pass
	} else {
		authData.MaskedPassword, err = maskSecret(pass, string(username))
		if err != nil {
			return err
		}
	}

	// Authenticate first to verify creds are good
	err = i.auth(authData, keyFilename)
	if err != nil {
		return err
	}

	// Save to disk if auth succeeded
	return writeCreds(keyFilename, authFilename, authData)
}

func writeCreds(keyFilename string, authFilename string, authData authDataT) error {
	return encryptToFile(keyFilename, authFilename, authData)
}

func readCreds(keyFilename string, authFilename string) (authDataT, error) {
	var authData authDataT
	err := decryptFromFile(keyFilename, authFilename, &authData)
	if err != nil {
		return authData, err
	}

	// Detect legacy credentials (missing ClientID or Secret)
	// Because gob ignores unknown fields, decoding an old file into the new struct
	// will leave these fields empty.
	if authData.ClientID == "" || authData.ClientSecret == "" {
		return authData, makeErrorf("credentials file '%s' is in a legacy format (missing Client ID/Secret). Please delete it and re-authenticate.", authFilename)
	}

	return authData, nil
}

func (i *Irdata) writeAuthToken(keyFilename string) error {
	if i.authTokenFile == "" || keyFilename == "" {
		return nil // Not configured to save auth token
	}

	token := AuthTokenT{
		AccessToken:  i.AccessToken,
		RefreshToken: i.RefreshToken,
		TokenExpiry:  i.TokenExpiry,
		ClientID:     i.ClientID,
		ClientSecret: i.ClientSecret,
	}

	return encryptToFile(keyFilename, i.authTokenFile, token)
}

func (i *Irdata) readAuthToken(keyFilename string) error {
	if i.authTokenFile == "" || keyFilename == "" {
		return makeErrorf("no auth token file configured or no key provided")
	}

	var token AuthTokenT
	err := decryptFromFile(keyFilename, i.authTokenFile, &token)
	if err != nil {
		return err
	}

	i.AccessToken = token.AccessToken
	i.RefreshToken = token.RefreshToken
	i.TokenExpiry = token.TokenExpiry
	i.ClientID = token.ClientID
	i.ClientSecret = token.ClientSecret

	return nil
}

func encryptToFile(keyFilename string, filename string, payload interface{}) error {
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

	err = enc.Encode(payload)
	if err != nil {
		return makeErrorf("unable to gob encode payload %v", err)
	}

	data := aesgcm.Seal(nonce, nonce, buf.Bytes(), additionalContext)

	base64data := base64.StdEncoding.Strict().EncodeToString(data)

	if err := os.WriteFile(filename, []byte(base64data), os.ModePerm); err != nil {
		return makeErrorf("unable to write %s [%v]", filename, err)
	}

	return nil
}

func decryptFromFile(keyFilename string, filename string, payload interface{}) error {
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

	base64data, err := os.ReadFile(filename)
	if err != nil {
		return err // Return original error for file not found etc.
	}

	data, err := base64.StdEncoding.Strict().DecodeString(string(base64data))
	if err != nil {
		return makeErrorf("unable to decode base64 file [%v]", err)
	}

	if len(data) < aesgcm.NonceSize() {
		return makeErrorf("malformed ciphertext")
	}

	decryptedGob, err := aesgcm.Open(nil, data[:aesgcm.NonceSize()], data[aesgcm.NonceSize():], additionalContext)
	if err != nil {
		return makeErrorf("unable to open aesgcm [%v]", err)
	}

	buf := bytes.NewReader(decryptedGob)

	dec := gob.NewDecoder(buf)

	err = dec.Decode(payload)
	if err != nil {
		return makeErrorf("unable to gob decode [%v]", err)
	}

	return nil
}

// auth client using Password Limited Flow
func (i *Irdata) auth(authData authDataT, keyFilename string) error {
	if i.isAuthed {
		return nil
	}

	// Try loading from token file if available and configured
	if i.authTokenFile != "" && keyFilename != "" {
		if err := i.readAuthToken(keyFilename); err == nil {
			log.Info("Loaded auth token from file")
			// Validate/Refresh
			if i.TokenExpiry.Before(time.Now()) {
				log.Info("Loaded token is expired, refreshing")
				if err := i.refreshToken(); err == nil {
					i.isAuthed = true
					log.Debug("Refreshing token successful, writing new token to file.")
					_ = i.writeAuthToken(keyFilename) // Ignore error on write, token is already valid in memory
					return nil
				} else {
					log.Warn("Failed to refresh loaded token, falling back to password auth", err)
				}
			} else {
				i.isAuthed = true
				return nil
			}
		} else {
			log.WithField("err", err).Debug("Failed to load auth token, proceeding with password auth")
		}
	}

	if authData.MaskedPassword == "" || authData.ClientID == "" || authData.ClientSecret == "" {
		return makeErrorf("missing credentials (password, client_id, or client_secret)")
	}

	log.Info("Authenticating via OAuth2 Password Limited Flow")

	// Store credentials in struct for future refreshes
	i.ClientID = authData.ClientID
	i.ClientSecret = authData.ClientSecret

	// Mask the client secret for transmission
	maskedClientSecret, err := maskSecret(authData.ClientSecret, authData.ClientID)
	if err != nil {
		return makeErrorf("failed to mask client secret: %v", err)
	}

	// Build Form Data
	formData := url.Values{}
	formData.Set("grant_type", "password_limited")
	formData.Set("client_id", authData.ClientID)
	formData.Set("client_secret", maskedClientSecret)
	formData.Set("username", authData.Username)
	formData.Set("password", authData.MaskedPassword)
	// Request the specific scope required for the API
	formData.Set("scope", "iracing.auth")

	retries := 5
	var resp *http.Response

	for retries > 0 {
		resp, err = i.httpClient.PostForm(TokenURL, formData)

		// 429 Too Many Requests or 5xx Server Errors -> Retry
		// 400/401 -> Do not retry, usually config error
		if err == nil && resp.StatusCode < 500 && resp.StatusCode != 429 {
			break
		}

		retries--
		backoff := time.Duration((6-retries)*5) * time.Second
		status := "error"
		if resp != nil {
			status = resp.Status
		}
		log.WithFields(log.Fields{"status": status, "backoff": backoff}).Warn(" *** Retrying Authentication")

		time.Sleep(backoff)
	}

	if err != nil {
		return makeErrorf("post to token endpoint failed %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.WithFields(log.Fields{
			"resp.Status":     resp.Status,
			"resp.StatusCode": resp.StatusCode,
		}).Warn("Failed to authenticate")

		return makeErrorf("unexpected auth failure [%v]", resp.Status)
	}

	// Parse the JSON response
	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return makeErrorf("failed to decode token response: %v", err)
	}

	log.WithFields(log.Fields{"scope": tokenResp.Scope}).Info("Login succeeded")

	// Store the token and refresh info
	i.AccessToken = tokenResp.AccessToken
	i.RefreshToken = tokenResp.RefreshToken
	i.TokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	i.isAuthed = true

	// If auth was successful and authTokenFile is configured, write the new token
	if i.authTokenFile != "" && keyFilename != "" {
		log.Debug("Initial auth successful, writing token to file.")
		_ = i.writeAuthToken(keyFilename) // Ignore error on write, auth is already successful
	}

	return nil
}

// refreshToken attempts to use the stored Refresh Token to get a new Access Token
func (i *Irdata) refreshToken() error {
	if i.RefreshToken == "" {
		return makeErrorf("no refresh token available")
	}

	log.Info("Refreshing Access Token")

	// Mask the client secret
	maskedClientSecret, err := maskSecret(i.ClientSecret, i.ClientID)
	if err != nil {
		return makeErrorf("failed to mask client secret: %v", err)
	}

	// Build Form Data for Refresh
	formData := url.Values{}
	formData.Set("grant_type", "refresh_token")
	formData.Set("client_id", i.ClientID)
	formData.Set("client_secret", maskedClientSecret)
	formData.Set("refresh_token", i.RefreshToken)

	resp, err := i.httpClient.PostForm(TokenURL, formData)
	if err != nil {
		return makeErrorf("refresh request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		// If refresh fails, we might be totally de-authed
		log.Warnf("Refresh failed with status: %s", resp.Status)
		return makeErrorf("refresh failed [%v]", resp.Status)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return makeErrorf("failed to decode refresh response: %v", err)
	}

	// Update state
	i.AccessToken = tokenResp.AccessToken
	i.TokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	// iRacing *may* issue a new refresh token. If they do, update it.
	// If they don't, keep the old one (if that's their policy), or it might be single-use.
	if tokenResp.RefreshToken != "" {
		i.RefreshToken = tokenResp.RefreshToken
	}

	log.Info("Token Refresh Successful")
	return nil
}

// isMasked checks if a secret is already masked.
// It does this by checking if the secret is a valid base64 encoded
// string that decodes to a sha256 hash.
func isMasked(secret string) bool {
	decoded, err := base64.StdEncoding.Strict().DecodeString(secret)
	if err != nil {
		return false
	}
	return len(decoded) == sha256.Size
}

// See: https://forums.iracing.com/discussion/22109/login-form-changes/p1
func maskSecret(secret string, id string) (string, error) {
	hasher := sha256.New()

	// "The normalized id is concatenated onto the secret with no separator."
	_, err := hasher.Write([]byte(secret))
	if err != nil {
		return "", makeErrorf("unable to hash secret [%v]", err)
	}

	_, err = hasher.Write([]byte(strings.ToLower(id)))
	if err != nil {
		return "", makeErrorf("unable to hash id [%v]", err)
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
