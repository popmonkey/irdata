# irdata
Golang module to simplify access to the iRacing /data API from go

## Setup

```sh
go get github.com/popmonkey/irdata
```

```go
api := irdata.New(context.Background)
```

## Authentication

You can use the provided utility function to request creds from the terminal:

```go
var credsProvider irdata.CredsFromTerminal

api.AuthWithProvideCreds(credsProvider.GetCreds)
```

You can specify the username, password yourself:

```go
func myCreds() {
    return []byte("prost"), []byte("senna")
}

api.AuthWithProvideCreds(myCreds)
```

You can also store your credentials in a file encrypted using a keyfile:

```go
var cp irdata.CredsFromTerminal

irdata.SaveProvideCredsToFile(keyFn, credsFn, cp.GetCreds)
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
