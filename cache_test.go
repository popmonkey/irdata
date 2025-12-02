package irdata

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const testDataString1 = "IF YOU NO LONGER GO FOR A GAP THAT EXISTS, YOU`RE NO LONGER A RACING DRIVER."
const testDataString2 = "WE ARE CHECKING"
const testTtl = time.Duration(1) * time.Hour

func setupCache(t *testing.T) (*Irdata, func()) {
	t.Helper()

	cacheDir, err := os.MkdirTemp("", "irdata-cache-test-")
	assert.NoError(t, err)

	i := Open(context.Background())
	err = i.EnableCache(cacheDir)
	assert.NoError(t, err)

	cleanup := func() {
		i.Close()
		os.RemoveAll(cacheDir)
	}

	return i, cleanup
}

func TestSetGet(t *testing.T) {
	i, cleanup := setupCache(t)
	defer cleanup()

	key := "key"

	assert.NoError(t, i.setCachedData(key, []byte(testDataString1), testTtl))

	data, err := i.getCachedData(key)

	assert.NoError(t, err)
	assert.Equal(t, []byte(testDataString1), data)
}

func TestMultipleKVs(t *testing.T) {
	i, cleanup := setupCache(t)
	defer cleanup()

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
	i, cleanup := setupCache(t)
	defer cleanup()

	key := "key"

	assert.NoError(t, i.setCachedData(key, []byte(testDataString1), time.Duration(1)*time.Millisecond))

	time.Sleep(2 * time.Millisecond)

	data, err := i.getCachedData(key)

	assert.NoError(t, err)
	assert.Nil(t, data)
}

func TestDelete(t *testing.T) {
	i, cleanup := setupCache(t)
	defer cleanup()

	key := "key"

	assert.NoError(t, i.setCachedData(key, []byte(testDataString1), testTtl))
	assert.NoError(t, i.deleteCachedData(key))

	data, err := i.getCachedData(key)

	assert.NoError(t, err)
	assert.Nil(t, data)
}