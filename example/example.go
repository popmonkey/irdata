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

// this is a simple utility function that collects a username and password from the Terminal
var credsProvider irdata.CredsFromTerminal

func main() {
	// get an instance of irdata
	i := irdata.Open(context.Background())

	defer i.Close()

	// this enables some logging
	i.EnableDebug()

	// see if we have a creds file
	if _, err := os.Stat(fnExampleCreds); err == nil {
		fmt.Printf("%s exists.  Will use those creds.\n", fnExampleCreds)

		if err := i.AuthWithCredsFromFile(fnExampleKey, fnExampleCreds); err != nil {
			log.Panic(err)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		fmt.Printf("%s does not exist, lets generate it\n", fnExampleCreds)

		irdata.SaveProvidedCredsToFile(fnExampleKey, fnExampleCreds, credsProvider)

		if err := i.AuthWithCredsFromFile(fnExampleKey, fnExampleCreds); err != nil {
			log.Panic(err)
		}
	} else {
		log.Panic(err)
	}

	if err := i.EnableCache(".cache"); err != nil {
		log.Panic(err)
	}

	// we'll get the current member's info - the current member is the one whose
	//  credentials we're using to authenticate
	//
	// we'll cache the results of this call for 15 minutes
	data, err := i.GetWithCache("/data/member/info", time.Duration(15)*time.Minute)
	if err != nil {
		log.Panic(err)
	}

	// structured unmarshall
	var member struct {
		CustID      int64  `json:"cust_id"`
		DisplayName string `json:"display_name"`
		MemberSince string `json:"member_since"`
		LastLogin   string `json:"last_login"`
		Licenses    map[string]struct {
			Category string  `json:"category_name"`
			License  string  `json:"group_name"`
			SR       float64 `json:"safety_rating"`
			IR       float64 `json:"irating"`
			CPI      float64 `json:"cpi"`
		} `json:"licenses"`
	}

	if err := json.Unmarshal(data, &member); err != nil {
		log.Panic(err)
	}

	// now we'll get the most recent 90 days of sessions for this same user
	startTime := time.Now().UTC().Add(time.Duration(-(90 * 24)) * time.Hour).Format("2006-01-02T15:04Z")
	var uri = fmt.Sprintf("/data/results/search_series?cust_id=%d&start_range_begin=%s", member.CustID, startTime)

	data, err = i.GetWithCache(uri, time.Duration(1)*time.Hour)
	if err != nil {
		log.Panic(err)
	}

	// Note that this endpoint returns chunked data so we can unmarshall into []irdata.Chunk
	var chunks []irdata.Chunk

	if err := json.Unmarshal(data, &chunks); err != nil {
		log.Panic(err)
	}

	type sessionT map[string]interface{}

	var sessions []sessionT

	for _, chunk := range chunks {
		// unstructured unmarshall
		var sessionsChunk []sessionT

		if err := json.Unmarshal(chunk.Data, &sessionsChunk); err != nil {
			log.Panic(err)
		}

		sessions = append(sessions, sessionsChunk...)
	}

	fmt.Print("\n\nMember Info:\n")
	fmt.Printf("\tname:      %s\n", member.DisplayName)
	fmt.Printf("\tid:        %d\n", member.CustID)
	fmt.Printf("\tsince:     %s\n", member.MemberSince)
	fmt.Printf("\tlast seen: %s\n\n", member.LastLogin)

	var keys []string

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

	fmt.Printf("\n--- Sessions since %s ---\n\n", startTime)

	// reverse sessions so most recent comes first
	sort.SliceStable(sessions, func(i, j int) bool { return i > j })

	for _, session := range sessions {
		fmt.Printf("%s %d [%s: %s]\t%s Car: %s --- Started:%d Finished: %d\n",
			session["start_time"].(string),
			int(session["subsession_id"].(float64)),
			session["license_category"].(string),
			session["event_type_name"].(string),
			session["series_name"].(string),
			session["car_name"].(string),
			int(session["starting_position_in_class"].(float64)),
			int(session["finish_position_in_class"].(float64)),
		)
	}

	fmt.Print("\n\n")
}
