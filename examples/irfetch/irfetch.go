package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/popmonkey/irdata"
	"gopkg.in/yaml.v3"
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

type fetchConfig struct {
	AuthTokenFile string `yaml:"authtoken"`
	KeyFile       string `yaml:"key"`
	CredsFile     string `yaml:"creds"`
	CacheDir      string `yaml:"cachedir"`
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func main() {
	var err error
	var cfg fetchConfig

	if home, err := os.UserHomeDir(); err == nil {
		configFiles := []string{
			filepath.Join(home, ".irfetch.yaml"),
			filepath.Join(home, "irfetch.yaml"),
		}
		for _, fn := range configFiles {
			if _, err := os.Stat(fn); err == nil {
				f, err := os.Open(fn)
				if err == nil {
					decoder := yaml.NewDecoder(f)
					if err := decoder.Decode(&cfg); err == nil {
						// Config loaded successfully
					}
					f.Close()
					break
				}
			}
		}
	}

	if cfg.CacheDir != "" {
		cacheDir = expandPath(cfg.CacheDir)
	}
	if cfg.AuthTokenFile != "" {
		authTokenFile = expandPath(cfg.AuthTokenFile)
	}
	if cfg.KeyFile != "" {
		cfg.KeyFile = expandPath(cfg.KeyFile)
	}
	if cfg.CredsFile != "" {
		cfg.CredsFile = expandPath(cfg.CredsFile)
	}

	flag.Parse()

	flag.Usage = func() {
		w := flag.CommandLine.Output()
		fmt.Fprintf(w, "Usage: %s [options] <path to keyfile> <path to credsfile> <api uri>\n", toolName)
		fmt.Fprintf(w, "       %s [options] <api uri> (if key/creds configured in .irfetch.yaml)\n", toolName)
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

Configuration:
%[1]s checks for a configuration file in your home directory:
  - .irfetch.yaml (or irfetch.yaml)

Example config:
  authtoken: ~/.irdata_token
  key: ~/my.key
  creds: ~/ir.creds
  cachedir: .irfetch_cache

If configured, you can omit the key and creds arguments.

(%[1]s is built in Go using the irdata library at https://github.com/popmonkey/irdata)

Example:
%[1]s -c -cachettl 60m ~/my.key ~/ir.creds /data/member/info
%[1]s --authtoken ~/.irdata_token ~/my.key ~/ir.creds /data/member/info
%[1]s /data/member/info

`, toolName)
		flag.Usage()
		os.Exit(0)
	}

	args := flag.Args()
	var keyFn, credsFn, apiUri string

	if len(args) == 3 {
		keyFn, credsFn, apiUri = args[0], args[1], args[2]
	} else if len(args) == 1 && cfg.KeyFile != "" && cfg.CredsFile != "" {
		keyFn = cfg.KeyFile
		credsFn = cfg.CredsFile
		apiUri = args[0]
	} else {
		flag.Usage()
		os.Exit(1)
	}

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
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	} else {
		err = api.AuthWithCredsFromFile(keyFn, credsFn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
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
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	writer := bufio.NewWriter(os.Stdout)

	_, err = writer.Write(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	err = writer.Flush()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
}