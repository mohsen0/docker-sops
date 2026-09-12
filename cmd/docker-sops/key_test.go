package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/mohsen0/docker-sops/internal/keychain"
)

func runKey(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := newKeyCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func testKey(t *testing.T) (secret, public string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(testdata, "age-test-key.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(string(b), "\n") {
		switch {
		case strings.HasPrefix(l, "# public key: "):
			public = strings.TrimPrefix(l, "# public key: ")
		case strings.HasPrefix(l, "AGE-SECRET-KEY-"):
			secret = l
		}
	}
	return secret, public
}

func TestKeySetFromStdinThenShowPrintsPublicKey(t *testing.T) {
	keyring.MockInit()
	secret, public := testKey(t)
	if _, err := runKey(t, secret+"\n", "set"); err != nil {
		t.Fatal(err)
	}
	out, err := runKey(t, "", "show")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != public {
		t.Fatalf("got %q want %q", out, public)
	}
	if strings.Contains(out, "AGE-SECRET-KEY") {
		t.Fatal("show must not print the private key by default")
	}
}

func TestKeySetFromFileAndShowPrivate(t *testing.T) {
	keyring.MockInit()
	secret, _ := testKey(t)
	if _, err := runKey(t, "", "set", "--file", filepath.Join(testdata, "age-test-key.txt")); err != nil {
		t.Fatal(err)
	}
	out, err := runKey(t, "", "show", "--private")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != secret {
		t.Fatalf("got %q", out)
	}
}

func TestKeyShowWithoutKeyErrors(t *testing.T) {
	keyring.MockInit()
	_, err := runKey(t, "", "show")
	if err == nil || !strings.Contains(err.Error(), "no age key stored") {
		t.Fatalf("got %v", err)
	}
}

func TestKeyRmRemovesKey(t *testing.T) {
	keyring.MockInit()
	secret, _ := testKey(t)
	if _, err := runKey(t, secret, "set"); err != nil {
		t.Fatal(err)
	}
	if _, err := runKey(t, "", "rm"); err != nil {
		t.Fatal(err)
	}
	if _, err := keychain.Get(); err == nil {
		t.Fatal("key still stored")
	}
}

func TestKeySetRejectsInvalidInput(t *testing.T) {
	keyring.MockInit()
	if _, err := runKey(t, "not a key\n", "set"); err == nil {
		t.Fatal("expected error")
	}
}

func TestInjectKeychainKeySetsSopsAgeKeyOnlyWhenUnset(t *testing.T) {
	keyring.MockInit()
	secret, _ := testKey(t)
	_ = keychain.Set(secret)
	t.Setenv("SOPS_AGE_KEY", "")
	os.Unsetenv("SOPS_AGE_KEY")
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	os.Unsetenv("SOPS_AGE_KEY_FILE")
	t.Setenv("SOPS_AGE_KEY_CMD", "")
	os.Unsetenv("SOPS_AGE_KEY_CMD")
	if !injectKeychainKey() {
		t.Fatal("expected injection")
	}
	if os.Getenv("SOPS_AGE_KEY") != secret {
		t.Fatalf("SOPS_AGE_KEY = %q", os.Getenv("SOPS_AGE_KEY"))
	}
	t.Setenv("SOPS_AGE_KEY_FILE", "/some/file")
	os.Unsetenv("SOPS_AGE_KEY")
	if injectKeychainKey() {
		t.Fatal("must not inject when SOPS_AGE_KEY_FILE is set")
	}
}
