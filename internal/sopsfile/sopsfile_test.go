package sopsfile

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const testdata = "../../testdata"

func TestMain(m *testing.M) {
	abs, _ := filepath.Abs(filepath.Join(testdata, "age-test-key.txt"))
	_ = os.Setenv("SOPS_AGE_KEY_FILE", abs)
	os.Exit(m.Run())
}

func TestDetectEncryptedFixtures(t *testing.T) {
	for _, name := range []string{"secrets.yaml", "secrets.json", "app.env", "settings.ini", "blob.bin"} {
		data, err := os.ReadFile(filepath.Join(testdata, "enc", name))
		if err != nil {
			t.Fatal(err)
		}
		if !Detect(data) {
			t.Errorf("%s: expected encrypted", name)
		}
	}
}

func TestDetectRejectsPlainFiles(t *testing.T) {
	cases := []string{"plain/secrets.yaml", "plain/secrets.json", "plain/app.env", "plain/settings.ini", "plain/blob.bin", "negative/plain.yaml"}
	for _, rel := range cases {
		data, err := os.ReadFile(filepath.Join(testdata, rel))
		if err != nil {
			t.Fatal(err)
		}
		if Detect(data) {
			t.Errorf("%s: expected plain", rel)
		}
	}
	if Detect(nil) || Detect([]byte("sops_mac")) || Detect([]byte("{not json")) {
		t.Error("garbage detected as encrypted")
	}
}

func TestIsEncryptedPath(t *testing.T) {
	enc, err := IsEncrypted(filepath.Join(testdata, "enc", "app.env"))
	if err != nil || !enc {
		t.Fatalf("enc/app.env: got %v, %v", enc, err)
	}
	enc, err = IsEncrypted(filepath.Join(testdata, "plain", "app.env"))
	if err != nil || enc {
		t.Fatalf("plain/app.env: got %v, %v", enc, err)
	}
	if _, err := IsEncrypted(filepath.Join(testdata, "missing")); err == nil {
		t.Fatal("missing file should error")
	}
	if enc, err := IsEncrypted(testdata); err != nil || enc {
		t.Fatalf("directory: got %v, %v", enc, err)
	}
}

func TestInferFormat(t *testing.T) {
	cases := map[string]string{
		"a.yaml": "yaml", "a.yml": "yaml", "a.json": "json", "a.env": "dotenv",
		".env": "dotenv", "a.ini": "ini", "a.bin": "binary", "a": "binary",
		// a trailing .enc / .sops segment is stripped before inferring
		"secrets.yaml.enc": "yaml", ".env.enc": "dotenv", "a.json.sops": "json",
	}
	for name, want := range cases {
		if got := InferFormat(name, nil); got != want {
			t.Errorf("%s: got %s, want %s", name, got, want)
		}
	}
}

func TestInferFormatSniffsContentForUnknownExtension(t *testing.T) {
	read := func(n string) []byte { b, _ := os.ReadFile(filepath.Join(testdata, "enc", n)); return b }
	cases := []struct{ name, want string }{
		{"app.env", "dotenv"}, {"secrets.yaml", "yaml"}, {"secrets.json", "json"}, {"blob.bin", "binary"},
	}
	for _, c := range cases {
		if got := InferFormat("noext", read(c.name)); got != c.want {
			t.Errorf("%s content: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestDecryptFixtures(t *testing.T) {
	read := func(dir, n string) []byte {
		b, err := os.ReadFile(filepath.Join(testdata, dir, n))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	// exact round trips
	for _, name := range []string{"app.env", "blob.bin"} {
		got, err := Decrypt(filepath.Join(testdata, "enc", name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(got, read("plain", name)) {
			t.Errorf("%s: got %q want %q", name, got, read("plain", name))
		}
	}
	// structured round trips (sops may reformat whitespace)
	var wantY, gotY map[string]any
	got, err := Decrypt(filepath.Join(testdata, "enc", "secrets.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	_ = yaml.Unmarshal(got, &gotY)
	_ = yaml.Unmarshal(read("plain", "secrets.yaml"), &wantY)
	if gotY["database"].(map[string]any)["password"] != wantY["database"].(map[string]any)["password"] {
		t.Errorf("yaml mismatch: %s", got)
	}
	var wantJ, gotJ map[string]any
	got, err = Decrypt(filepath.Join(testdata, "enc", "secrets.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(got, &gotJ)
	_ = json.Unmarshal(read("plain", "secrets.json"), &wantJ)
	if gotJ["api_key"] != wantJ["api_key"] {
		t.Errorf("json mismatch: %s", got)
	}
	got, err = Decrypt(filepath.Join(testdata, "enc", "settings.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "s3cr3t-ini") {
		t.Errorf("ini mismatch: %s", got)
	}
}

func TestDecryptPlainFileFails(t *testing.T) {
	if _, err := Decrypt(filepath.Join(testdata, "plain", "app.env")); err == nil {
		t.Fatal("expected error decrypting a plain file")
	}
}

func TestDecryptWithoutKeyFails(t *testing.T) {
	t.Setenv("SOPS_AGE_KEY_FILE", filepath.Join(t.TempDir(), "nope"))
	if _, err := Decrypt(filepath.Join(testdata, "enc", "app.env")); err == nil {
		t.Fatal("expected error without key")
	}
}
