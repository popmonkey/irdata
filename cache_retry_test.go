package irdata

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCacheMergeRetry(t *testing.T) {
	cacheDir, err := os.MkdirTemp("", "irdata-cache-retry-")
	assert.NoError(t, err)
	defer os.RemoveAll(cacheDir)

	i := Open(context.Background())
	// Use debug level to ensure the log paths for retries are hit
	i.SetLogLevel(LogLevelDebug)
	i.SetCacheMaxDatafileSize(50) 
	err = i.EnableCache(cacheDir)
	assert.NoError(t, err)

	// Write multiple keys that expire at slightly different times
	// to trigger the retry loop during Merge().
	for j := 0; j < 50; j++ {
		key := "retry-key-" + string(rune(j))
		// Expire very quickly, but slightly offset
		ttl := time.Duration(100+j*2) * time.Millisecond 
		err = i.setCachedData(key, []byte("some data"), ttl)
		assert.NoError(t, err)
	}

	// Rotate some files to ensure Merge has work to do
	for j := 0; j < 5; j++ {
		k := "rotate-" + string(rune(j))
		err = i.setCachedData(k, []byte("filling up the file with some more data to rotate it"), 1*time.Hour)
		assert.NoError(t, err)
	}

	// Wait for the first keys to begin expiring
	time.Sleep(120 * time.Millisecond)

	// Close() triggers the retry loop if keys continue to expire during compaction
	i.Close()
}
