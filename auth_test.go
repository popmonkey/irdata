package irdata

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var testUsername, testPassword = []byte("louis@ferrari.com"), []byte("red4life")
var testClientID, testClientSecret = []byte("ferrari"), []byte("we-are-faster")

var testCredsFilename = filepath.Join("testdata", "test.creds")
var testKeyFilename = filepath.Join("testdata", "test.key")

var testAuthDir = filepath.Join(os.TempDir(), "irdata-auth")

type testCreds struct{}

func (testCreds) GetCreds() ([]byte, []byte, []byte, []byte, error) {
	return testUsername, testPassword, testClientID, testClientSecret, nil
}

func setupAuthTest() {
	os.Mkdir(testAuthDir, 0777)
	TokenURL = "https://oauth.iracing.com/oauth2/token" // Reset for each test
}

func cleanupAuthTest() {
	os.RemoveAll(testAuthDir)
	os.Remove(filepath.Join(testAuthDir, "test.token")) // Clean up auth token file
	TokenURL = "https://oauth.iracing.com/oauth2/token" // Reset global TokenURL
}

type mockAuthResponse struct {
	accessToken  string
	tokenType    string
	expiresIn    int
	refreshToken string
	scope        string
}

func startMockAuthServer(t *testing.T, resp mockAuthResponse, statusCode int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/oauth2/token", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))

		err := r.ParseForm()
		assert.NoError(t, err)

		// Basic validation of client_id/secret and username/password for initial auth
		if r.Form.Get("grant_type") == "password_limited" {
			assert.Equal(t, string(testClientID), r.Form.Get("client_id"))
			assert.True(t, isMasked(r.Form.Get("client_secret"))) // Should be masked
			assert.Equal(t, string(testUsername), r.Form.Get("username"))
			assert.True(t, isMasked(r.Form.Get("password"))) // Should be masked
			assert.Equal(t, "iracing.auth", r.Form.Get("scope"))
		} else if r.Form.Get("grant_type") == "refresh_token" {
			assert.Equal(t, string(testClientID), r.Form.Get("client_id"))
			assert.True(t, isMasked(r.Form.Get("client_secret"))) // Should be masked
			assert.True(t, strings.HasPrefix(r.Form.Get("refresh_token"), "initial_refresh") || strings.HasPrefix(r.Form.Get("refresh_token"), "new_refresh"))
		} else {
			t.Errorf("Unexpected grant_type: %s", r.Form.Get("grant_type"))
		}

		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(TokenResponse{
			AccessToken:  resp.accessToken,
			TokenType:    resp.tokenType,
			ExpiresIn:    resp.expiresIn,
			RefreshToken: resp.refreshToken,
			Scope:        resp.scope,
		})
	}))
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

func TestMaskSecret(t *testing.T) {
	encodedPasswordExpected := "K3j4XfB2QoZYXLJH9eo5ZY2aC2fJ16MVwt7DD2uZJtw="

	maskedPasswordActual, err := maskSecret(string(testPassword), string(testUsername))

	assert.NoError(t, err)

	// verify it can be decoded
	_, err = base64.StdEncoding.Strict().DecodeString(maskedPasswordActual)

	assert.NoError(t, err)
	assert.Equal(t, encodedPasswordExpected, maskedPasswordActual)
}

func TestIsMasked(t *testing.T) {
	maskedPassword, err := maskSecret(string(testPassword), string(testUsername))
	assert.NoError(t, err)

	assert.True(t, isMasked(maskedPassword))
	assert.False(t, isMasked(string(testPassword)))
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
	assert.Equal(t, string(testClientID), auth.ClientID)
	assert.Equal(t, string(testClientSecret), auth.ClientSecret)

	maskedPassword, err := maskSecret(string(testPassword), string(testUsername))

	assert.NoError(t, err)

	assert.Equal(t, maskedPassword, auth.MaskedPassword)
}

func TestWriteCreds(t *testing.T) {
	setupAuthTest()

	// use the CredsProvider interface just to make sure it is properly defined
	var creds testCreds
	username, password, clientID, clientSecret, _ := creds.GetCreds()

	maskedPassword, err := maskSecret(string(password), string(username))
	assert.NoError(t, err)

	authDataExpected := &authDataT{
		Username:       string(username),
		MaskedPassword: maskedPassword,
		ClientID:       string(clientID),
		ClientSecret:   string(clientSecret),
	}

	t.Cleanup(cleanupAuthTest)

	credsFn := filepath.Join(testAuthDir, "test.creds")

	writeCreds(testKeyFilename, credsFn, *authDataExpected)

	authDataActual, err := readCreds(testKeyFilename, credsFn)

	assert.NoError(t, err)

	assert.Equal(t, authDataExpected.Username, authDataActual.Username)
	assert.Equal(t, authDataExpected.MaskedPassword, authDataActual.MaskedPassword)
	assert.Equal(t, authDataExpected.ClientID, authDataActual.ClientID)
	assert.Equal(t, authDataExpected.ClientSecret, authDataActual.ClientSecret)
}

