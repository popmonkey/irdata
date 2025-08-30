package irdata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var i *Irdata = Open(context.Background())

var authed bool = false

func auth() bool {
	if authed {
		return true
	}

	testKeyFilename := os.Getenv("IRDATA_TEST_KEY")
	testCredsFilename := os.Getenv("IRDATA_TEST_CREDS")

	if testKeyFilename != "" && testCredsFilename != "" {
		err := i.AuthWithCredsFromFile(testKeyFilename, testCredsFilename)
		if err != nil {
			panic(err)
		}

		authed = true

		// Create a temp dir for caching tests
		dir, err := os.MkdirTemp("", "irdata-cache-")
		if err != nil {
			panic(err)
		}
		testCacheDir = dir

		return true
	}

	return false
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

// TestRateLimiting deliberately hits the rate limit concurrently to test both error and wait handlers.
// NOTE: This test can take over a minute to run as it must wait for the rate limit to reset.
// It is skipped by default. To run it, set the environment variable RUN_RATE_LIMIT_TEST=true
func TestRateLimiting(t *testing.T) {
	if os.Getenv("RUN_RATE_LIMIT_TEST") != "true" {
		t.Skip("Skipping rate limit test; set RUN_RATE_LIMIT_TEST=true to run.")
	}

	if auth() {
		const numRequests = 20 // Number of concurrent requests to send

		// Test the default RateLimitError behavior with concurrent requests
		t.Run("RateLimitError", func(t *testing.T) {
			i.SetRateLimitHandler(RateLimitError)
			var wg sync.WaitGroup
			errs := make(chan error, numRequests)

			for j := 0; j < numRequests; j++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, err := i.Get("/data/constants/event_types")
					if err != nil {
						errs <- err
					}
				}()
			}
			wg.Wait()
			close(errs)

			// Check if we received at least one rate limit error
			foundRateLimitError := false
			var rateLimitErr *RateLimitExceededError
			for err := range errs {
				if errors.As(err, &rateLimitErr) {
					foundRateLimitError = true
					assert.NotZero(t, rateLimitErr.ResetTime, "ResetTime should be set in the error")
					break
				}
			}
			assert.True(t, foundRateLimitError, "Expected at least one error of type *RateLimitExceededError from concurrent requests")
		})

		// Test the RateLimitWait behavior with concurrent requests
		t.Run("RateLimitWait", func(t *testing.T) {
			i.SetRateLimitHandler(RateLimitWait)
			var wg sync.WaitGroup
			errs := make(chan error, numRequests)

			// This call should now wait for the reset and succeed.
			// It will take a while.
			for j := 0; j < numRequests; j++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, err := i.Get("/data/constants/event_types")
					if err != nil {
						errs <- err
					}
				}()
			}
			wg.Wait()
			close(errs)

			// Check that there were no errors
			assert.Len(t, errs, 0, "Expected no errors when handler is set to wait")
		})

		// Reset to defaults for other tests
		i.SetRateLimitHandler(RateLimitError)
	}
}

// test resolveChunks with empty chunk_info
func TestResolveChunksEmpty(t *testing.T) {
	raw := map[string]interface{}{}

	raw["chunk_info"] = nil

	assert.NoError(t, i.resolveChunks(raw))

	v, ok := raw[ChunkDataKey]

	assert.True(t, ok)
	assert.Nil(t, v)
}

// event_types returns json directly
func TestGetBasic(t *testing.T) {
	if auth() {
		data, err := i.Get("/data/constants/event_types")
		assert.NoError(t, err)
		assert.NotNil(t, data)
		a := getJsonArray(t, data)
		assert.NotNil(t, a[0].(map[string]interface{})["label"])
	}
}

// track uses an s3link
func TestGetWithS3Link(t *testing.T) {
	if auth() {
		data, err := i.Get("/data/track/get")
		assert.NoError(t, err)
		assert.NotNil(t, data)
		a := getJsonArray(t, data)
		assert.NotNil(t, a[0].(map[string]interface{})["category"])
	}
}

// search_series returns chunks in a data value
func TestChunkedGetType1(t *testing.T) {
	if auth() {
		data, err := i.Get(
			fmt.Sprintf(
				"/data/results/search_series?start_range_begin=%s",
				time.Now().UTC().Add(time.Duration(-(1))*time.Hour).Format("2006-01-02T15:04Z"),
			),
		)
		assert.NoError(t, err)
		assert.NotNil(t, data)
		o := getJsonObject(t, data)
		assert.NotNil(t, o)
		a := o["data"].(map[string]interface{})["_chunk_data"].([]interface{})
		assert.NotNil(t, a[0].(map[string]interface{})["series_short_name"])
	}
}

// event_log returns chunks in the top level
func TestChunkedGetType2(t *testing.T) {
	if auth() {
		data, err := i.Get("/data/results/event_log?subsession_id=69054157&simsession_number=0")
		assert.NoError(t, err)
		assert.NotNil(t, data)
		o := getJsonObject(t, data)
		assert.NotNil(t, o)
		a := o["_chunk_data"].([]interface{})
		assert.NotNil(t, a[0].(map[string]interface{})["event_code"])
	}
}

// test with cached
func TestCachedGetBasic(t *testing.T) {
	err := i.EnableCache(testCacheDir)
	assert.NoError(t, err)

	if auth() {
		data, err := i.GetWithCache("/data/constants/event_types", time.Duration(2)*time.Minute)
		assert.NoError(t, err)
		assert.NotNil(t, data)
		a := getJsonArray(t, data)
		assert.NotNil(t, a[0].(map[string]interface{})["label"])

		data, err = i.GetWithCache("/data/constants/event_types", time.Duration(2)*time.Minute)
		assert.Nil(t, err)
		assert.NotNil(t, data)
		a = getJsonArray(t, data)
		assert.NotNil(t, a[0].(map[string]interface{})["label"])
	}
}

// test dataUrl following
func TestDataUrl(t *testing.T) {
	if auth() {
		data, err := i.Get("/data/league/roster?league_id=666")
		assert.NoError(t, err)
		assert.NotNil(t, data)
		o := getJsonObject(t, data)
		assert.NotNil(t, o["roster"])
	}
}
