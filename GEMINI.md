# irdata Project Context

## Overview
The `irdata` project is a Go client library designed to simplify interaction with the iRacing Data API. It provides a robust abstraction layer handling authentication, complex data retrieval patterns (S3 links, chunking), and local caching.

## Core Architecture

### 1. Client (`irdata.go`)
The primary interface is the `Irdata` struct. It maintains the HTTP client, authentication state, cache handle, and configuration.

```go
// Initialization pattern
ctx := context.Background()
api := irdata.Open(ctx)
defer api.Close() // Important for cache cleanup
```

### 2. Authentication (`auth.go`, `creds.go`)
The library implements the iRacing **OAuth2 Password Limited Flow**.
* **Requirements**: Username, Password, Client ID, Client Secret.
* **Storage**: Credentials can be stored on disk, encrypted via AES-GCM using a separate key file.
* **Logic**:
    * `auth.go` handles the OAuth2 token acquisition, refreshing, and file encryption/decryption.
    * `creds.go` defines the `CredsProvider` interface and a terminal-based implementation.

```go
// Usage: Authenticating with encrypted files
err := api.AuthWithCredsFromFile("path/to/key", "path/to/creds")

// Usage: Authenticating via interface
err := api.AuthWithProvideCreds(myCredsProvider)
```

### 3. Data Retrieval Logic (`irdata.go`)
The `Get` method encapsulates several layers of logic beyond a simple HTTP GET:
1.  **S3 Link Following**: If the API returns a JSON object with a `link` field (pointing to S3), the library automatically fetches that URL.
2.  **Data URL Following**: Similar to S3 links, handles `data_url` redirects.
3.  **Chunk Merging**: If the API returns `chunk_info`, the library iterates through all chunks, downloads them, and merges them into a single array under the `_chunk_data` key in the returned map.

### 4. Caching System (`cache.go`)
* **Backend**: Uses `bitcask` for persistent, disk-based key-value storage.
* **Behavior**:
    * `EnableCache(dir)` must be called to initialize.
    * `GetWithCache(uri, ttl)` checks disk first. On miss, fetches from API and writes to disk with TTL.

### 5. Resilience & Configuration
* **Rate Limiting**: Configurable via `SetRateLimitHandler`.
    * `RateLimitError`: Returns a `RateLimitExceededError` struct containing the reset time.
    * `RateLimitWait`: Blocks execution until the rate limit resets.
* **Retries**: Built-in exponential backoff for HTTP 5xx errors. Configurable via `SetRetries`.

## Important Implementation Details
* **Logging**: Uses `logrus`. Levels can be set via `SetLogLevel`.
* **Dependencies**:
    * `github.com/sirupsen/logrus`: Logging.
    * `git.mills.io/prologic/bitcask`: Caching storage.
    * `golang.org/x/term`: Secure password input.

## Usage Example

```go
package main

import (
    "context"
    "fmt"
    "time"
    "github.com/popmonkey/irdata"
)

func main() {
    api := irdata.Open(context.Background())
    defer api.Close()

    // Auth
    api.AuthWithCredsFromFile("./my.key", "./my.creds")

    // Enable Cache
    api.EnableCache("./irdata_cache")

    // Fetch Data (Member Info) with 15 min cache
    data, err := api.GetWithCache("/data/member/info", 15*time.Minute)
    if err != nil {
        panic(err)
    }

    fmt.Println(string(data))
}
```
