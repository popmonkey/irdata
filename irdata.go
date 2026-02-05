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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"git.mills.io/prologic/bitcask"
	log "github.com/sirupsen/logrus"
)

// RateLimitHandler defines the behavior when a rate limit is encountered.
type RateLimitHandler int

const (
	// RateLimitError (default) will cause Get methods to return a RateLimitExceededError.
	RateLimitError RateLimitHandler = iota
	// RateLimitWait will cause the Get method to pause and wait until the rate limit resets.
	RateLimitWait
)

// RateLimitExceededError is returned when the iRacing API rate limit has been exceeded.
// It includes the time when the rate limit is expected to reset.
type RateLimitExceededError struct {
	ResetTime time.Time
}

func (e *RateLimitExceededError) Error() string {
	return fmt.Sprintf("iRacing API rate limit exceeded; resets at %v", e.ResetTime)
}

type Irdata struct {
	httpClient http.Client
	isAuthed   bool
	cask       *bitcask.Bitcask
	getRetries int

	cacheMaxDatafileSize int

	// Auth Data used for Refreshing
	AccessToken  string
	RefreshToken string
	TokenExpiry  time.Time
	ClientID     string
	ClientSecret string

	// S3 Link callback
	S3LinkCallback func(link string)

	// Rate limiting fields
	rateLimitHandler   RateLimitHandler
	rateLimitMu        sync.Mutex
	rateLimitRemaining int
	rateLimitReset     time.Time

	// Auth Persistence
	authTokenFile string
}

type LogLevel int8

const (
	LogLevelFatal LogLevel = iota
	LogLevelError LogLevel = iota
	LogLevelWarn  LogLevel = iota
	LogLevelInfo  LogLevel = iota
	LogLevelDebug LogLevel = iota
)

type s3LinkT struct {
	Link string `json:"link"`
}

const ChunkDataKey = "_chunk_data"

type dataUrlT struct {
	DataURL string `json:"data_url"`
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
	client := http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &Irdata{
		httpClient:           client,
		isAuthed:             false,
		cask:                 nil,
		getRetries:           0,
		rateLimitHandler:     RateLimitError, // Default to erroring out
		cacheMaxDatafileSize: 1024 * 1024,
	}
}

// Close
// Calling Close when done is important when using caching - this will compact the cache.
func (i *Irdata) Close() {
	if i.cask != nil {
		i.cacheClose()
	}
}

// CloseFast
// Calling CloseFast when done will close the cache WITHOUT compacting/merging it.
func (i *Irdata) CloseFast() {
	if i.cask != nil {
		i.cacheCloseFast()
	}
}

// EnableCache enables on the optional caching layer which will
// use the directory path provided as cacheDir
func (i *Irdata) EnableCache(cacheDir string) error {
	log.WithFields(log.Fields{"cacheDir": cacheDir}).Debug("Enabling cache")
	return i.cacheOpen(cacheDir)
}

// SetCacheMaxDatafileSize sets the maximum size of a single datafile in the cache (in bytes).
// This must be called before EnableCache.
func (i *Irdata) SetCacheMaxDatafileSize(size int) {
	i.cacheMaxDatafileSize = size
}

// SetLogLevel sets the loging level using the logrus module
func (i *Irdata) SetLogLevel(logLevel LogLevel) {
	switch logLevel {
	case LogLevelFatal:
		log.SetLevel(log.FatalLevel)
	case LogLevelError:
		log.SetLevel(log.ErrorLevel)
	case LogLevelInfo:
		log.SetLevel(log.InfoLevel)
	case LogLevelWarn:
		log.SetLevel(log.WarnLevel)
	case LogLevelDebug:
		log.SetLevel(log.DebugLevel)
	}
}

// SetRateLimitHandler sets the desired behavior for handling API rate limits.
// The default is RateLimitError.
func (i *Irdata) SetRateLimitHandler(handler RateLimitHandler) {
	i.rateLimitHandler = handler
}

// SetRetries sets the number of times a get will be retried if a retriable error
// is encountered (e.g. a 5xx)
func (i *Irdata) SetRetries(retries int) {
	i.getRetries = retries
}

// SetS3LinkCallback sets a callback function to be invoked when an S3 link is
// identified and followed.
func (i *Irdata) SetS3LinkCallback(callback func(link string)) {
	i.S3LinkCallback = callback
}

