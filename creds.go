package irdata

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

type CredsProvider interface {
	GetCreds() ([]byte, []byte, error)
}

type CredsFromTerminal struct{}

// CredsFromTerminal can be used with any of the SetCreds* functions
// and will prompt for iRacing credentials (username and password) from
// the terminal.
func (CredsFromTerminal) GetCreds() ([]byte, []byte, error) {
	username := ""

	fmt.Println("Please provide creds for an active iRacing account")
	fmt.Print("username:")
	fmt.Scan(&username)
	fmt.Print("password:")
	password_bytes, err := term.ReadPassword(int(os.Stdin.Fd()))

	fmt.Printf("\n\n")

	if err != nil {
		return nil, nil, makeErrorf("Unable to read password [%v]", err)
	}

	return []byte(username), password_bytes, nil
}
