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

	log.Info("RunGC")

	err := i.cask.RunGC()
	if err != nil {
		log.WithField("err", err).Info("cask.RunGC failed")
	}

	log.Info("Merging cache")

	err = i.cask.Merge()
	if err != nil {
		log.WithField("err", err).Info("cask.Merge failed")
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
	}

	return data, err
}

func (i *Irdata) setCachedData(key string, data []byte, ttl time.Duration) error {
	return i.cask.PutWithTTL(hashKey(key), data, ttl)
}

func (i *Irdata) deleteCachedData(key string) error {
	k := hashKey(key)
	if i.cask.Has(k) {
		return i.cask.Delete(k)
	} else {
		return nil
	}
}
