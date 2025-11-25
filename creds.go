package irdata

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

type CredsProvider interface {
	// GetCreds returns username, password, clientID, and clientSecret
	GetCreds() ([]byte, []byte, []byte, []byte, error)
}

type CredsFromTerminal struct{}

// CredsFromTerminal can be used with any of the SetCreds* functions
// and will prompt for iRacing credentials (username, password, clientID, clientSecret) from
// the terminal.
func (CredsFromTerminal) GetCreds() ([]byte, []byte, []byte, []byte, error) {
	username := ""
	clientID := ""
	clientSecret := ""

	fmt.Println("Please provide creds for an active iRacing account and Registered Client")

	fmt.Print("username (email): ")
	fmt.Scan(&username)

	fmt.Print("password: ")
	passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println() // Newline after silent input
	if err != nil {
		return nil, nil, nil, nil, makeErrorf("Unable to read password [%v]", err)
	}

	fmt.Print("client_id: ")
	fmt.Scan(&clientID)

	fmt.Print("client_secret: ")
	clientSecretBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println() // Newline after silent input
	if err != nil {
		return nil, nil, nil, nil, makeErrorf("Unable to read client secret [%v]", err)
	}
	clientSecret = string(clientSecretBytes)

	fmt.Printf("\n")

	return []byte(username), passwordBytes, []byte(clientID), []byte(clientSecret), nil
}
