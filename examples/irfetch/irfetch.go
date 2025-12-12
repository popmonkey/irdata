package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/popmonkey/irdata"
)

const toolName = "irfetch"

var (
	showHelp      bool
	useCache      bool
	cacheDir      string
	cacheDuration time.Duration
	logDebug      bool
	authAndStop   bool
	authTokenFile string
)

func init() {
	flag.BoolVar(&showHelp, "h", false, "show help")
	flag.BoolVar(&showHelp, "help", false, "show help")
	flag.BoolVar(&useCache, "cache", false, "cache api results")
	flag.BoolVar(&useCache, "c", false, "cache api results")
	flag.StringVar(&cacheDir, "cachedir", "."+toolName+"_cache", "path to cache directory")
	flag.DurationVar(&cacheDuration, "cachettl", time.Duration(15)*time.Minute, "cache TTL for this call")
	flag.BoolVar(&logDebug, "v", false, "log verbosely")
	flag.BoolVar(&authAndStop, "a", false, "just run auth and stop (will generate creds file)")
	flag.StringVar(&authTokenFile, "authtoken", "", "path to file to store/load auth token")
}

func main() {
	var err error

	flag.Parse()

	flag.Usage = func() {
		w := flag.CommandLine.Output()
		fmt.Fprintf(w, "Usage: %s [options] <path to keyfile> <path to credsfile> <api uri>\n", toolName)
		flag.PrintDefaults()
	}

	if showHelp {
		fmt.Fprintf(flag.CommandLine.Output(), `
%[1]s is a tool to return results from any iRacing /data API endpoint.
It automatically follows s3Links as well as detecting and combining chunked results.

You will need to create a secret key to encrypt your credentials.  See the
instructions here:
https://github.com/popmonkey/irdata#creating-and-protecting-the-keyfile

The first time %[1]s is used it will request creds from the terminal.  It will
then use the keyfile to encrypt these in the specified credsfile.

Note that the api request should be in the form of a URI, not a full URL.

%[1]s can optionally cache results from iRacing's /data API. Subsequent requests to the
same URI will return data from this cache until it is expired.  See --help.

(%[1]s is built in Go using the irdata library at https://github.com/popmonkey/irdata)

Example:
%[1]s -c -cachettl 60m ~/my.key ~/ir.creds /data/member/info
%[1]s --authtoken ~/.irdata_token ~/my.key ~/ir.creds /data/member/info

`, toolName)
		flag.Usage()
		os.Exit(0)
	}

	if len(flag.Args()) != 3 {
		flag.Usage()
		os.Exit(1)
	}

	keyFn, credsFn, apiUri := flag.Arg(0), flag.Arg(1), flag.Arg(2)

	api := irdata.Open(context.Background())

	defer api.Close()

	if logDebug {
		api.SetLogLevel(irdata.LogLevelDebug)
	} else {
		api.SetLogLevel(irdata.LogLevelWarn)
	}

	if useCache {
		api.EnableCache(cacheDir)
	}

	if authTokenFile != "" {
		api.SetAuthTokenFile(authTokenFile)
	}

	if _, err := os.Stat(credsFn); err != nil {
		err = api.AuthAndSaveProvidedCredsToFile(keyFn, credsFn, irdata.CredsFromTerminal{})
		if err != nil {
			log.Panic(err)
		}
	} else {
		err = api.AuthWithCredsFromFile(keyFn, credsFn)
		if err != nil {
			log.Panic(err)
		}
	}

	if authAndStop {
		os.Exit(0)
	}

	var data []byte

	if useCache {
		data, err = api.GetWithCache(apiUri, cacheDuration)
	} else {
		data, err = api.Get(apiUri)
	}
	if err != nil {
		log.Panic(err)
	}

	writer := bufio.NewWriter(os.Stdout)

	_, err = writer.Write(data)
	if err != nil {
		log.Panic(err)
	}

	err = writer.Flush()
	if err != nil {
		log.Panic(err)
	}

	fmt.Println()
}
