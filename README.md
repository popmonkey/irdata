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

irdata.SaveProvidedCredsToFile(keyFn, credsFn, credsProvider)
```

After you have a creds file you can load these into your session like so:

```go
api.AuthWithCredsFromFile(keyFn, credsFn)
```

### Creating and protecting the keyfile

For the key file, you need to create a random string of 16, 24, or 32
bytes and hex encode it into a file.  The file must be set to read only by
user (`0400`) and it is recommended this lives someplace safe.

Example key file creation in Linux:

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
[the example](example/example.go) for some json handling logic.

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

Subsequent calls over the next 15 minutes will return `data` from the local cache before
calling the iRacing /data API again.

## Chunked responses

Some iRacing data APIs returns data in chunks (e.g. `/data/results/search_series`).  When `irdata`
detects this it will fetch each chunk and return everything as one JSON blob containing an array of
chunks (marshaled as `irdata.Chunk`).  E.g.

```json
[
    {
        "Number": 0,
        "FileName": "1b57c5c8a0bd9ff081a8b2a20187219ee5259b75451eef1e5e8d7b7e7a4ade42.json?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Date=20240618T192913Z&X-Amz-SignedHeaders=host&X-Amz-Expires=1200&X-Amz-Credential=AKIAUO6OO4A3357USLO7%2F20240618%2Fus-east-1%2Fs3%2Faws4_request&X-Amz-Signature=dc1c2567163436564045e912dc0fb5043f5720f1b30e3de77a02923f10b3ca70",
        "Data": "[<json from the chunk>]"
    },
    {
        "Number": 1,
        ...
    }
]
```

You can unmarshal this to an array of `irdata.Chunk` like so:

```go
var chunks []irdata.Chunk

json.Unmarshal(data, &chunks)
```

## Debugging

You can turn on verbose logging in order to debug your sessions.  This will use the `log`
module to write to `stdout`.

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

Run example:

```sh
pushd example
go run example.go
popd
```
