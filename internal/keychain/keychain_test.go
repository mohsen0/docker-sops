package keychain

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

const testdata = "../../testdata"

func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

func TestSetGetRoundTrip(t *testing.T) {
	const identity = "AGE-SECRET-KEY-1TYDXY7M5TW8CCFSHZ2YTHWEUE0MQ2YTKM2E3QYN6J4KXXMXPM7XSJGNTNW"
	if err := Set(identity); err != nil {
		t.Fatal(err)
	}
	got, err := Get()
	if err != nil {
		t.Fatal(err)
	}
	if got != identity {
		t.Errorf("Get() = %q, want %q", got, identity)
	}
}

func TestSetRejectsGarbage(t *testing.T) {
	if err := Set("this is not a key"); err == nil {
		t.Error("expected error for garbage input")
	}
}

func TestSetRejectsAgePublicKey(t *testing.T) {
	const pub = "age1tx9d9wzsecug4rjfhqxdz64tqghk55853d9xrs84epsjhf29nu6sph4e9x"
	if err := Set(pub); err == nil {
		t.Error("expected error for public key input")
	}
}

func TestSetAcceptsAgeKeygenOutputWithComments(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(testdata, "age-test-key.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Set(string(raw)); err != nil {
		t.Fatal(err)
	}
	got, err := Get()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "#") {
		t.Errorf("Get() should not contain comment lines: %q", got)
	}
	if got != "AGE-SECRET-KEY-1TYDXY7M5TW8CCFSHZ2YTHWEUE0MQ2YTKM2E3QYN6J4KXXMXPM7XSJGNTNW" {
		t.Errorf("Get() = %q, want only the secret key line", got)
	}
}

func TestGetOnEmptyReturnsErrNotFound(t *testing.T) {
	_ = Delete()
	_, err := Get()
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestDeleteThenGetReturnsErrNotFound(t *testing.T) {
	if err := Set("AGE-SECRET-KEY-1TYDXY7M5TW8CCFSHZ2YTHWEUE0MQ2YTKM2E3QYN6J4KXXMXPM7XSJGNTNW"); err != nil {
		t.Fatal(err)
	}
	if err := Delete(); err != nil {
		t.Fatal(err)
	}
	_, err := Get()
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestDeleteOnEmptyIsNil(t *testing.T) {
	_ = Delete()
	if err := Delete(); err != nil {
		t.Errorf("Delete() on empty = %v, want nil", err)
	}
}

func TestPublicKeysMatchesTestdataComment(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(testdata, "age-test-key.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var wantPub string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "# public key:") {
			wantPub = strings.TrimSpace(strings.TrimPrefix(line, "# public key:"))
			break
		}
	}
	if wantPub == "" {
		t.Fatal("testdata file has no '# public key:' comment")
	}

	pubs, err := PublicKeys(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(pubs) != 1 {
		t.Fatalf("PublicKeys() returned %d keys, want 1", len(pubs))
	}
	if pubs[0] != wantPub {
		t.Errorf("PublicKeys()[0] = %q, want %q", pubs[0], wantPub)
	}
}
