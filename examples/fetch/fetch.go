package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/popmonkey/irdata"
)

const cacheDir = ".cache"

func main() {
	args := os.Args[1:]

	if len(args) != 3 {
		fmt.Println("Usage: go run fetch.go <path to keyfile> <path to creds> <api uri>")
		os.Exit(1)
	}

	keyFn, credsFn, apiUri := args[0], args[1], args[2]

	api := irdata.Open(context.Background())

	defer api.Close()

	api.EnableDebug()
	api.EnableCache(cacheDir)

	if _, err := os.Stat(credsFn); err != nil {
		irdata.SaveProvidedCredsToFile(keyFn, credsFn, irdata.CredsFromTerminal{})
	}

	err := api.AuthWithCredsFromFile(keyFn, credsFn)
	if err != nil {
		log.Panic(err)
	}

	data, err := api.GetWithCache(apiUri, time.Duration(15)*time.Minute)
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
