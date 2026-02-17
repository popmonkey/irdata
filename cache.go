package irdata

import (
	"crypto/md5"
	"errors"
	"time"

	"git.mills.io/prologic/bitcask"
	log "github.com/sirupsen/logrus"
)

const _maxValueSize = 1024 * 1024 * 256 // 256MB
const _maxKeySize = 1024 * 4            // 4K

type hashedKey []byte

func (i *Irdata) cacheOpen(cacheDir string) error {
	var err error

	options := []bitcask.Option{
		bitcask.WithMaxValueSize(_maxValueSize),
		bitcask.WithMaxKeySize(_maxKeySize),
		bitcask.WithSync(true),
	}

	if i.cacheMaxDatafileSize > 0 {
		options = append(options, bitcask.WithMaxDatafileSize(i.cacheMaxDatafileSize))
	}

	i.cask, err = bitcask.Open(
		cacheDir,
		options...,
	)

	return err
}

func (i *Irdata) cacheClose() {
	// call close no matter what
	defer i.cask.Close()

	log.Info("Running cache cleanup")

	err := i.cask.RunGC()
	if err != nil {
		log.WithField("err", err).Info("cask.RunGC failed")
	}

	log.Debug("Merging cache")

	// Merge is compaction.  If it hits an expired key, it may return ErrKeyExpired.
	// Since we run GC right before this, it's usually transient or due to keys
	// expiring during the merge itself.  We retry up to 3 times to ensure compaction
	// completes as it is a critical maintenance task.
	for attempts := 1; attempts <= 3; attempts++ {
		err = i.cask.Merge()
		if err == nil {
			break
		}

		if errors.Is(err, bitcask.ErrKeyExpired) {
			log.WithField("attempt", attempts).Debug("cask.Merge hit expired keys, retrying...")
			_ = i.cask.RunGC()
			continue
		}

		log.WithField("err", err).Warn("cask.Merge failed")
		break
	}

	log.Info("Done")
}

func (i *Irdata) cacheCloseFast() {
	log.Info("Closing cache (fast)")
	i.cask.Close()
}

func hashKey(key string) hashedKey {
	hash := md5.Sum([]byte(key))
	return hash[:]
}

func (i *Irdata) getCachedData(key string) ([]byte, error) {
	data, err := i.cask.Get(hashKey(key))

	if errors.Is(err, bitcask.ErrKeyExpired) || errors.Is(err, bitcask.ErrKeyNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, makeErrorf("cache get error for %s [%v]", key, err)
	}

	return data, nil
}

func (i *Irdata) setCachedData(key string, data []byte, ttl time.Duration) error {
	err := i.cask.PutWithTTL(hashKey(key), data, ttl)
	if err != nil {
		return makeErrorf("cache put error for %s [%v]", key, err)
	}

	return nil
}

func (i *Irdata) deleteCachedData(key string) error {
	k := hashKey(key)

	if i.cask.Has(k) {
		err := i.cask.Delete(k)
		if err != nil {
			return makeErrorf("cache delete error for %s [%v]", key, err)
		}
	}

	return nil
}
