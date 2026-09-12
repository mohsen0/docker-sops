//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests need docker with the plugin installed (make install) and
// SOPS_AGE_KEY_FILE pointing at testdata/age-test-key.txt (make e2e).

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "testdata", "enc", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func docker(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("docker", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func assertNoTempDirs(t *testing.T) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "docker-sops-*"))
	if len(matches) > 0 {
		t.Errorf("decrypted temp dirs left behind: %v", matches)
	}
}

func TestRunWithEncryptedEnvFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.env"), fixture(t, "app.env"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := docker(t, dir, "sops", "run", "--rm", "--env-file", "app.env", "alpine", "sh", "-c", "echo $APP_SECRET")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "s3cr3t-env") {
		t.Fatalf("plaintext not delivered to the container:\n%s", out)
	}
	assertNoTempDirs(t)
}

func TestRunMirrorsExitCode(t *testing.T) {
	_, err := docker(t, "", "sops", "run", "--rm", "alpine", "sh", "-c", "exit 7")
	var ee *exec.ExitError
	if err == nil || !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("expected exit status 7, got %v (%T %v)", err, err, ee)
	}
}

func TestBuildWithEncryptedSecret(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "npmrc.enc"), fixture(t, "secrets.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	dockerfile := "FROM alpine\nRUN --mount=type=secret,id=npmrc cat /run/secrets/npmrc\n"
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfile), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := docker(t, dir, "sops", "build", "--no-cache", "--progress=plain", "--secret", "id=npmrc,src=npmrc.enc", ".")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "s3cr3t-json") {
		t.Fatalf("secret not readable during build:\n%s", out)
	}
	assertNoTempDirs(t)
}

func TestComposeWithEncryptedEnvFileAndSecret(t *testing.T) {
	dir := t.TempDir()
	files := map[string][]byte{
		"secrets.env": fixture(t, "app.env"),
		"db.enc.yaml": fixture(t, "secrets.yaml"),
		"compose.yaml": []byte(`services:
  web:
    image: alpine
    command: sleep 120
    env_file: [secrets.env]
    secrets: [db]
secrets:
  db:
    file: ./db.enc.yaml
`),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	project := "dockersopse2e"
	t.Cleanup(func() { _, _ = docker(t, dir, "compose", "-p", project, "down", "--remove-orphans") })

	if out, err := docker(t, dir, "sops", "compose", "-p", project, "up", "-d"); err != nil {
		t.Fatalf("up: %v\n%s", err, out)
	}
	assertNoTempDirs(t)

	out, err := docker(t, dir, "exec", project+"-web-1", "sh", "-c", "echo $APP_SECRET; cat /run/secrets/db")
	if err != nil {
		t.Fatalf("exec: %v\n%s", err, out)
	}
	if !strings.Contains(out, "s3cr3t-env") {
		t.Errorf("env_file plaintext missing:\n%s", out)
	}
	if !strings.Contains(out, "s3cr3t-yaml") {
		t.Errorf("secret plaintext missing after the plugin exited:\n%s", out)
	}
	if out, err := docker(t, dir, "sops", "compose", "-p", project, "down"); err != nil {
		t.Fatalf("down: %v\n%s", err, out)
	}
}

func TestDecryptCommand(t *testing.T) {
	out, err := docker(t, "", "sops", "decrypt", filepath.Join("..", "testdata", "enc", "app.env"))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out != "APP_SECRET=s3cr3t-env\nDB_URL=postgres://user:pw@db/app\n" {
		t.Fatalf("got %q", out)
	}
}
