// Package keychain stores age identities in the OS credential store
// (macOS Keychain, Windows Credential Manager, or the freedesktop Secret
// Service on Linux) via github.com/zalando/go-keyring.
package keychain

import (
	"errors"
	"fmt"
	"strings"

	"filippo.io/age"
	"github.com/zalando/go-keyring"
)

// Service is the keyring service name under which identities are stored.
const Service = "docker-sops"

// Account is the keyring account/user name under which identities are stored.
const Account = "age-key"

// ErrNotFound is returned by Get when no age key is stored in the keychain.
var ErrNotFound = errors.New("no age key stored in the keychain")

// Set validates that identities is one or more age secret keys (one per
// line; lines starting with "#" are ignored, as age-keygen emits comment
// lines) and stores the validated secret key lines, joined with "\n", in
// the OS keychain.
func Set(identities string) error {
	lines, err := parseIdentities(identities)
	if err != nil {
		return err
	}
	return keyring.Set(Service, Account, strings.Join(lines, "\n"))
}

// Get returns the stored identities, or ErrNotFound if none are stored.
func Get() (string, error) {
	v, err := keyring.Get(Service, Account)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", ErrNotFound
		}
		return "", err
	}
	return v, nil
}

// Delete removes the stored identities. It is not an error if nothing is
// stored.
func Delete() error {
	err := keyring.Delete(Service, Account)
	if err != nil && errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// PublicKeys derives the age recipient(s) for the given identities string
// (one secret key per line, comment lines starting with "#" ignored).
func PublicKeys(identities string) ([]string, error) {
	lines, err := parseIdentities(identities)
	if err != nil {
		return nil, err
	}
	pubs := make([]string, 0, len(lines))
	for _, line := range lines {
		id, err := age.ParseX25519Identity(line)
		if err != nil {
			return nil, fmt.Errorf("parsing age identity: %w", err)
		}
		pubs = append(pubs, id.Recipient().String())
	}
	return pubs, nil
}

// parseIdentities splits identities into non-comment, non-empty lines and
// validates each one is an age secret key.
func parseIdentities(identities string) ([]string, error) {
	var lines []string
	for _, raw := range strings.Split(identities, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := validateSecretKey(line); err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return nil, errors.New("no age secret key found in input")
	}
	return lines, nil
}

const secretKeyPrefix = "AGE-SECRET-KEY-1"

func validateSecretKey(line string) error {
	if !strings.HasPrefix(line, secretKeyPrefix) {
		return fmt.Errorf("not an age secret key (must start with %q): %q", secretKeyPrefix, truncate(line))
	}
	if strings.ContainsAny(line, " \t\r\n") {
		return fmt.Errorf("age secret key contains whitespace: %q", truncate(line))
	}
	return nil
}

func truncate(s string) string {
	const max = 20
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
