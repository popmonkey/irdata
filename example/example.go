package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/popmonkey/irdata"
)

const fnExampleKey = "example.key"
const fnExampleCreds = "example.creds"

var credsProvider irdata.CredsFromTerminal

func main() {
	i := irdata.Open(context.Background())

	defer i.Close()

	i.EnableDebug()

	// see if we have a creds file
	if _, err := os.Stat(fnExampleCreds); err == nil {
		fmt.Printf("%s exists.  Will use those creds.\n", fnExampleCreds)

		if err := i.AuthWithCredsFromFile(fnExampleKey, fnExampleCreds); err != nil {
			log.Panic(err)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		fmt.Printf("%s does not exist, lets generate it\n", fnExampleCreds)

		irdata.SaveProvidedCredsToFile(fnExampleKey, fnExampleCreds, credsProvider.GetCreds)

		if err := i.AuthWithCredsFromFile(fnExampleKey, fnExampleCreds); err != nil {
			log.Panic(err)
		}
	} else {
		log.Panic(err)
	}

	if err := i.EnableCache(".cache"); err != nil {
		log.Panic(err)
	}

	data, err := i.GetWithCache("/data/member/info", time.Duration(15)*time.Minute)
	if err != nil {
		log.Panic(err)
	}

	var member struct {
		CustID      int64  `json:"cust_id"`
		DisplayName string `json:"display_name"`
		MemberSince string `json:"member_since"`
	}

	if err := json.Unmarshal(data, &member); err != nil {
		log.Panic(err)
	}

	fmt.Print("\n\nMember Info:\n")
	fmt.Printf("\tname:\t%s\n", member.DisplayName)
	fmt.Printf("\tid:\t%d\n", member.CustID)
	fmt.Printf("\tsince:\t%s\n", member.MemberSince)
}