func TestAuthTokenFile(t *testing.T) {
	setupAuthTest()
	t.Cleanup(cleanupAuthTest)

	api := Open(nil)
	defer api.Close()

	// 1. Authenticate and ensure a token is saved
	credsFn := filepath.Join(testAuthDir, "test.creds")
	authTokenFn := filepath.Join(testAuthDir, "test.token")

	// Create a dummy creds file for the initial auth (it won't be used if token is loaded)
	maskedPassword, err := maskSecret(string(testPassword), string(testUsername))
	assert.NoError(t, err)
	authData := authDataT{
		Username:       string(testUsername),
		MaskedPassword: maskedPassword,
		ClientID:       string(testClientID),
		ClientSecret:   string(testClientSecret),
	}
	err = writeCreds(testKeyFilename, credsFn, authData)
	assert.NoError(t, err)

	api.SetAuthTokenFile(authTokenFn)

	// Mock the token endpoint response
	ts := startMockAuthServer(t, mockAuthResponse{
		accessToken:  "initial_access",
		refreshToken: "initial_refresh",
		expiresIn:    3600, // 1 hour
	}, 200)
	defer ts.Close()
	TokenURL = ts.URL + "/oauth2/token"

	err = api.AuthWithCredsFromFile(testKeyFilename, credsFn)
	assert.NoError(t, err)
	assert.True(t, api.isAuthed)
	assert.Equal(t, "initial_access", api.AccessToken)

	// Verify token file was written
	_, err = os.Stat(authTokenFn)
	assert.NoError(t, err)

	// 2. Create a new API instance and load from token file
	api2 := Open(nil)
	defer api2.Close()
	api2.SetAuthTokenFile(authTokenFn)

	// Mock server will return refresh token this time
	tsRefresh := startMockAuthServer(t, mockAuthResponse{
		accessToken:  "refreshed_access",
		refreshToken: "new_refresh", // Simulate new refresh token
		expiresIn:    7200,          // 2 hours
	}, 200)
	defer tsRefresh.Close()
	TokenURL = tsRefresh.URL + "/oauth2/token"

	// Simulate expired token in file
	var savedToken AuthTokenT
	decryptFromFile(testKeyFilename, authTokenFn, &savedToken)
	savedToken.TokenExpiry = savedToken.TokenExpiry.Add(-2 * time.Hour) // Make it expired
	encryptToFile(testKeyFilename, authTokenFn, savedToken)

	err = api2.AuthWithCredsFromFile(testKeyFilename, credsFn)
	assert.NoError(t, err)
	assert.True(t, api2.isAuthed)
	assert.Equal(t, "refreshed_access", api2.AccessToken)
	assert.Equal(t, "new_refresh", api2.RefreshToken) // Check if refresh token was updated

	// Verify token file was updated with refreshed token
	var updatedToken AuthTokenT
	decryptFromFile(testKeyFilename, authTokenFn, &updatedToken)
	assert.Equal(t, "refreshed_access", updatedToken.AccessToken)
	assert.Equal(t, "new_refresh", updatedToken.RefreshToken)

	// 3. Test with no key provided (should not honor authTokenFile)
	api3 := Open(nil)
	defer api3.Close()
	api3.SetAuthTokenFile(authTokenFn) // Set authTokenFile but no key

	// This should fail because the original credsFn is not valid for direct auth
	// (it's a dummy file, and we don't have the real password)
	// and no key for the token file.
	err = api3.AuthWithCredsFromFile("", credsFn) // No keyFilename
	assert.Error(t, err)
	assert.False(t, api3.isAuthed)

	// 4. Test Token Expired AND Refresh Failed -> Fallback to Password Auth
	api4 := Open(nil)
	defer api4.Close()
	api4.SetAuthTokenFile(authTokenFn)

	// Custom mock server for this scenario
	tsFallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("grant_type") == "refresh_token" {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":"invalid_grant"}`))
		} else if r.Form.Get("grant_type") == "password_limited" {
			w.WriteHeader(200)
			json.NewEncoder(w).Encode(TokenResponse{
				AccessToken:  "fallback_access",
				TokenType:    "Bearer",
				ExpiresIn:    3600,
				RefreshToken: "fallback_refresh",
				Scope:        "iracing.auth",
			})
		}
	}))
	defer tsFallback.Close()
	TokenURL = tsFallback.URL + "/oauth2/token"

	// Simulate expired token in file again
	decryptFromFile(testKeyFilename, authTokenFn, &savedToken)
	savedToken.TokenExpiry = savedToken.TokenExpiry.Add(-2 * time.Hour) // Make it expired
	encryptToFile(testKeyFilename, authTokenFn, savedToken)

	err = api4.AuthWithCredsFromFile(testKeyFilename, credsFn)
	assert.NoError(t, err)
	assert.True(t, api4.isAuthed)
	assert.Equal(t, "fallback_access", api4.AccessToken)
}
