package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
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

	var foo map[string]interface{}

	json.Unmarshal(data, &foo)

	// structured unmarshall
	type license struct {
		Category string  `json:"category_name"`
		License  string  `json:"group_name"`
		SR       float64 `json:"safety_rating"`
		IR       float64 `json:"irating"`
		CPI      float64 `json:"cpi"`
	}

	var member struct {
		CustID      int64              `json:"cust_id"`
		DisplayName string             `json:"display_name"`
		MemberSince string             `json:"member_since"`
		LastLogin   string             `json:"last_login"`
		Licenses    map[string]license `json:"licenses"`
	}

	if err := json.Unmarshal(data, &member); err != nil {
		log.Panic(err)
	}

	data, err = i.GetWithCache("data/series/seasons", time.Duration(1)*time.Hour)
	if err != nil {
		log.Panic(err)
	}

	// unstructured unmarshall
	var seasons []map[string]interface{}

	if err := json.Unmarshal(data, &seasons); err != nil {
		log.Panic(err)
	}

	fmt.Print("\n\nMember Info:\n")
	fmt.Printf("\tname:      %s\n", member.DisplayName)
	fmt.Printf("\tid:        %d\n", member.CustID)
	fmt.Printf("\tsince:     %s\n", member.MemberSince)
	fmt.Printf("\tlast seen: %s\n\n", member.LastLogin)

	keys := make([]string, len(member.Licenses))
	for k := range member.Licenses {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, k := range keys {
		license := member.Licenses[k]
		fmt.Printf("\t%s: %s\n\t\tIR: %0.0f\tSR: %0.2f\t[CPI: %0.3f]\n",
			license.Category,
			license.License,
			license.IR,
			license.SR,
			license.CPI,
		)
	}

	fmt.Print("\n--- Current Series ---\n\n")

	for _, season := range seasons {
		fmt.Printf("[%5.0f | %5.0f] %s\n",
			season["series_id"].(float64),
			season["season_id"].(float64),
			season["season_name"],
		)
	}

	fmt.Print("\n\n")
}
