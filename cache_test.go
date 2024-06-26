package irdata

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const testDataString1 = "IF YOU NO LONGER GO FOR A GAP THAT EXISTS, YOU`RE NO LONGER A RACING DRIVER."
const testDataString2 = "WE ARE CHECKING"
const testTtl = time.Duration(1) * time.Hour

var testCacheDir = filepath.Join(os.TempDir(), "irdata-cache")

func setupCacheTest() {
	i.cacheOpen(testCacheDir)
}

func cleanupCacheTest() {
	i.cacheClose()
	os.RemoveAll(testCacheDir)
}

func TestSetGet(t *testing.T) {
	setupCacheTest()
	t.Cleanup(cleanupCacheTest)

	key := "key"

	assert.NoError(t, i.setCachedData(key, []byte(testDataString1), testTtl))

	data, err := i.getCachedData(key)

	assert.NoError(t, err)
	assert.Equal(t, []byte(testDataString1), data)
}

func TestMultipleKVs(t *testing.T) {
	setupCacheTest()
	t.Cleanup(cleanupCacheTest)

	key1, key2 := "key1", "key2"

	assert.NoError(t, i.setCachedData(key1, []byte(testDataString1), testTtl))
	assert.NoError(t, i.setCachedData(key2, []byte(testDataString2), testTtl))

	data, err := i.getCachedData(key1)

	assert.NoError(t, err)
	assert.Equal(t, []byte(testDataString1), data)

	data, err = i.getCachedData(key2)

	assert.NoError(t, err)
	assert.Equal(t, []byte(testDataString2), data)

}

func TestSetTtl(t *testing.T) {
	setupCacheTest()
	t.Cleanup(cleanupCacheTest)

	key := "key"

	assert.NoError(t, i.setCachedData(key, []byte(testDataString1), time.Duration(1)*time.Millisecond))

	time.Sleep(2 * time.Millisecond)

	data, err := i.getCachedData(key)

	assert.NoError(t, err)
	assert.Nil(t, data)
}

func TestDelete(t *testing.T) {
	setupCacheTest()
	t.Cleanup(cleanupCacheTest)

	key := "key"

	assert.NoError(t, i.setCachedData(key, []byte(testDataString1), testTtl))
	assert.NoError(t, i.deleteCachedData(key))

	data, err := i.getCachedData(key)

	assert.NoError(t, err)
	assert.Nil(t, data)
}
