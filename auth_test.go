package irdata

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

var testUsername, testPassword = []byte("louis"), []byte("ferrari")

var testCredsFilename = filepath.Join("testdata", "test.creds")
var testKeyFilename = filepath.Join("testdata", "test.key")

var testAuthDir = filepath.Join(os.TempDir(), "irdata-auth")

type testCreds struct{}

func (testCreds) GetCreds() ([]byte, []byte) {
	return testUsername, testPassword
}

func setupAuthTest() {
	os.Mkdir(testAuthDir, 0777)
}

func cleanupAuthTest() {
	os.RemoveAll(testAuthDir)
}

func TestNonce(t *testing.T) {
	key, err := getKey(testKeyFilename)

	assert.NoError(t, err)

	block, err := aes.NewCipher(key)

	assert.NoError(t, err)

	gcm, err := cipher.NewGCM(block)

	assert.NoError(t, err)

	nonce1, err := makeNonce(gcm)

	assert.NoError(t, err)

	// size
	assert.Equal(t, len(nonce1), gcm.NonceSize())

	nonce2, err := makeNonce(gcm)

	assert.NoError(t, err)

	// is random
	assert.False(t, bytes.Equal(nonce1, nonce2))
}

func TestEncodePassword(t *testing.T) {
	encodedPasswordExpected := "nKb060s95vcF0RpjfkGKapQG1o0AgbaPz10/H6QpHn4="

	encodedPasswordActual, err := encodePassword(testUsername, testPassword)

	assert.NoError(t, err)

	// verify it can be decoded
	_, err = base64.StdEncoding.Strict().DecodeString(encodedPasswordActual)

	assert.NoError(t, err)
	assert.Equal(t, encodedPasswordExpected, encodedPasswordActual)
}

func TestShredKey(t *testing.T) {
	expectedKey := []byte{0, 1, 2, 3, 4, 5, 6, 7}

	testKey := make([]byte, len(expectedKey))

	copy(testKey, expectedKey)

	for i := 0; i < 8; i++ {
		assert.Equal(t, expectedKey[i], testKey[i])
	}

	shred(&testKey)

	// make sure the entire key was shredded
	for i := 0; i < 8; i++ {
		assert.NotEqual(t, expectedKey[i], testKey[i])
	}

}

func TestGetCreds(t *testing.T) {
	auth, err := readCreds(testKeyFilename, testCredsFilename)

	assert.NoError(t, err)

	assert.Equal(t, string(testUsername), auth.Username)

	encodedPassword, err := encodePassword(testUsername, testPassword)

	assert.NoError(t, err)

	assert.Equal(t, encodedPassword, auth.EncodedPassword)
}

func TestWriteCreds(t *testing.T) {
	setupAuthTest()

	// use the CredsProvider interface just to make sure it is properly defined
	var creds testCreds
	username, password := creds.GetCreds()

	encodedPassword, err := encodePassword(username, password)

	assert.NoError(t, err)

	authDataExpected := &authDataT{
		Username:        string(username),
		EncodedPassword: encodedPassword,
	}

	t.Cleanup(cleanupAuthTest)

	credsFn := filepath.Join(testAuthDir, "test.creds")

	writeCreds(testKeyFilename, credsFn, *authDataExpected)

	authDataActual, err := readCreds(testKeyFilename, credsFn)

	assert.NoError(t, err)

	assert.Equal(t, authDataExpected.Username, authDataActual.Username)
	assert.Equal(t, authDataExpected.EncodedPassword, authDataActual.EncodedPassword)
}
