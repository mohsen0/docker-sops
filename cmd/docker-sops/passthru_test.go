package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func fakeSops(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sops")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\"\nexit 4\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEncryptPassesThroughToSopsWithExitCode(t *testing.T) {
	t.Setenv("DOCKER_SOPS_BIN", fakeSops(t))
	var out bytes.Buffer
	cmd := newSopsPassthruCommand("encrypt", "Encrypt a file with the sops binary")
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--age", "age1x", "-i", "file.yaml"})
	err := cmd.Execute()
	var ec exitCodeError
	if !asExitCode(err, &ec) || int(ec) != 4 {
		t.Fatalf("expected exit code 4, got %v", err)
	}
	if out.String() != "encrypt\n--age\nage1x\n-i\nfile.yaml\n" {
		t.Fatalf("args: %q", out.String())
	}
}

func TestEditWithoutSopsBinaryFails(t *testing.T) {
	t.Setenv("DOCKER_SOPS_BIN", filepath.Join(t.TempDir(), "missing"))
	cmd := newSopsPassthruCommand("edit", "Edit")
	cmd.SetArgs([]string{"f.yaml"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}
