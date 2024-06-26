// Package irdata provides simplified access to the [iRacing /data API]
//
//   - Authentication is handled internally and credentials can be saved in an
//     encrypted credentials file protected by a secure key file.
//   - The iRacing /data API returns data in the form of S3 links.  This package
//     delivers the S3 results directly handling all the redirection.
//   - If an API endpoint returns chunked data, irdata will handle the chunk fetching
//     and return a merged object (note, it could be *huge*)
//   - An optional caching layer is provided to minimize direct calls to the /data
//     endpoints themselves as those are rate limited.
//
// [iRacing /data API] https://forums.iracing.com/discussion/15068/general-availability-of-data-api/p1
package irdata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"

	"git.mills.io/prologic/bitcask"
	log "github.com/sirupsen/logrus"
)

type Irdata struct {
	httpClient http.Client
	isAuthed   bool
	cask       *bitcask.Bitcask
}

type Chunk struct {
	Number   int
	FileName string
	Data     []byte
}

type s3LinkT struct {
	Link string
}

type chunkedResultT struct {
	Type string
	Data struct {
		Success    bool
		Chunk_Info struct {
			Chunk_Size        int64
			Num_Chunks        int64
			Rows              int64
			Base_Download_Url string
			Chunk_File_Names  []string
		}
	}
}

const rootURL = "https://members-ng.iracing.com"

var urlBase *url.URL

func init() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})

	var err error
	urlBase, err = url.Parse(rootURL)
	if err != nil {
		log.Panic(err)
	}

	log.SetLevel(log.ErrorLevel)
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
		httpClient: client,
		isAuthed:   false,
		cask:       nil,
	}
}

// Close
// Calling Close when done is important when using caching - this will compact the cache.
func (i *Irdata) Close() {
	if i.cask != nil {
		i.cacheClose()
	}
}

// EnableCache enables on the optional caching layer which will
// use the directory path provided as cacheDir
func (i *Irdata) EnableCache(cacheDir string) error {
	log.WithFields(log.Fields{"cacheDir": cacheDir}).Info("Enabling cache")
	return i.cacheOpen(cacheDir)
}

// EnableDebug enables debug logging which uses the logrus module
func (i *Irdata) EnableDebug() {
	log.SetLevel(log.DebugLevel)
}

// DisableDebug disables debug logging
func (i *Irdata) DisableDebug() {
	log.SetLevel(log.ErrorLevel)
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

	log.WithFields(log.Fields{"url": url}).Info("Fetching")

	resp, err := i.retryingGet(url.String())
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var s3Link s3LinkT

	log.WithFields(log.Fields{"url": url}).Debug("Unmarshalling")

	err = json.Unmarshal(data, &s3Link)
	if err != nil {
		// there's no link so just return directly
		return data, nil
	}

	if s3Link.Link != "" {
		log.WithFields(log.Fields{"s3Link.Link": s3Link.Link}).Debug("Following s3link")

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

	// quick check for chunk info
	if bytes.Contains(data, []byte("chunk_info")) {
		var chunkedResult chunkedResultT

		err = json.Unmarshal(data, &chunkedResult)

		if err == nil {
			log.Info("Chunked data detected")

			var results []interface{}

			for chunkNumber, chunkFileName := range chunkedResult.Data.Chunk_Info.Chunk_File_Names {
				chunkUrl := fmt.Sprintf("%s%s", chunkedResult.Data.Chunk_Info.Base_Download_Url, chunkFileName)

				log.WithFields(log.Fields{
					"chunkNumber": chunkNumber,
					"chunkUrl":    chunkUrl,
				}).Debug("Fetching chunk")

				chunkResp, err := i.retryingGet(chunkUrl)
				if err != nil {
					return nil, err
				}

				chunkData, err := io.ReadAll(chunkResp.Body)
				if err != nil {
					return nil, err
				}

				var r []interface{}

				err = json.Unmarshal(chunkData, &r)
				if err != nil {
					return nil, err
				}

				log.WithFields(log.Fields{
					"len(chunkData)": len(chunkData),
					"len(r)":         len(r),
				}).Debug("Got chunk bytes")

				results = append(results, r...)
			}

			data, err = json.Marshal(results)
			if err != nil {
				return nil, err
			}

		}
	}

	return data, nil
}

// GetWithCache will first check the local cache for an unexpired result
// and will the call Get with the uri provided.
//
// The ttl defines for how long the results should be cached.
//
// You must call EnableCache before calling GetWithCache
// NOTE: If data is fetched this will return the data even
// if it can't be written to the cache (along with an error)
func (i *Irdata) GetWithCache(uri string, ttl time.Duration) ([]byte, error) {
	if i.cask == nil {
		return nil, errors.New("cache must be enabled")
	}

	log.WithFields(log.Fields{"uri": uri}).Debug("Checking for cached data")

	data, err := i.getCachedData(uri)
	if err != nil {
		log.WithFields(log.Fields{
			"err": err,
			"uri": uri,
		}).Error("Unable to get cached data")
		return nil, err
	}

	if data != nil {
		return data, nil
	}

	log.WithFields(log.Fields{"uri": uri}).Debug("Nothing in cache")

	data, err = i.Get(uri)
	if err != nil {
		return nil, err
	}

	log.WithFields(log.Fields{
		"ttl": ttl,
		"uri": uri,
	}).Debug("Got data, writing to cache")

	err = i.setCachedData(uri, data, ttl)
	if err != nil {
		log.WithFields(log.Fields{
			"uri":       uri,
			"err":       err,
			"len(data)": len(data),
		}).Error("Unable to cache")

		return data, err
	}

	return data, nil
}

func (i *Irdata) retryingGet(url string) (resp *http.Response, err error) {
	retries := 5

	for retries > 0 {
		log.WithFields(log.Fields{
			"url":     url,
			"retries": retries,
		}).Info("httpClient.Get")

		resp, err = i.httpClient.Get(url)

		if resp.StatusCode < 500 {
			break
		}

		log.WithFields(log.Fields{
			"url":             url,
			"resp.StatusCode": resp.StatusCode,
		}).Info("*** Retrying")

		retries--

		time.Sleep(time.Duration((6-retries)*5) * time.Second)
	}

	return resp, err
}
