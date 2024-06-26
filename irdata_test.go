package irdata

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

		return true
	}

	return false
}

func assertIsJson(t *testing.T, data []byte) {
	var jsonData interface{}

	assert.NoError(t, json.Unmarshal(data, &jsonData))
}

// event_types returns json directly
func TestGetBasic(t *testing.T) {
	if auth() {
		data, err := i.Get("/data/constants/event_types")
		assert.Nil(t, err)
		assert.NotNil(t, data)
		assertIsJson(t, data)
	}
}

// track uses an s3link
func TestGetWithS3Link(t *testing.T) {
	if auth() {
		data, err := i.Get("/data/track/get")
		assert.Nil(t, err)
		assert.NotNil(t, data)
		assertIsJson(t, data)
	}
}

// search_series returns chunks
func TestChunkedGet(t *testing.T) {
	if auth() {
		data, err := i.Get(
			fmt.Sprintf(
				"/data/results/search_series?start_range_begin=%s",
				time.Now().UTC().Add(time.Duration(-(1*24))*time.Hour).Format("2006-01-02T15:04Z"),
			),
		)
		assert.Nil(t, err)
		assert.NotNil(t, data)
		assertIsJson(t, data)
	}
}

// test with cached
func TestCachedGetBasic(t *testing.T) {
	i.EnableCache(testCacheDir)
	if auth() {
		data, err := i.GetWithCache("/data/constants/event_types", time.Duration(2)*time.Minute)
		assert.Nil(t, err)
		assert.NotNil(t, data)
		assertIsJson(t, data)

		data, err = i.GetWithCache("/data/constants/event_types", time.Duration(2)*time.Minute)
		assert.Nil(t, err)
		assert.NotNil(t, data)
		assertIsJson(t, data)
	}
}
