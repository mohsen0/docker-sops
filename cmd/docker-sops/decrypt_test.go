package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testdata = "../../testdata"

func TestMain(m *testing.M) {
	abs, _ := filepath.Abs(filepath.Join(testdata, "age-test-key.txt"))
	os.Setenv("SOPS_AGE_KEY_FILE", abs)
	os.Exit(m.Run())
}

func runDecrypt(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := newDecryptCommand()
	cmd.SetOut(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestDecryptPrintsPlaintextToStdout(t *testing.T) {
	out, err := runDecrypt(t, filepath.Join(testdata, "enc", "app.env"))
	if err != nil {
		t.Fatal(err)
	}
	if out != "APP_SECRET=s3cr3t-env\nDB_URL=postgres://user:pw@db/app\n" {
		t.Fatalf("got %q", out)
	}
}

func TestDecryptToOutputFile(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "out.env")
	out, err := runDecrypt(t, "-o", dst, filepath.Join(testdata, "enc", "app.env"))
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("stdout should be empty, got %q", out)
	}
	b, _ := os.ReadFile(dst)
	if !strings.Contains(string(b), "s3cr3t-env") {
		t.Errorf("file content %q", b)
	}
	fi, _ := os.Stat(dst)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %o", fi.Mode().Perm())
	}
}

func TestDecryptInPlace(t *testing.T) {
	src := filepath.Join(t.TempDir(), "app.env")
	enc, _ := os.ReadFile(filepath.Join(testdata, "enc", "app.env"))
	_ = os.WriteFile(src, enc, 0o600)
	if _, err := runDecrypt(t, "-i", src); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(src)
	if !strings.Contains(string(b), "s3cr3t-env") {
		t.Errorf("file not decrypted in place: %q", b)
	}
}

func TestDecryptRejectsInPlaceWithOutput(t *testing.T) {
	if _, err := runDecrypt(t, "-i", "-o", "x", filepath.Join(testdata, "enc", "app.env")); err == nil {
		t.Fatal("expected error")
	}
}

func TestDecryptPlainFileErrors(t *testing.T) {
	_, err := runDecrypt(t, filepath.Join(testdata, "plain", "app.env"))
	if err == nil || !strings.Contains(err.Error(), "not a sops-encrypted file") {
		t.Fatalf("got %v", err)
	}
}

func TestDecryptWithoutKeyShowsHint(t *testing.T) {
	t.Setenv("SOPS_AGE_KEY_FILE", filepath.Join(t.TempDir(), "missing"))
	_, err := runDecrypt(t, filepath.Join(testdata, "enc", "app.env"))
	if err == nil || !strings.Contains(err.Error(), "SOPS_AGE_KEY_FILE") || !strings.Contains(err.Error(), "docs/keychain.md") {
		t.Fatalf("expected key hint, got %v", err)
	}
}
