package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fakeDocker installs a script as the "original docker CLI" that records its
// argv to a log file and, for any argument that is an existing file, appends
// that file's content, so tests can prove the plaintext was present while the
// child ran.
func fakeDocker(t *testing.T, exit int) (logPath string) {
	t.Helper()
	dir := t.TempDir()
	logPath = filepath.Join(dir, "log")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\" >> \"$LOG\"; if [ -f \"$a\" ]; then cat \"$a\" >> \"$LOG\"; fi; done\nfor v in $(env | sed -n 's/^\\(DOCKER_SOPS_[A-Za-z0-9_]*\\)=.*/\\1/p'); do printf '%s=' \"$v\" >> \"$LOG\"; printenv \"$v\" >> \"$LOG\"; done\nexit " + itoa(exit) + "\n"
	bin := filepath.Join(dir, "docker")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_CLI_PLUGIN_ORIGINAL_CLI_COMMAND", bin)
	t.Setenv("LOG", logPath)
	childEnv = os.Environ()
	return logPath
}

func itoa(i int) string { return strconv.Itoa(i) }

func runWrap(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errb bytes.Buffer
	cmd := newRootCommandForTest()
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), errb.String(), err
}

func encFixture(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join(testdata, "enc", name)
	dst := filepath.Join(t.TempDir(), name)
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return dst
}

func TestWrapRunRewritesEnvFileAndCleansUp(t *testing.T) {
	logPath := fakeDocker(t, 0)
	enc := encFixture(t, "app.env")
	_, stderr, err := runWrap(t, "run", "--env-file", enc, "alpine")
	if err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(logPath)
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	if lines[0] != "run" || lines[1] != "--env-file" || lines[2] == enc || filepath.Base(lines[2]) != "app.env" {
		t.Fatalf("argv not rewritten: %q", lines)
	}
	if !strings.Contains(string(log), "APP_SECRET=s3cr3t-env") {
		t.Fatalf("plaintext not visible to child: %s", log)
	}
	if _, statErr := os.Stat(lines[2]); !os.IsNotExist(statErr) {
		t.Fatalf("decrypted file %s still exists after run", lines[2])
	}
	if !strings.Contains(stderr, "decrypted 1 file") {
		t.Errorf("notice missing: %q", stderr)
	}
}

func TestWrapQuietSuppressesNotice(t *testing.T) {
	fakeDocker(t, 0)
	enc := encFixture(t, "app.env")
	_, stderr, err := runWrap(t, "--quiet", "run", "--env-file", enc, "alpine")
	if err != nil {
		t.Fatal(err)
	}
	if stderr != "" {
		t.Errorf("expected no stderr, got %q", stderr)
	}
}

func TestWrapDryRunPrintsCommandWithoutRunningDocker(t *testing.T) {
	logPath := fakeDocker(t, 0)
	enc := encFixture(t, "app.env")
	out, _, err := runWrap(t, "--dry-run", "run", "--env-file", enc, "alpine")
	if err != nil {
		t.Fatal(err)
	}
	if out != "docker run --env-file <decrypted:app.env> alpine\n" {
		t.Fatalf("got %q", out)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatal("docker was executed during dry run")
	}
}

func TestWrapMirrorsChildExitCode(t *testing.T) {
	fakeDocker(t, 3)
	_, _, err := runWrap(t, "ps")
	var code exitCodeError
	if !asExitCode(err, &code) || int(code) != 3 {
		t.Fatalf("got %v", err)
	}
}

func TestWrapReplaysDockerGlobalFlags(t *testing.T) {
	logPath := fakeDocker(t, 0)
	old := pluginArgv
	pluginArgv = []string{"docker-sops", "--context", "prod", "sops", "ps"}
	defer func() { pluginArgv = old }()
	if _, _, err := runWrap(t, "ps"); err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(logPath)
	if string(log) != "--context\nprod\nps\n" {
		t.Fatalf("got %q", log)
	}
}

func TestWrapPlainFilesPassThroughUntouched(t *testing.T) {
	logPath := fakeDocker(t, 0)
	plain := filepath.Join(testdata, "plain", "app.env")
	_, stderr, err := runWrap(t, "run", "--env-file", plain, "alpine")
	if err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(logPath)
	if !strings.Contains(string(log), plain+"\n") {
		t.Fatalf("plain path was rewritten: %s", log)
	}
	if stderr != "" {
		t.Errorf("unexpected notice for plain files: %q", stderr)
	}
}

