package passthru

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeFakeSops writes a shell script named "sops" into dir that echoes
// each argument on its own line to stdout and exits with the given code.
// It returns the script's path.
func writeFakeSops(t *testing.T, dir string, exitCode int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake sops script is POSIX shell only")
	}
	path := filepath.Join(dir, "sops")
	script := "#!/bin/sh\nfor a in \"$@\"; do echo \"$a\"; done\nexit " + itoa(exitCode) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

func TestSopsBinaryUsesEnvVar(t *testing.T) {
	dir := t.TempDir()
	fake := writeFakeSops(t, dir, 0)
	t.Setenv("DOCKER_SOPS_BIN", fake)

	got, err := SopsBinary()
	if err != nil {
		t.Fatal(err)
	}
	if got != fake {
		t.Errorf("SopsBinary() = %q, want %q", got, fake)
	}
}

func TestSopsBinaryFallsBackToPATH(t *testing.T) {
	t.Setenv("DOCKER_SOPS_BIN", "")
	dir := t.TempDir()
	writeFakeSops(t, dir, 0)
	t.Setenv("PATH", dir)

	got, err := SopsBinary()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "sops")
	if got != want {
		t.Errorf("SopsBinary() = %q, want %q", got, want)
	}
}

func TestSopsBinaryNotFoundError(t *testing.T) {
	t.Setenv("DOCKER_SOPS_BIN", "")
	t.Setenv("PATH", t.TempDir())

	_, err := SopsBinary()
	if err == nil {
		t.Fatal("expected error when sops is not found")
	}
	const want = "sops binary not found; install sops or set DOCKER_SOPS_BIN"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestRunPassesArgsThroughAndWiresStdout(t *testing.T) {
	dir := t.TempDir()
	fake := writeFakeSops(t, dir, 0)
	t.Setenv("DOCKER_SOPS_BIN", fake)
	var stdout bytes.Buffer

	code, err := Run(context.Background(), []string{"foo", "bar baz"}, Options{
		Stdout: &stdout,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	want := "foo\nbar baz\n"
	if stdout.String() != want {
		t.Errorf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunMirrorsNonZeroExitCode(t *testing.T) {
	dir := t.TempDir()
	fake := writeFakeSops(t, dir, 7)
	t.Setenv("DOCKER_SOPS_BIN", fake)

	code, err := Run(context.Background(), nil, Options{Stdout: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if code != 7 {
		t.Errorf("exit code = %d, want 7", code)
	}
}

func TestRunUsesEnvFromOptions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sops")
	script := "#!/bin/sh\necho \"MYVAR=$MYVAR\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_SOPS_BIN", path)
	var stdout bytes.Buffer

	code, err := Run(context.Background(), nil, Options{
		Stdout: &stdout,
		Env:    []string{"MYVAR=hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "MYVAR=hello") {
		t.Errorf("stdout = %q, want it to contain MYVAR=hello", stdout.String())
	}
}

func TestRunErrorsWhenBinaryMissing(t *testing.T) {
	t.Setenv("DOCKER_SOPS_BIN", "")
	t.Setenv("PATH", t.TempDir())

	_, err := Run(context.Background(), nil, Options{})
	if err == nil {
		t.Fatal("expected error when sops binary cannot be found")
	}
}
