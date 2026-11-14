// Package secrets keeps the session token in the OS keychain (macOS Keychain,
// Linux Secret Service). There is deliberately no file fallback: if the
// keychain is unavailable, signing in fails.
package secrets

import (
	"errors"

	"github.com/zalando/go-keyring"

	"tracker/desktop/internal/brand"
)

// ErrNotFound means no token is stored for that server.
var ErrNotFound = errors.New("no stored session")

// Keychain stores one token per server origin.
type Keychain interface {
	Get(server string) (string, error)
	Set(server, token string) error
	Delete(server string) error
}

// OS is the system keychain.
type OS struct{}

func service() string { return brand.Name + " session" }

// Get returns the token stored for server.
func (OS) Get(server string) (string, error) {
	tok, err := keyring.Get(service(), server)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return tok, err
}

// Set stores the token for server, replacing any previous one.
func (OS) Set(server, token string) error { return keyring.Set(service(), server, token) }

// Delete removes the token for server. Deleting a missing token is not an error.
func (OS) Delete(server string) error {
	err := keyring.Delete(service(), server)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
