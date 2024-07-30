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
	i.EnableCache(testCacheDir)
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
