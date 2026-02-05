package irdata

import (
	"context"
	"os"
	"strings"
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

func TestCloseFast(t *testing.T) {
	cacheDir, err := os.MkdirTemp("", "irdata-cache-fast-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(cacheDir)

	i := Open(context.Background())
	// Set small max datafile size to ensure multiple files are created
	i.SetCacheMaxDatafileSize(100)
	err = i.EnableCache(cacheDir)
	assert.NoError(t, err)

	// Write the same key multiple times to create "stale" data that Merge would clean up
	key := "key"
	for j := 0; j < 50; j++ {
		err = i.setCachedData(key, []byte(testDataString1), testTtl)
		assert.NoError(t, err)
	}

	countDataFiles := func(dir string) int {
		files, _ := os.ReadDir(dir)
		count := 0
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".data") {
				count++
			}
		}
		return count
	}

	// Count data files before CloseFast
	countBefore := countDataFiles(cacheDir)
	assert.True(t, countBefore > 1, "Should have multiple data files")

	i.CloseFast()

	// Count data files after CloseFast - should be the same
	countAfter := countDataFiles(cacheDir)

	assert.Equal(t, countBefore, countAfter, "Data file count should not change after CloseFast")
}

func TestCloseNormal(t *testing.T) {
	cacheDir, err := os.MkdirTemp("", "irdata-cache-normal-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(cacheDir)

	i := Open(context.Background())
	// Set small max datafile size to ensure multiple files are created
	i.SetCacheMaxDatafileSize(100)
	err = i.EnableCache(cacheDir)
	assert.NoError(t, err)

	// Write the same key multiple times to create "stale" data that Merge will clean up
	key := "key"
	for j := 0; j < 50; j++ {
		err = i.setCachedData(key, []byte(testDataString1), testTtl)
		assert.NoError(t, err)
	}

	countDataFiles := func(dir string) int {
		files, _ := os.ReadDir(dir)
		count := 0
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".data") {
				count++
			}
		}
		return count
	}

	// Count data files before Close
	countBefore := countDataFiles(cacheDir)
	assert.True(t, countBefore > 1, "Should have multiple data files before merge")

	// Normal Close should run GC and Merge
	i.Close()

	// Count data files after Close - should be reduced to 1 (or at least fewer than before)
	countAfter := countDataFiles(cacheDir)

	assert.True(t, countAfter < countBefore, "Data file count should decrease after normal Close (Merge)")
	assert.True(t, countAfter >= 1, "Should still have at least one data file")
}

func TestCacheMaxDatafileSize(t *testing.T) {
	cacheDir, err := os.MkdirTemp("", "irdata-cache-maxsize-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(cacheDir)

	i := Open(context.Background())
	// Set a very small max data file size (e.g., 100 bytes)
	i.SetCacheMaxDatafileSize(100)
	err = i.EnableCache(cacheDir)
	assert.NoError(t, err)
	defer i.Close()

	// Write enough data to force multiple files
	for j := 0; j < 10; j++ {
		key := "key" + string(rune(j))
		err = i.setCachedData(key, []byte(testDataString1), testTtl)
		assert.NoError(t, err)
	}

	// Verify we can still read it back
	for j := 0; j < 10; j++ {
		key := "key" + string(rune(j))
		data, err := i.getCachedData(key)
		assert.NoError(t, err)
		assert.Equal(t, []byte(testDataString1), data)
	}

	// Verify file sizes are approximately 100 bytes
	files, err := os.ReadDir(cacheDir)
	assert.NoError(t, err)
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".data") {
			info, err := f.Info()
			assert.NoError(t, err)
			// Bitcask might have some overhead, but it shouldn't be vastly larger than 100 bytes
			// plus the size of one record if it exceeded the limit.
			assert.True(t, info.Size() > 0)
			assert.True(t, info.Size() < 300, "File %s size %d is too large", f.Name(), info.Size())
		}
	}
}