// SetAuthTokenFile sets the filename where the authentication token will be
// persisted.  The token is encrypted using the same key as the credentials file.
func (i *Irdata) SetAuthTokenFile(filename string) {
	i.authTokenFile = filename
}

// Get returns the result value for the uri provided (e.g. "/data/member/info")
//
// The value returned is a JSON byte array and a potential error.
func (i *Irdata) Get(uri string) ([]byte, error) {
	if !i.isAuthed {
		return nil, makeErrorf("must auth first")
	}

	uriRef, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}

	url := urlBase.ResolveReference(uriRef)

	log.WithFields(log.Fields{"url": url}).Debug("Fetching")

	resp, err := i.retryingGet(url.String())
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// If the response is not 200 OK, it's likely not the JSON we expect.
	if resp.StatusCode != http.StatusOK {
		return nil, makeErrorf("received non-200 status code: %d - body: %s", resp.StatusCode, string(data))
	}

	// First, try to unmarshal as an S3 link object
	var s3Link s3LinkT
	if json.Unmarshal(data, &s3Link) == nil && s3Link.Link != "" {
		if i.S3LinkCallback != nil {
			i.S3LinkCallback(s3Link.Link)
		}

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
	} else {
		// If not an S3 link, try to unmarshal as a data URL object
		var dataUrl dataUrlT
		if json.Unmarshal(data, &dataUrl) == nil && dataUrl.DataURL != "" {
			log.WithFields(log.Fields{"dataUrl.Data_Url": dataUrl.DataURL}).Debug("Following dataUrl")
			dataUrlResp, err := i.retryingGet(dataUrl.DataURL)
			if err != nil {
				return nil, err
			}
			defer dataUrlResp.Body.Close()
			data, err = io.ReadAll(dataUrlResp.Body)
			if err != nil {
				return nil, err
			}
		}
		// If neither of the above, we assume the original 'data' is the final response.
	}

	// quick check for chunk info
	if bytes.Contains(data, []byte("chunk_info")) {
		var raw map[string]interface{}

		err = json.Unmarshal(data, &raw)
		if err != nil {
			return nil, err
		}

		// walk the object looking for chunks
		err = i.resolveChunks(raw)
		if err != nil {
			return nil, err
		}

		data, err = json.Marshal(raw)
		if err != nil {
			return nil, err
		}
	}

	return data, nil
}

