# irdata
Golang module to simplify access to the iRacing /data API from go

## Setup

```sh
go get github.com/popmonkey/irdata
```

```go
api := irdata.Open(context.Background)
```

## Authentication

You can use the provided utility function to request creds from the terminal:

```go
var credsProvider irdata.CredsFromTerminal

api.AuthWithProvideCreds(credsProvider.GetCreds)
```

You can specify the username, password yourself:

```go
var myCreds struct{}

func (myCreds) GetCreds() {
    return []byte("prost"), []byte("senna")
}

api.AuthWithProvidedCreds(myCreds)
```

You can also store your credentials in a file encrypted using a keyfile:

```go
var credsProvider irdata.CredsFromTerminal

api.AuthAndSaveProvidedCredsToFile(keyFn, credsFn, credsProvider)
```

After you have a creds file you can load these into your session like so:

```go
api.AuthWithCredsFromFile(keyFn, credsFn)
```

### Creating and protecting the keyfile

For the key file, you need to create a random string of 16, 24, or 32
bytes and base64 encode it into a file.  The file must be set to read only by
user (`0400`) and it is recommended this lives someplace safe.

Example key file creation in Linux or OS X:

```sh
openssl rand -base64 32 > ~/my.key && chmod 0400 ~/my.key
```

> [!WARNING]
> Don't check your keys into git ;)

## Accessing the /data API

Once authenticated, you can query the API by URI, for example:

```go
data, err := api.Get("/data/member/info")
```

If successful, this returns a `[]byte` array containing the JSON response.  See
[the profile example](examples/profile/profile.go) for some json handling logic.

The API is lightly documented via the /data API itself.  Check out the
[latest version](https://github.com/popmonkey/iracing-data-api-doc/blob/main/doc.json)
and
[track changes](https://github.com/popmonkey/iracing-data-api-doc/commits/main/doc.json)
to it.

## Using the cache

The iRacing /data API imposes a rate limit which can become problematic especially when
running your program over and over such as during development.

irdata therefore provides a caching layer which you can use to avoid making calls to the
actual iRacing API.

```go
api.EnableCache(".cache")

data, err := api.GetWithCache("/data/member/info", time.Duration(15)*time.Minute)
```

Subsequent calls to the same URI (with same parameters) over the next 15 minutes will return
`data` from the local cache before calling the iRacing /data API again.

## Chunked responses

Some iRacing data APIs returns data in chunks (e.g. `/data/results/search_series`).  When `irdata`
detects this it will fetch each chunk and then merge the results into an object array.  This object
array can be found in the new value `_chunk_data` which will be present where the `chunk_info` block
was found.

## Debugging

You can turn on verbose logging in order to debug your sessions.  This will use the `logrus`
module to write to `stderr`.

```go
api.EnableDebug()
```

## Development

```sh
git clone git@github.com:popmonkey/irdata.git
```

> [!NOTE]
> Keyfiles must have permissions set to 0400.  The example and test keys that are checked into
> this repository need to be adjust after cloning/pulling
> ```sh
> chmod 0400 example/example.key testdata/test.key
> ```

Run tests:

```sh
go test
```

These tests run without actually reaching out to the API.  To run the complete gamut of tests
you need to specify an existing key and creds file (such as those created in the examples) by
setting environment variables `IRDATA_TEST_KEY` to point to the keyfile and `IRDATA_TEST_CREDS`
to point to the creds file encrypted by the key:

```sh
IRDATA_TEST_KEY=/path/to/key IRDATA_TEST_CREDS=/path/to/creds go test
```

Run examples:

```sh
pushd examples/profile
go run profile.go
popd
```

## Error Handling

### Automatic Retries

`irdata` will automatically retry requests that fail with a server-side error (an HTTP `5xx` status code). By default, it will retry up to 5 times with a backoff period between each attempt. You can configure the number of retries:

```go
// Set the number of retries to 10
api.SetRetries(10)
```

### Rate Limit Management

The iRacing API imposes a rate limit on requests. `irdata` automatically manages this for you in one of two ways.

#### Return an Error (Default)

By default, if the rate limit is exceeded, `Get()` will immediately return a special error, `irdata.RateLimitExceededError`. This error type contains the time when the rate limit will reset, allowing you to build intelligent handling logic.

You can check for this specific error using `errors.As`:

```go
import "errors"
// ...

data, err := api.Get("/data/member/info")
if err != nil {
    var rateLimitErr *irdata.RateLimitExceededError
    if errors.As(err, &rateLimitErr) {
        // We are being rate limited!
        fmt.Printf("Rate limit hit. Please wait until %v to retry.\n", rateLimitErr.ResetTime)
    } else {
        // Handle other kinds of errors
        log.Fatal(err)
    }
}
```

#### Wait and Continue

Alternatively, you can configure `irdata` to automatically wait until the rate limit resets and then continue with the request. In this mode, the `Get()` call will block for the required amount of time and then return the data as if no limit was ever hit.

```go
api.SetRateLimitHandler(irdata.RateLimitWait)

// This call will now block and wait if the rate limit is hit,
// instead of returning an error.
data, err := api.Get("/data/member/info")
```