func TestWrapPatternWithoutDetect(t *testing.T) {
	logPath := fakeDocker(t, 0)
	enc := encFixture(t, "app.env")
	plainNamed := filepath.Join(t.TempDir(), "other.env") // encrypted content but name does not match
	b, _ := os.ReadFile(enc)
	_ = os.WriteFile(plainNamed, b, 0o600)
	_, _, err := runWrap(t, "--no-detect", "--pattern", "app.*", "run", "--env-file", enc, "--env-file", plainNamed, "alpine")
	if err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(logPath)
	if !strings.Contains(string(log), "APP_SECRET=s3cr3t-env") || !strings.Contains(string(log), plainNamed+"\n") {
		t.Fatalf("pattern handling wrong: %s", log)
	}
}

func TestWrapNoArgsShowsHelp(t *testing.T) {
	out, _, err := runWrap(t)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Usage:") {
		t.Fatalf("expected help, got %q", out)
	}
}

func TestWrapMissingKeyFailsBeforeRunningDocker(t *testing.T) {
	logPath := fakeDocker(t, 0)
	t.Setenv("SOPS_AGE_KEY_FILE", filepath.Join(t.TempDir(), "missing"))
	enc := encFixture(t, "app.env")
	_, _, err := runWrap(t, "run", "--env-file", enc, "alpine")
	if err == nil || !strings.Contains(err.Error(), "SOPS_AGE_KEY_FILE") {
		t.Fatalf("expected key error, got %v", err)
	}
	if _, statErr := os.Stat(logPath); !os.IsNotExist(statErr) {
		t.Fatal("docker was executed despite decrypt failure")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func composeProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	enc, _ := os.ReadFile(filepath.Join(testdata, "enc", "app.env"))
	writeFile(t, filepath.Join(dir, "secrets.env"), string(enc))
	encY, _ := os.ReadFile(filepath.Join(testdata, "enc", "secrets.yaml"))
	writeFile(t, filepath.Join(dir, "db.enc.yaml"), string(encY))
	writeFile(t, filepath.Join(dir, "compose.yaml"), `services:
  web:
    image: alpine
    env_file:
      - secrets.env
    secrets: [db]
secrets:
  db:
    file: ./db.enc.yaml
`)
	return dir
}

func TestWrapComposeAppendsOverrideBeforeSubcommand(t *testing.T) {
	logPath := fakeDocker(t, 0)
	dir := composeProject(t)
	real, _ := filepath.EvalSymlinks(dir) // compose-go reports symlink-resolved paths
	t.Chdir(real)

	_, stderr, err := runWrap(t, "compose", "--project-name", "p1", "up", "-d")
	if err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(logPath)
	got := string(log)
	// argv: compose --project-name p1 -f <discovered compose.yaml> -f <override> up -d;
	// the fake docker appends each file's content after its path.
	base := filepath.Join(real, "compose.yaml")
	if !strings.HasPrefix(got, "compose\n--project-name\np1\n-f\n"+base+"\n") {
		t.Fatalf("discovered compose file not passed explicitly:\n%s", got)
	}
	if strings.Count(got, "\n-f\n") != 2 {
		t.Fatalf("expected base file and override, got:\n%s", got)
	}
	if !strings.Contains(got, "env_file: !override") || !strings.Contains(got, "db.enc.yaml") {
		t.Fatalf("override content missing:\n%s", got)
	}
	if !strings.Contains(got, "\nup\n-d\n") {
		t.Fatalf("subcommand args not preserved:\n%s", got)
	}
	if !strings.Contains(got, "DOCKER_SOPS_SECRET_db=") || !strings.Contains(got, "s3cr3t-yaml") {
		t.Fatalf("secret plaintext not injected into the child environment:\n%s", got)
	}
	if !strings.Contains(stderr, "decrypted 2 file(s)") {
		t.Errorf("notice: %q", stderr)
	}
}

func TestWrapComposeDryRunRedactsOverride(t *testing.T) {
	fakeDocker(t, 0)
	dir := composeProject(t)
	real, _ := filepath.EvalSymlinks(dir)
	t.Chdir(real)
	out, _, err := runWrap(t, "--dry-run", "compose", "up")
	if err != nil {
		t.Fatal(err)
	}
	if out != "docker compose -f "+filepath.Join(real, "compose.yaml")+" -f <decrypted:docker-sops.override.yaml> up\n" {
		t.Fatalf("got %q", out)
	}
}

func TestWrapComposeWithoutEncryptedFilesAddsNothing(t *testing.T) {
	logPath := fakeDocker(t, 0)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "compose.yaml"), "services:\n  web:\n    image: alpine\n")
	t.Chdir(dir)
	if _, _, err := runWrap(t, "compose", "ps"); err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(logPath)
	if string(log) != "compose\nps\n" {
		t.Fatalf("got %q", log)
	}
}

func TestWrapComposeSubcommandWithoutProjectPassesThrough(t *testing.T) {
	logPath := fakeDocker(t, 0)
	t.Chdir(t.TempDir())
	if _, _, err := runWrap(t, "compose", "version"); err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(logPath)
	if string(log) != "compose\nversion\n" {
		t.Fatalf("got %q", log)
	}
}