func (i *Irdata) resolveChunks(raw map[string]interface{}) error {
	for k, v := range raw {
		if k == "chunk_info" {
			log.WithFields(log.Fields{
				"chunk_info": v,
			}).Debug("Chunked data found")

			var results []interface{}

			if v != nil {
				chunkInfo := v.(map[string]interface{})

				for chunkNumber, chunkFileName := range chunkInfo["chunk_file_names"].([]interface{}) {
					chunkUrl := fmt.Sprintf("%s%s", chunkInfo["base_download_url"], chunkFileName)

					log.WithFields(log.Fields{
						"chunkNumber": chunkNumber,
						"chunkUrl":    chunkUrl,
					}).Debug("Fetching chunk")

					chunkResp, err := i.retryingGet(chunkUrl)
					if err != nil {
						return err
					}

					chunkData, err := io.ReadAll(chunkResp.Body)
					if err != nil {
						return err
					}

					var r []interface{}

					err = json.Unmarshal(chunkData, &r)
					if err != nil {
						return err
					}

					log.WithFields(log.Fields{
						"len(chunkData)": len(chunkData),
						"len(r)":         len(r),
					}).Debug("Got chunk bytes")

					results = append(results, r...)
				}
			}

			// insert the results in the special ChunkDataKey key
			raw[ChunkDataKey] = results
		} else {
			// recurse deeper into objects
			o, ok := v.(map[string]interface{})
			if ok {
				i.resolveChunks(o)
			}
			// TODO: Do we need to walk arrays?  could an array have chunks?
		}
	}

	return nil
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
		return nil, makeErrorf("cache must be enabled")
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
		log.WithFields(log.Fields{"uri": uri}).Debug("Cached data found")
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

// updateRateLimit parses rate limit headers and updates the internal state.
func (i *Irdata) updateRateLimit(resp *http.Response) {
	i.rateLimitMu.Lock()
	defer i.rateLimitMu.Unlock()

	if remaining := resp.Header.Get("x-ratelimit-remaining"); remaining != "" {
		if val, err := strconv.Atoi(remaining); err == nil {
			i.rateLimitRemaining = val
		}
	}

	if reset := resp.Header.Get("x-ratelimit-reset"); reset != "" {
		if val, err := strconv.ParseInt(reset, 10, 64); err == nil {
			i.rateLimitReset = time.Unix(val, 0)
		}
	}

	log.WithFields(log.Fields{
		"remaining": i.rateLimitRemaining,
		"reset":     i.rateLimitReset,
	}).Debug("Updated rate limit state")
}

func (i *Irdata) retryingGet(url string) (resp *http.Response, err error) {
	// Proactive Token Refresh
	// If we are authenticated and the token is within 1 minute of expiring, refresh it.
	if i.isAuthed && !i.TokenExpiry.IsZero() && time.Now().Add(1*time.Minute).After(i.TokenExpiry) {
		log.Info("Access token expiring soon, refreshing...")
		if rErr := i.refreshToken(); rErr != nil {
			log.WithFields(log.Fields{"err": rErr}).Warn("Failed to refresh token, proceeding with old token")
			// We proceed; maybe the clock is wrong, or maybe we will get a 401 and fail then.
		}
	}

	// Proactive rate limit check
	i.rateLimitMu.Lock()
	if i.rateLimitRemaining <= 0 && time.Now().Before(i.rateLimitReset) {
		resetTime := i.rateLimitReset
		handler := i.rateLimitHandler
		i.rateLimitMu.Unlock() // Unlock before potentially waiting

		log.WithFields(log.Fields{
			"reset":   resetTime,
			"handler": handler,
		}).Warn("Rate limit reached proactively")

		if handler == RateLimitError {
			return nil, &RateLimitExceededError{ResetTime: resetTime}
		}

		// RateLimitWait
		waitUntil := time.Until(resetTime)
		log.WithFields(log.Fields{"wait": waitUntil}).Info("Waiting for rate limit reset")
		time.Sleep(waitUntil)
	} else {
		i.rateLimitMu.Unlock()
	}

	attempts := i.getRetries + 1

	for attempts > 0 {
		log.WithFields(log.Fields{
			"url":     url,
			"retries": attempts - 1,
		}).Info("httpClient.Do")

		// Create a new request so we can set the Header
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}

		// *** OAUTH HEADER LOGIC ***
		// 1. Check if we have an Access Token
		// 2. Check if the domain is iRacing (Do NOT send headers to S3, they will reject it)
		if i.AccessToken != "" && strings.Contains(url, "iracing.com") {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", i.AccessToken))
		}

		resp, err = i.httpClient.Do(req)
		if err != nil {
			// If there's a network error etc., we should probably just fail.
			return nil, err
		}

		// Always update rate limit state from headers on any response
		i.updateRateLimit(resp)

		// Handle 401 Unauthorized (Expired Token)
		// If we get a 401 and we haven't tried refreshing yet in this loop, try once.
		if resp.StatusCode == http.StatusUnauthorized {
			log.Warn("Received 401 Unauthorized. Attempting token refresh.")
			// Attempt refresh
			if rErr := i.refreshToken(); rErr == nil {
				// Close the old body
				resp.Body.Close()
				continue
			} else {
				log.WithError(rErr).Error("Token refresh failed during 401 handling.")
				// If refresh failed, the 401 is likely permanent. Break/return.
			}
		}

		// Handle 429 Too Many Requests (Rate Limit)
		if resp.StatusCode == http.StatusTooManyRequests {
			if i.rateLimitHandler == RateLimitError {
				i.rateLimitMu.Lock()
				resetTime := i.rateLimitReset
				i.rateLimitMu.Unlock()
				return nil, &RateLimitExceededError{ResetTime: resetTime}
			}

			i.rateLimitMu.Lock()
			resetTime := i.rateLimitReset
			i.rateLimitMu.Unlock()
			waitUntil := time.Until(resetTime)
			if waitUntil < 0 {
				waitUntil = 0
			}
			log.WithFields(log.Fields{"wait": waitUntil}).Info("Waiting for rate limit reset after 429")
			time.Sleep(waitUntil)
			continue
		} else if resp.StatusCode < 500 {
			// This is a success or a non-retriable client error, break the loop
			break
		}

		attempts--
		backoff := time.Duration((i.getRetries-attempts)*5) * time.Second
		log.WithFields(log.Fields{
			"url":             url,
			"resp.StatusCode": resp.StatusCode,
			"backoff":         backoff,
		}).Warn("*** Retrying 5xx error")
		time.Sleep(backoff)
	}

	return resp, err
}
