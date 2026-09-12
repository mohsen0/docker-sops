package argscan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mohsen0/docker-sops/internal/tempstore"
)

const testdataDir = "../../testdata"

func TestMain(m *testing.M) {
	abs, err := filepath.Abs(filepath.Join(testdataDir, "age-test-key.txt"))
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("SOPS_AGE_KEY_FILE", abs)
	os.Exit(m.Run())
}

func mustCopy(t *testing.T, dir, name, srcRel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testdataDir, srcRel))
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, name)
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return dst
}

func newStore(t *testing.T) *tempstore.Store {
	t.Helper()
	s, err := tempstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestPlanNilStoreErrors(t *testing.T) {
	_, err := Plan([]string{"build", "."}, nil, Options{})
	if err == nil {
		t.Fatal("expected error for nil store")
	}
}

func TestEnvFileSpaceSeparated(t *testing.T) {
	dir := t.TempDir()
	mustCopy(t, dir, "enc.env", "enc/app.env")
	store := newStore(t)
	argv := []string{"run", "--env-file", "enc.env", "alpine"}
	rw, err := Plan(argv, store, Options{Detect: true, Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if rw.Args[2] == "enc.env" {
		t.Fatalf("expected rewrite, got %v", rw.Args)
	}
	if filepath.Base(rw.Args[2]) != "enc.env" {
		t.Errorf("basename changed: %s", rw.Args[2])
	}
	if rw.Args[0] != "run" || rw.Args[1] != "--env-file" || rw.Args[3] != "alpine" {
		t.Errorf("unexpected mutation: %v", rw.Args)
	}
	if len(rw.Decrypted) != 1 || rw.Decrypted[0].Source != "enc.env" || rw.Decrypted[0].Path != rw.Args[2] {
		t.Errorf("decrypted list: %+v", rw.Decrypted)
	}
	want, err := os.ReadFile(filepath.Join(testdataDir, "plain", "app.env"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(rw.Args[2])
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("plaintext mismatch: got %q want %q", got, want)
	}
}

func TestEnvFileEqualsForm(t *testing.T) {
	dir := t.TempDir()
	mustCopy(t, dir, "enc.env", "enc/app.env")
	store := newStore(t)
	argv := []string{"run", "--env-file=enc.env", "alpine"}
	rw, err := Plan(argv, store, Options{Detect: true, Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rw.Args[1], "--env-file=") {
		t.Fatalf("prefix lost: %s", rw.Args[1])
	}
	newPath := strings.TrimPrefix(rw.Args[1], "--env-file=")
	if newPath == "enc.env" {
		t.Fatalf("expected rewrite: %s", rw.Args[1])
	}
	if filepath.Base(newPath) != "enc.env" {
		t.Errorf("basename changed: %s", newPath)
	}
}

func TestBarePositionalSecretCreate(t *testing.T) {
	dir := t.TempDir()
	mustCopy(t, dir, "enc.env", "enc/app.env")
	store := newStore(t)
	argv := []string{"secret", "create", "myname", "enc.env"}
	rw, err := Plan(argv, store, Options{Detect: true, Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if rw.Args[3] == "enc.env" {
		t.Fatalf("expected rewrite: %v", rw.Args)
	}
	if rw.Args[2] != "myname" {
		t.Errorf("unexpected mutation: %v", rw.Args)
	}
}

func TestBuildSecretSrc(t *testing.T) {
	dir := t.TempDir()
	mustCopy(t, dir, "enc.env", "enc/app.env")
	store := newStore(t)
	argv := []string{"build", "--secret", "id=npmrc,src=enc.env", "."}
	rw, err := Plan(argv, store, Options{Detect: true, Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rw.Args[2], "id=npmrc,src=") {
		t.Fatalf("shape lost: %s", rw.Args[2])
	}
	val := strings.TrimPrefix(rw.Args[2], "id=npmrc,src=")
	if val == "enc.env" {
		t.Fatalf("expected rewrite: %s", rw.Args[2])
	}
}

func TestMountTypeBindSource(t *testing.T) {
	dir := t.TempDir()
	mustCopy(t, dir, "enc.env", "enc/app.env")
	store := newStore(t)
	argv := []string{"build", "--mount", "type=bind,source=enc.env,target=/t", "."}
	rw, err := Plan(argv, store, Options{Detect: true, Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rw.Args[2], "type=bind,source=") || !strings.HasSuffix(rw.Args[2], ",target=/t") {
		t.Fatalf("shape lost: %s", rw.Args[2])
	}
	middle := strings.TrimSuffix(strings.TrimPrefix(rw.Args[2], "type=bind,source="), ",target=/t")
	if middle == "enc.env" {
		t.Fatalf("expected rewrite: %s", rw.Args[2])
	}
}

func TestPlainFileUntouched(t *testing.T) {
	dir := t.TempDir()
	mustCopy(t, dir, "plain.env", "plain/app.env")
	store := newStore(t)
	argv := []string{"build", "--env-file", "plain.env", "."}
	rw, err := Plan(argv, store, Options{Detect: true, Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if rw.Args[2] != "plain.env" {
		t.Errorf("plain file rewritten: %v", rw.Args)
	}
	if len(rw.Decrypted) != 0 {
		t.Errorf("decrypted list should be empty: %+v", rw.Decrypted)
	}
}

func TestNonExistentPathUntouched(t *testing.T) {
	dir := t.TempDir()
	store := newStore(t)
	argv := []string{"build", "--env-file", "does-not-exist.env", "."}
	rw, err := Plan(argv, store, Options{Detect: true, Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if rw.Args[2] != "does-not-exist.env" {
		t.Errorf("nonexistent path rewritten: %v", rw.Args)
	}
}

func TestDirectoryUntouched(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "somedir"), 0o700); err != nil {
		t.Fatal(err)
	}
	store := newStore(t)
	argv := []string{"build", "--env-file", "somedir", "."}
	rw, err := Plan(argv, store, Options{Detect: true, Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if rw.Args[2] != "somedir" {
		t.Errorf("directory rewritten: %v", rw.Args)
	}
}

func TestDashPrefixedTokenNotCandidate(t *testing.T) {
	dir := t.TempDir()
	mustCopy(t, dir, "-enc.env", "enc/app.env")
	store := newStore(t)
	argv := []string{"build", "-enc.env", "."}
	rw, err := Plan(argv, store, Options{Detect: true, Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if rw.Args[1] != "-enc.env" {
		t.Errorf("dash-prefixed token was rewritten: %v", rw.Args)
	}
	if len(rw.Decrypted) != 0 {
		t.Errorf("expected no decryption: %+v", rw.Decrypted)
	}
}

func TestRunCutoffStopsAtImage(t *testing.T) {
	dir := t.TempDir()
	mustCopy(t, dir, "enc.env", "enc/app.env")
	store := newStore(t)
	argv := []string{"run", "--env-file", "enc.env", "alpine", "cat", "enc.env"}
	rw, err := Plan(argv, store, Options{Detect: true, Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if rw.Args[2] == "enc.env" {
		t.Fatalf("pre-image reference was not rewritten: %v", rw.Args)
	}
	if rw.Args[5] != "enc.env" {
		t.Errorf("post-image reference should be untouched: %v", rw.Args)
	}
	if len(rw.Decrypted) != 1 {
		t.Errorf("expected exactly one decryption: %+v", rw.Decrypted)
	}
}

func TestCombinedShortFlagsBeforeImage(t *testing.T) {
	dir := t.TempDir()
	mustCopy(t, dir, "enc.env", "enc/app.env")
	store := newStore(t)
	argv := []string{"run", "-it", "--env-file", "enc.env", "alpine", "cat", "enc.env"}
	rw, err := Plan(argv, store, Options{Detect: true, Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if rw.Args[1] != "-it" {
		t.Errorf("boolean combined flag mutated: %v", rw.Args)
	}
	if rw.Args[3] == "enc.env" {
		t.Fatalf("pre-image reference was not rewritten: %v", rw.Args)
	}
	if rw.Args[6] != "enc.env" {
		t.Errorf("post-image reference should be untouched: %v", rw.Args)
	}
}

func TestBuildScansAfterDoubleDash(t *testing.T) {
	dir := t.TempDir()
	mustCopy(t, dir, "enc.env", "enc/app.env")
	store := newStore(t)
	argv := []string{"build", "--build-arg", "X=1", "--", "enc.env"}
	rw, err := Plan(argv, store, Options{Detect: true, Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if rw.Args[4] == "enc.env" {
		t.Fatalf("token after -- should still be scanned for non-run commands: %v", rw.Args)
	}
}

func TestSameFileTwiceDecryptedOnce(t *testing.T) {
	dir := t.TempDir()
	mustCopy(t, dir, "enc.env", "enc/app.env")
	store := newStore(t)
	argv := []string{"build", "--env-file", "enc.env", "--env-file", "enc.env", "."}
	rw, err := Plan(argv, store, Options{Detect: true, Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if rw.Args[2] != rw.Args[4] {
		t.Errorf("expected identical rewritten path, got %s and %s", rw.Args[2], rw.Args[4])
	}
	if len(rw.Decrypted) != 1 {
		t.Errorf("expected a single decryption entry, got %+v", rw.Decrypted)
	}
	entries, err := os.ReadDir(store.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected a single temp subdirectory, got %d", len(entries))
	}
}

func TestPatternMatchWithDetectFalse(t *testing.T) {
	dir := t.TempDir()
	mustCopy(t, dir, "secret.env", "enc/app.env")
	mustCopy(t, dir, "other.txt", "enc/secrets.json")
	store := newStore(t)
	argv := []string{"build", "--env-file", "secret.env", "--label", "other.txt", "."}
	rw, err := Plan(argv, store, Options{Detect: false, Patterns: []string{"*.env"}, Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if rw.Args[2] == "secret.env" {
		t.Fatalf("pattern match should have rewritten: %v", rw.Args)
	}
	if rw.Args[4] != "other.txt" {
		t.Errorf("non-matching pattern with Detect=false should be untouched: %v", rw.Args)
	}
	if len(rw.Decrypted) != 1 {
		t.Errorf("expected exactly one decryption: %+v", rw.Decrypted)
	}
}

func TestDecryptedSourcePreservesArgvSpelling(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	mustCopy(t, filepath.Join(dir, "sub"), "secrets.yaml", "enc/secrets.yaml")
	store := newStore(t)
	argv := []string{"build", "--env-file", "sub/secrets.yaml", "."}
	rw, err := Plan(argv, store, Options{Detect: true, Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(rw.Decrypted) != 1 {
		t.Fatalf("expected one decrypted entry: %+v", rw.Decrypted)
	}
	if rw.Decrypted[0].Source != "sub/secrets.yaml" {
		t.Errorf("source not preserved: %q", rw.Decrypted[0].Source)
	}
	if rw.Decrypted[0].Path != rw.Args[2] {
		t.Errorf("path mismatch: %q vs %q", rw.Decrypted[0].Path, rw.Args[2])
	}
	if filepath.Base(rw.Decrypted[0].Path) != "secrets.yaml" {
		t.Errorf("basename not preserved: %s", rw.Decrypted[0].Path)
	}
}

func TestArgv0NeverTouched(t *testing.T) {
	dir := t.TempDir()
	store := newStore(t)
	argv := []string{"build", "."}
	rw, err := Plan(argv, store, Options{Detect: true, Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if rw.Args[0] != "build" {
		t.Errorf("argv[0] mutated: %v", rw.Args)
	}
}

func TestFlagTakesValue(t *testing.T) {
	cases := []struct {
		tok  string
		want bool
	}{
		{"-d", false}, {"--detach", false},
		{"-i", false}, {"--interactive", false},
		{"-t", false}, {"--tty", false},
		{"--rm", false}, {"--privileged", false}, {"--init", false},
		{"--no-healthcheck", false}, {"--read-only", false},
		{"--sig-proxy", false}, {"--oom-kill-disable", false},
		{"-P", false}, {"--publish-all", false},
		{"-q", false}, {"--quiet", false}, {"--help", false},
		{"-it", false}, {"-dit", false}, {"-ti", false},
		{"--env-file", true}, {"--name", true}, {"--network", true}, {"-v", true},
		{"--env-file=x", false}, {"-e=FOO=bar", false},
	}
	for _, c := range cases {
		if got := flagTakesValue(c.tok); got != c.want {
			t.Errorf("flagTakesValue(%q) = %v, want %v", c.tok, got, c.want)
		}
	}
}

func TestContainerCutoff(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want int
	}{
		{"flag with value then image", []string{"run", "--env-file", "x", "alpine"}, 3},
		{"equals form flag then image", []string{"run", "--env-file=x", "alpine"}, 2},
		{"boolean then image", []string{"run", "--rm", "alpine"}, 2},
		{"combined short booleans then image", []string{"run", "-it", "alpine"}, 2},
		{"double dash stops scan", []string{"run", "--env-file", "x", "--", "alpine"}, 3},
		{"no positional found", []string{"run", "--rm"}, 2},
	}
	for _, c := range cases {
		if got := containerCutoff(c.argv); got != c.want {
			t.Errorf("%s: containerCutoff(%v) = %d, want %d", c.name, c.argv, got, c.want)
		}
	}
}
