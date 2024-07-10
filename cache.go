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

	i.cask, err = bitcask.Open(
		cacheDir,
		bitcask.WithMaxValueSize(_maxValueSize),
		bitcask.WithMaxKeySize(_maxKeySize),
		bitcask.WithSync(true),
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

	err = i.cask.Merge()
	if err != nil {
		log.WithField("err", err).Warn("cask.Merge failed")
	}

	log.Info("Done")
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
