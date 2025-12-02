package irdata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// test helpers

func setupTest(t *testing.T, handler http.Handler) (*Irdata, func()) {
	t.Helper()

	server := httptest.NewServer(handler)

	originalURLBase := urlBase
	mockURL, err := url.Parse(server.URL)
	assert.NoError(t, err)
	urlBase = mockURL

	client := Open(context.Background())
	client.isAuthed = true // Bypass auth for unit tests

	cleanup := func() {
		server.Close()
		urlBase = originalURLBase
	}

	return client, cleanup
}

func getJsonObject(t *testing.T, data []byte) map[string]interface{} {
	var jsonData map[string]interface{}
	assert.NoError(t, json.Unmarshal(data, &jsonData))
	return jsonData
}

func getJsonArray(t *testing.T, data []byte) []interface{} {
	var jsonData []interface{}
	assert.NoError(t, json.Unmarshal(data, &jsonData))
	return jsonData
}

// tests

func TestResolveChunksEmpty(t *testing.T) {
	raw := map[string]interface{}{}
	raw["chunk_info"] = nil
	i := &Irdata{} // Doesn't need a full client
	assert.NoError(t, i.resolveChunks(raw))
	v, ok := raw[ChunkDataKey]
	assert.True(t, ok)
	assert.Nil(t, v)
}

func TestGetBasic(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `[{"label": "Oval"}]`)
	})
	client, cleanup := setupTest(t, handler)
	defer cleanup()

	data, err := client.Get("/data/constants/event_types")
	assert.NoError(t, err)
	a := getJsonArray(t, data)
	assert.Equal(t, "Oval", a[0].(map[string]interface{})["label"])
}

func TestGetWithS3Link(t *testing.T) {
	s3Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `[{"category": "oval"}]`)
	}))
	defer s3Server.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"link": "%s"}`, s3Server.URL)
	})
	client, cleanup := setupTest(t, handler)
	defer cleanup()

	data, err := client.Get("/data/track/get")
	assert.NoError(t, err)
	a := getJsonArray(t, data)
	assert.Equal(t, "oval", a[0].(map[string]interface{})["category"])
}

func TestS3LinkCallback(t *testing.T) {
	s3Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"s3_data": "success"}`)
	}))
	defer s3Server.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"link": "%s"}`, s3Server.URL)
	})
	client, cleanup := setupTest(t, handler)
	defer cleanup()

	var callbackURL string
	var callbackFired bool
	client.SetS3LinkCallback(func(link string) {
		callbackURL = link
		callbackFired = true
	})

	data, err := client.Get("/fake-endpoint-for-s3-link")
	assert.NoError(t, err)
	assert.True(t, callbackFired)
	assert.Equal(t, s3Server.URL, callbackURL)
	j := getJsonObject(t, data)
	assert.Equal(t, "success", j["s3_data"])
}

func TestDataUrl(t *testing.T) {
	dataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"roster": "some_roster"}`)
	}))
	defer dataServer.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"data_url": "%s"}`, dataServer.URL)
	})
	client, cleanup := setupTest(t, handler)
	defer cleanup()

	data, err := client.Get("/data/league/roster?league_id=666")
	assert.NoError(t, err)
	o := getJsonObject(t, data)
	assert.Equal(t, "some_roster", o["roster"])
}

func TestCachedGetBasic(t *testing.T) {
	var requestCount int
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		fmt.Fprintln(w, `[{"label": "Oval"}]`)
	})
	client, cleanup := setupTest(t, handler)
	defer cleanup()

	cacheDir, err := os.MkdirTemp("", "irdata-test-cache-")
	assert.NoError(t, err)
	defer os.RemoveAll(cacheDir)

	err = client.EnableCache(cacheDir)
	assert.NoError(t, err)

	// First call - should hit the server
	data, err := client.GetWithCache("/data/constants/event_types", 2*time.Minute)
	assert.NoError(t, err)
	assert.NotNil(t, data)
	assert.Equal(t, 1, requestCount, "Server should be hit once")

	// Second call - should hit the cache
	data, err = client.GetWithCache("/data/constants/event_types", 2*time.Minute)
	assert.NoError(t, err)
	assert.NotNil(t, data)
	assert.Equal(t, 1, requestCount, "Server should not be hit a second time")
}

func TestChunked(t *testing.T) {
	mux := http.NewServeMux()

	// Mock for the chunk files
	mux.HandleFunc("/chunk1", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `[{"event_code": "loaded"}]`)
	})
	mux.HandleFunc("/chunk2", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `[{"event_code": "unloaded"}]`)
	})

	// Mock for the main endpoint that returns chunk_info
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// We need to construct the base URL for the chunks from the running server
		serverURL, _ := url.Parse("http://" + r.Host)
		
		chunkInfo := map[string]interface{}{
			"chunk_info": map[string]interface{}{
				"base_download_url": serverURL.String(),
				"chunk_file_names":  []string{"/chunk1", "/chunk2"},
			},
		}
		json.NewEncoder(w).Encode(chunkInfo)
	})

	client, cleanup := setupTest(t, mux)
	defer cleanup()

	data, err := client.Get("/data/results/event_log")
	assert.NoError(t, err)

	o := getJsonObject(t, data)
	assert.NotNil(t, o)

	chunks, ok := o["_chunk_data"].([]interface{})
	assert.True(t, ok)
	assert.Len(t, chunks, 2)
	assert.Equal(t, "loaded", chunks[0].(map[string]interface{})["event_code"])
	assert.Equal(t, "unloaded", chunks[1].(map[string]interface{})["event_code"])
}