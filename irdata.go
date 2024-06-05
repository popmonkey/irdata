// Package irdata provides simplified access to the [iRacing /data API]
//
//   - Authentication is handled internally and credentials can be saved in an
//     encrypted credentials file protected by a secure key file.
//   - The iRacing /data API returns data in the form of S3 links.  This package
//     delivers the S3 results directly handling all the redirection.
//   - An optional caching layer is provided to minimize direct calls to the /data
//     endpoints themselves as those are rate limited.
//
// [iRacing /data API] https://forums.iracing.com/discussion/15068/general-availability-of-data-api/p1
package irdata

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"

	"git.mills.io/prologic/bitcask"
)

type Irdata struct {
	isDebug    bool
	httpClient http.Client
	isAuthed   bool
	cask       *bitcask.Bitcask
}

type t_s3link struct {
	Link string
}

const rootURL = "https://members-ng.iracing.com"

var urlBase *url.URL

func init() {
	var err error
	urlBase, err = url.Parse(rootURL)
	if err != nil {
		log.Panic(err)
	}
}

func Open(ctx context.Context) *Irdata {
	jar, err := cookiejar.New(nil)
	if err != nil {
		log.Panic(err)
	}

	client := http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &Irdata{
		isDebug:    false,
		httpClient: client,
		isAuthed:   false,
		cask:       nil,
	}
}

func (i *Irdata) Close() {
	if i.cask != nil {
		err := i.cacheClose()
		if err != nil {
			log.Panic(err)
		}
	}
}

// EnableCache enables on the optional caching layer which will
// use the directory path provided as cacheDir
func (i *Irdata) EnableCache(cacheDir string) error {
	if i.isDebug {
		log.Printf("Enabling cache in %s", cacheDir)
	}
	return i.cacheOpen(cacheDir)
}

// EnableDebug enables debug logging which uses the log module
func (i *Irdata) EnableDebug() {
	i.isDebug = true
}

// Get returns the result value for the uri provided (e.g. "/data/member/info")
//
// The value returned is a JSON byte array and a potential error.
//
// Get will automatically retry 5 times if iRacing returns 500 errors
func (i *Irdata) Get(uri string) ([]byte, error) {
	if !i.isAuthed {
		return nil, errors.New("must auth first")
	}

	uriRef, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}

	url := urlBase.ResolveReference(uriRef)

	if i.isDebug {
		log.Printf("Fetching from %s", url)
	}

	resp, err := i.retryingGet(url.String())
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var s3Link t_s3link

	if i.isDebug {
		log.Printf("Unmarshalling data from %s", url)
	}

	err = json.Unmarshal([]byte(data), &s3Link)
	if err != nil {
		return nil, err
	}

	if s3Link.Link != "" {
		if i.isDebug {
			log.Printf("Fetching from link %s", s3Link.Link)
		}

		s3Resp, err := i.retryingGet(s3Link.Link)
		if err != nil {
			return nil, err
		}

		defer s3Resp.Body.Close()

		data, err = io.ReadAll(s3Resp.Body)
		if err != nil {
			return nil, err
		}
	}

	return data, nil
}

// GetWithCache will first check the local cache for an unexpired result
// and will the call Get with the uri provided.
//
// The ttl defines how long the results should be available.
//
// You must call EnableCache before calling GetWithCache
func (i *Irdata) GetWithCache(uri string, ttl time.Duration) ([]byte, error) {
	if i.cask == nil {
		return nil, errors.New("cache must be enabled")
	}

	if i.isDebug {
		log.Printf("Checking for cached data for %s", uri)
	}

	data, err := i.getCachedData(uri)
	if err != nil {
		if i.isDebug {
			log.Printf("Unable to get cached data for %s", uri)
		}
		return nil, err
	}

	if data != nil {
		return data, nil
	}

	if i.isDebug {
		log.Printf("Nothing in cache so will call iRacing /data API @ %s", uri)
	}

	data, err = i.Get(uri)
	if err != nil {
		return nil, err
	}

	if i.isDebug {
		log.Printf("Got data, now writing to cache")
	}

	err = i.setCachedData(uri, data, ttl)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (i *Irdata) retryingGet(url string) (resp *http.Response, err error) {
	if i.isDebug {
		log.Printf("Fetching %s", url)
	}

	retries := 5

	for retries > 0 {
		resp, err = i.httpClient.Get(url)

		if resp.StatusCode < 500 {
			break
		}

		if i.isDebug {
			log.Printf(" *** Retrying [%s] due to error %d", url, resp.StatusCode)
		}

		retries--

		time.Sleep(time.Duration((6-retries)*5) * time.Second)
	}

	return resp, err
}
