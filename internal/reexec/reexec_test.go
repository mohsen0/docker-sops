//go:build !windows

// These tests drive Run with POSIX shell scripts and signals; Windows only
// gets a compile check in CI.

package reexec

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

// writeScript writes a shell script to a fresh temp dir, marks it
// executable, and returns its path.
func writeScript(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "docker")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRun_ExitCodeZero(t *testing.T) {
	bin := writeScript(t, "#!/bin/sh\nexit 0\n")
	code, err := Run(context.Background(), bin, nil, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestRun_ExitCodeNonZeroMirrored(t *testing.T) {
	bin := writeScript(t, "#!/bin/sh\nexit 3\n")
	code, err := Run(context.Background(), bin, nil, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 3 {
		t.Fatalf("exit code = %d, want 3", code)
	}
}

func TestRun_StdoutStderrWiring(t *testing.T) {
	bin := writeScript(t, "#!/bin/sh\necho out-line\necho err-line 1>&2\n")
	var stdout, stderr bytes.Buffer
	code, err := Run(context.Background(), bin, nil, Options{Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := strings.TrimSpace(stdout.String()); got != "out-line" {
		t.Errorf("stdout = %q, want %q", got, "out-line")
	}
	if got := strings.TrimSpace(stderr.String()); got != "err-line" {
		t.Errorf("stderr = %q, want %q", got, "err-line")
	}
}

func TestRun_ArgsPassedThroughVerbatim(t *testing.T) {
	bin := writeScript(t, "#!/bin/sh\necho \"$@\"\n")
	var stdout bytes.Buffer
	args := []string{"--context", "prod", "-D", "run", "x y"}
	code, err := Run(context.Background(), bin, args, Options{Stdout: &stdout})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	want := "--context prod -D run x y"
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Errorf("args echoed = %q, want %q", got, want)
	}
}

func TestRun_StdinWiring(t *testing.T) {
	bin := writeScript(t, "#!/bin/sh\ncat\n")
	var stdout bytes.Buffer
	code, err := Run(context.Background(), bin, nil, Options{
		Stdin:  strings.NewReader("hello from stdin"),
		Stdout: &stdout,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := stdout.String(); got != "hello from stdin" {
		t.Errorf("stdout = %q, want %q", got, "hello from stdin")
	}
}

func TestRun_EnvPassedThrough(t *testing.T) {
	bin := writeScript(t, "#!/bin/sh\necho \"$FOO\"\n")
	var stdout bytes.Buffer
	code, err := Run(context.Background(), bin, nil, Options{
		Stdout: &stdout,
		Env:    append(os.Environ(), "FOO=bar-baz"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := strings.TrimSpace(stdout.String()); got != "bar-baz" {
		t.Errorf("stdout = %q, want %q", got, "bar-baz")
	}
}

func TestRun_ChildKilledBySignalYields128PlusSignal(t *testing.T) {
	bin := writeScript(t, "#!/bin/sh\nkill -TERM $$\n")
	code, err := Run(context.Background(), bin, nil, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := 128 + int(syscall.SIGTERM)
	if code != want {
		t.Fatalf("exit code = %d, want %d", code, want)
	}
}

func TestRun_ForwardsSignalToChild(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "trap-installed")
	bin := writeScript(t, "#!/bin/sh\ntrap 'exit 7' TERM\n: > \""+marker+"\"\nwhile true; do sleep 0.05; done\n")

	type result struct {
		code int
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		code, err := Run(context.Background(), bin, nil, Options{})
		resCh <- result{code, err}
	}()

	// Wait for the child to prove its TERM trap is installed before we
	// signal this test process. Run installs its own signal forwarding
	// (via signal.Notify) before starting the child, so by the time the
	// child can create the marker file, forwarding is already armed.
	// Without this synchronization, sending SIGTERM too early would kill
	// the test binary instead of exercising forwarding.
	waitForFile(t, marker, 5*time.Second)
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("failed to signal self: %v", err)
	}

	select {
	case res := <-resCh:
		if res.err != nil {
			t.Fatalf("unexpected error: %v", res.err)
		}
		if res.code != 7 {
			t.Fatalf("exit code = %d, want 7", res.code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for child to exit after forwarded signal")
	}
}

// waitForFile polls until path exists or the timeout elapses.
func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to appear", path)
}

func TestRun_ContextCancelKillsChild(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "running")
	bin := writeScript(t, "#!/bin/sh\n: > \""+marker+"\"\nwhile true; do sleep 0.05; done\n")
	ctx, cancel := context.WithCancel(context.Background())

	type result struct {
		code int
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		code, err := Run(ctx, bin, nil, Options{})
		resCh <- result{code, err}
	}()

	waitForFile(t, marker, 5*time.Second)
	cancel()

	select {
	case res := <-resCh:
		if res.err != nil {
			t.Fatalf("unexpected error: %v", res.err)
		}
		want := 128 + int(syscall.SIGKILL)
		if res.code != want {
			t.Fatalf("exit code = %d, want %d", res.code, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for child to be killed after context cancel")
	}
}

func TestDockerBinary_PrefersEnvVar(t *testing.T) {
	t.Setenv("DOCKER_CLI_PLUGIN_ORIGINAL_CLI_COMMAND", "/custom/path/docker")
	got, err := DockerBinary()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/custom/path/docker" {
		t.Fatalf("got %q, want %q", got, "/custom/path/docker")
	}
}

func TestDockerBinary_FallsBackToPath(t *testing.T) {
	t.Setenv("DOCKER_CLI_PLUGIN_ORIGINAL_CLI_COMMAND", "")
	dir := t.TempDir()
	dockerPath := filepath.Join(dir, "docker")
	if err := os.WriteFile(dockerPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	got, err := DockerBinary()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resolved, err := exec.LookPath("docker")
	if err != nil {
		t.Fatalf("LookPath: %v", err)
	}
	if got != resolved {
		t.Fatalf("got %q, want %q", got, resolved)
	}
}

func TestDockerBinary_ErrorsWhenNeitherAvailable(t *testing.T) {
	t.Setenv("DOCKER_CLI_PLUGIN_ORIGINAL_CLI_COMMAND", "")
	dir := t.TempDir()
	t.Setenv("PATH", dir)

	if _, err := DockerBinary(); err == nil {
		t.Fatal("expected error when docker is not resolvable, got nil")
	}
}

func TestGlobalFlags(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		pluginName string
		want       []string
	}{
		{
			name:       "none",
			args:       []string{"docker-sops", "sops", "run", "x"},
			pluginName: "sops",
			want:       nil,
		},
		{
			name:       "several flags",
			args:       []string{"docker-sops", "--context", "prod", "-D", "sops", "run", "x"},
			pluginName: "sops",
			want:       []string{"--context", "prod", "-D"},
		},
		{
			name:       "flag=value form",
			args:       []string{"docker-sops", "--context=prod", "--host=tcp://x", "sops", "run"},
			pluginName: "sops",
			want:       []string{"--context=prod", "--host=tcp://x"},
		},
		{
			name:       "plugin name missing",
			args:       []string{"docker-sops", "--context", "prod", "run", "x"},
			pluginName: "sops",
			want:       nil,
		},
		{
			name:       "plugin name appears again later as an arg",
			args:       []string{"docker-sops", "--context", "prod", "sops", "run", "sops"},
			pluginName: "sops",
			want:       []string{"--context", "prod"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := GlobalFlags(tc.args, tc.pluginName)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("GlobalFlags(%v, %q) = %#v, want %#v", tc.args, tc.pluginName, got, tc.want)
			}
		})
	}
}
