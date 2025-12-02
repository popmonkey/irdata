# irdata

[![CI](https://github.com/popmonkey/irdata/actions/workflows/ci.yml/badge.svg)](https://github.com/popmonkey/irdata/actions/workflows/ci.yml)
[![Integration Tests](https://github.com/popmonkey/irdata/actions/workflows/integration.yml/badge.svg)](https://github.com/popmonkey/irdata/actions/workflows/integration.yml)

A Go module for simplified access to the iRacing `/data` API.

## Features

* **Modern Authentication**: Supports iRacing's **OAuth2 Password Limited Flow**
* **Simplified Management**: Handles token acquisition, masking, and credential encryption locally.
* **Transparent Data Fetching**: Follows and dereferences iRacing's S3 links transparently.
* **Automatic Chunk Merging**: If an endpoint returns chunked data, `irdata` fetches all chunks and merges them into a single object.
* **Caching Layer**: An optional disk-based cache to minimize API calls.
* **Resiliency**: Built-in support for automatic retries on server errors (`5xx`) and configurable handling for rate limits (`429`).

## Installation

```sh
go get github.com/popmonkey/irdata
```

## Quick Start

The following is a complete example of setting up the client, authenticating, and fetching data with caching enabled.

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    "time"

    "github.com/popmonkey/irdata"
)

func main() {
    // Create a new client instance.
    api := irdata.Open(context.Background())
    defer api.Close()

    // Authenticate using credentials stored in a file.
    err := api.AuthWithCredsFromFile("path/to/my.key", "path/to/my.creds")
    if err != nil {
        log.Fatalf("Authentication failed: %v", err)
    }
    
    // Enable the cache
    if err := api.EnableCache(".ir-cache"); err != nil {
        log.Fatalf("Failed to enable cache: %v", err)
    }

    // Fetch data from an API endpoint (member info for these creds)
    jsonData, err := api.GetWithCache("/data/member/info", 15*time.Minute)
    if err != nil {
        log.Fatalf("Failed to get member info: %v", err)
    }

    // Unmarshal the JSON response into a struct.
    var memberInfo struct {
        DisplayName string `json:"display_name"`
        CustomerID  int    `json:"cust_id"`
    }

    if err := json.Unmarshal(jsonData, &memberInfo); err != nil {
        log.Fatalf("Failed to parse JSON: %v", err)
    }

    fmt.Printf("Successfully fetched data for %s (Customer ID: %d)\n",
        memberInfo.DisplayName, memberInfo.CustomerID)
}
```

---

## Authentication

The library uses the **OAuth2 Password Limited Flow**. This requires you to register your "headless" client with iRacing support to obtain a **Client ID** and **Client Secret**, in addition to your standard iRacing username and password.

> [!IMPORTANT]
> **Migration Note:** If you used previous versions of `irdata`, your existing `.creds` files are incompatible. You must delete them and regenerate them using the new flow to include your Client ID and Secret.

### Encrypted Credential File (Recommended)

You can store credentials in a file encrypted with a key file. First, generate a key file.

#### Create the Key File
The key must be a random string of 16, 24, or 32 bytes, base64 encoded, and stored in a file with user-only read permissions (`0400`).

```sh
# Example for Linux or macOS
openssl rand -base64 32 > ~/my.key && chmod 0400 ~/my.key
```

> [!WARNING]
> Do not commit your key file or credentials file to version control.

#### Save and Load Credentials
Use the key file to save your credentials once. The helper `CredsFromTerminal` will prompt you for your Username, Password, Client ID, and Client Secret.

```go
// Define file paths for the key and encrypted credentials
keyFile := "my.key"
credsFile := "my.creds"

// First-time setup to save credentials from terminal input
var credsProvider irdata.CredsFromTerminal
err := api.AuthAndSaveProvidedCredsToFile(keyFile, credsFile, credsProvider)
if err != nil {
    log.Fatalf("Failed to save credentials: %v", err)
}

// In subsequent runs, you can authenticate directly from the file
err = api.AuthWithCredsFromFile(keyFile, credsFile)
if err != nil {
    log.Fatalf("Failed to auth from file: %v", err)
}
```

### Programmatic Credentials

You can provide credentials directly in your code by implementing the `CredsProvider` interface.

```go
type MyCredsProvider struct{}

func (p MyCredsProvider) GetCreds() ([]byte, []byte, []byte, []byte, error) {
    username := "your_email@example.com"
    password := "your_password"
    clientId := "your_client_id"
    clientSecret := "your_client_secret"
    
    return []byte(username), []byte(password), []byte(clientId), []byte(clientSecret), nil
}

var provider MyCredsProvider
err := api.AuthWithProvideCreds(provider)
if err != nil {
    log.Fatalf("Auth failed: %v", err)
}
```

### Terminal Prompt

To prompt for credentials from the terminal interactively:
```go
var provider irdata.CredsFromTerminal
err := api.AuthWithProvideCreds(provider)
if err != nil {
    log.Fatalf("Auth failed: %v", err)
}
```
---

## API Usage

Once authenticated, use `Get()` or `GetWithCache()` to access API endpoints.

### Basic Fetch

`Get()` retrieves data directly from the iRacing API.

```go
// Get member info
data, err := api.Get("/data/member/info")
if err != nil {
    // handle error
}
// Unmarshal and use data...
```

### Cached Fetch

The iRacing API has a rate limit. Using the cache is highly recommended to avoid interruptions.

First, enable the cache. This should only be done once.
```go
err := api.EnableCache(".cache")
if err != nil {
    // handle error
}
```

Then use `GetWithCache()` which first checks the cache for the requested data. On a cache hit, it returns the cached data immediately. On a cache miss, it calls the main API, returns the result, and populates the cache with the new data for the specified time-to-live (TTL) duration.

```go
// This call will hit the iRacing API only if the data is not in the cache
// or if the cached data is older than 15 minutes.
data, err := api.GetWithCache("/data/member/info", 15*time.Minute)
```

---

## Handling Chunked Responses

Some API endpoints (e.g., `/data/results/search_series`) return large datasets in chunks. `irdata` automatically detects this, fetches all chunks, and merges the results into a new `_chunk_data` field in the JSON response.

For a response that originally contains a `chunk_info` block, `irdata` adds the `_chunk_data` array containing the merged content from all chunks.

```json
{
  "some_other_data": "value",
  "chunk_info": {
    "num_chunks": 2,
    "base_download_url": "...",
    "chunk_file_names": ["chunk_0.json", "chunk_1.json"]
  },
  "_chunk_data": [
    { "result_id": 1 },
    { "result_id": 2 }
  ]
}
```

---

## S3 Link Callback

Some API endpoints return a link to data stored on S3 instead of the data itself. `irdata` automatically follows these links and returns the data from the S3 bucket.

If you need to know which S3 link is being followed for a given request, you can set a callback function. This is useful for debugging or logging.

```go
// Set a callback to print any S3 link that is followed.
api.SetS3LinkCallback(func(link string) {
    fmt.Printf("Following S3 link: %s\n", link)
})

// When you make a request that returns an S3 link, the callback will be invoked.
data, err := api.Get("/data/some_endpoint_with_s3_link")
```

---

## Error Handling

### Rate Limit Management

`irdata` can handle rate limits in two ways. You can change the behavior with `SetRateLimitHandler()`.

#### Return an Error (Default)

By default, `Get()` or `GetWithCache()` will return an `irdata.RateLimitExceededError` if the rate limit is hit. You can check for this specific error to handle it gracefully.  This error includes a timestamp value which is the reset time after which iRacing will no longer rate limit you.

```go
import "errors"
// ...

data, err := api.Get("/data/member/info")
if err != nil {
    var rateLimitErr *irdata.RateLimitExceededError
    if errors.As(err, &rateLimitErr) {
        fmt.Printf("Rate limit exceeded. Please wait until %v to retry.\n", rateLimitErr.ResetTime)
    } else {
        // Handle other errors
        log.Fatal(err)
    }
}
```

#### Wait and Continue

Alternatively, configure `irdata` to pause and automatically retry the request after the rate limit resets. In this mode, the call will block until it succeeds.

```go
api.SetRateLimitHandler(irdata.RateLimitWait)

// This call will now block and wait if the rate limit is hit
// instead of returning an error.
data, err := api.Get("/data/member/info")
```

### Automatic Retries

For server-side errors (HTTP `5xx` status codes), `irdata` will automatically retry the request with an increasing backoff period. You can configure the number of retries.  The default is 0 (no retries):

```go
// Set the number of retries to 10
api.SetRetries(10)
```

---

## Logging

`irdata` uses `logrus` for logging. By default, only errors are logged but more detailed logging can be enabled.

```go
api.SetLogLevel(irdata.LogLevelInfo)
```

---

## Development

Clone the repository:
```sh
git clone git@github.com:popmonkey/irdata.git
```

> [!NOTE]
> Key files included in the repository for testing must have their permissions set to `0400`.
> ```sh
> chmod 0400 testdata/test.key
> ```

Run tests:
```sh
# Run standard tests (no API calls)
go test

# Run integration tests against the live iRacing API
# Requires valid key and creds files created beforehand
IRDATA_TEST_KEY=/path/to/key.file \
IRDATA_TEST_CREDS=/path/to/creds.file \
go test
```
