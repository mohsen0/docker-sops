package composefix

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"gopkg.in/yaml.v3"

	"github.com/mohsen0/docker-sops/internal/tempstore"
)

const testdata = "../../testdata"

func TestMain(m *testing.M) {
	abs, err := filepath.Abs(filepath.Join(testdata, "age-test-key.txt"))
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("SOPS_AGE_KEY_FILE", abs)
	// Keep compose-go's default ".env" discovery out of the sample projects.
	_ = os.Unsetenv("COMPOSE_FILE")
	_ = os.Unsetenv("COMPOSE_PROJECT_NAME")
	os.Exit(m.Run())
}

func TestParseFlags(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		want    Flags
		wantIdx int
	}{
		{
			name:    "no flags",
			args:    []string{"up", "-d"},
			want:    Flags{},
			wantIdx: 0,
		},
		{
			name:    "file twice keeps order",
			args:    []string{"-f", "a.yaml", "-f", "b.yaml", "up"},
			want:    Flags{Files: []string{"a.yaml", "b.yaml"}},
			wantIdx: 4,
		},
		{
			name:    "file with equals",
			args:    []string{"--file=x.yaml", "config"},
			want:    Flags{Files: []string{"x.yaml"}},
			wantIdx: 1,
		},
		{
			name:    "short file attached value",
			args:    []string{"-fa.yaml", "up"},
			want:    Flags{Files: []string{"a.yaml"}},
			wantIdx: 1,
		},
		{
			name:    "short project name",
			args:    []string{"-p", "proj", "up"},
			want:    Flags{ProjectName: "proj"},
			wantIdx: 2,
		},
		{
			name:    "long project name",
			args:    []string{"--project-name", "proj", "ps"},
			want:    Flags{ProjectName: "proj"},
			wantIdx: 2,
		},
		{
			name:    "profile repeated",
			args:    []string{"--profile", "a", "--profile=b", "up"},
			want:    Flags{Profiles: []string{"a", "b"}},
			wantIdx: 3,
		},
		{
			name:    "env file",
			args:    []string{"--env-file", ".env.enc", "up"},
			want:    Flags{EnvFiles: []string{".env.enc"}},
			wantIdx: 2,
		},
		{
			name:    "project directory",
			args:    []string{"--project-directory", "sub", "up"},
			want:    Flags{ProjectDirectory: "sub"},
			wantIdx: 2,
		},
		{
			name:    "unknown bool flag before subcommand",
			args:    []string{"--compatibility", "-f", "a.yaml", "up"},
			want:    Flags{Files: []string{"a.yaml"}},
			wantIdx: 3,
		},
		{
			name:    "unknown long flag with equals",
			args:    []string{"--dry-run=true", "up"},
			want:    Flags{},
			wantIdx: 1,
		},
		{
			name:    "known value flags we ignore",
			args:    []string{"--parallel", "2", "--ansi", "never", "--progress", "plain", "up", "-d"},
			want:    Flags{},
			wantIdx: 6,
		},
		{
			name:    "no subcommand",
			args:    []string{"-f", "a.yaml"},
			want:    Flags{Files: []string{"a.yaml"}},
			wantIdx: -1,
		},
		{
			name: "everything together",
			args: []string{"-f", "a.yaml", "--env-file", ".env", "--profile", "dev", "-p", "proj", "--project-directory", "d", "up", "--build"},
			want: Flags{
				Files:            []string{"a.yaml"},
				EnvFiles:         []string{".env"},
				Profiles:         []string{"dev"},
				ProjectName:      "proj",
				ProjectDirectory: "d",
			},
			wantIdx: 10,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orig := append([]string(nil), tc.args...)
			got, idx := ParseFlags(tc.args)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("flags = %+v, want %+v", got, tc.want)
			}
			if idx != tc.wantIdx {
				t.Errorf("subcommandIndex = %d, want %d", idx, tc.wantIdx)
			}
			if !reflect.DeepEqual(tc.args, orig) {
				t.Errorf("args mutated: %v, want %v", tc.args, orig)
			}
		})
	}
}

// project builds a sample project directory. files maps a relative path to
// either literal content or, when prefixed with "@", a fixture path under
// testdata that is copied.
func project(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		data := []byte(content)
		if strings.HasPrefix(content, "@") {
			var err error
			data, err = os.ReadFile(filepath.Join(testdata, strings.TrimPrefix(content, "@")))
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
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

func run(t *testing.T, dir string, files ...string) ([]byte, []Ref) {
	t.Helper()
	yamlBytes, refs, err := Override(context.Background(), Flags{Cwd: dir, Files: files}, newStore(t), nil)
	if err != nil {
		t.Fatalf("Override: %v", err)
	}
	return yamlBytes, refs
}

func fixture(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testdata, rel))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(string(data), "\n")
}

func TestOverrideSingleEncryptedEnvFile(t *testing.T) {
	dir := project(t, map[string]string{
		"compose.yaml": "services:\n  web:\n    image: nginx\n    env_file:\n      - secrets.env\n",
		"secrets.env":  "@enc/app.env",
	})
	out, refs := run(t, dir)
	if len(refs) != 1 {
		t.Fatalf("refs = %+v, want 1", refs)
	}
	ref := refs[0]
	if ref.Kind != "env_file" || ref.Service != "web" {
		t.Errorf("ref = %+v, want kind env_file service web", ref)
	}
	if want := filepath.Join(dir, "secrets.env"); ref.Source != want {
		t.Errorf("Source = %q, want %q", ref.Source, want)
	}
	if !filepath.IsAbs(ref.Path) {
		t.Errorf("Path = %q, want absolute", ref.Path)
	}
	if !strings.Contains(string(out), "env_file: !override") {
		t.Errorf("override YAML lacks the !override tag:\n%s", out)
	}
	if !strings.Contains(string(out), ref.Path) {
		t.Errorf("override YAML lacks the temp path %q:\n%s", ref.Path, out)
	}
}

func TestOverrideDecryptedContentIsPlaintext(t *testing.T) {
	dir := project(t, map[string]string{
		"compose.yaml": "services:\n  web:\n    image: nginx\n    env_file:\n      - secrets.env\n",
		"secrets.env":  "@enc/app.env",
	})
	_, refs := run(t, dir)
	data, err := os.ReadFile(refs[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if want := fixture(t, "plain/app.env"); strings.TrimRight(string(data), "\n") != want {
		t.Errorf("decrypted content = %q, want %q", data, want)
	}
}

func TestOverridePreservesOrderAndPlainEntries(t *testing.T) {
	dir := project(t, map[string]string{
		"compose.yaml": "services:\n  web:\n    image: nginx\n    env_file:\n      - plain.env\n      - secrets.env\n      - other.env\n",
		"plain.env":    "@plain/app.env",
		"other.env":    "OTHER=1\n",
		"secrets.env":  "@enc/app.env",
	})
	out, refs := run(t, dir)
	if len(refs) != 1 {
		t.Fatalf("refs = %+v, want 1", refs)
	}
	want := []string{
		filepath.Join(dir, "plain.env"),
		refs[0].Path,
		filepath.Join(dir, "other.env"),
	}
	if got := envFilePaths(t, out, "web"); !reflect.DeepEqual(got, want) {
		t.Errorf("env_file = %v, want %v", got, want)
	}
}

func TestOverridePreservesRequiredFalse(t *testing.T) {
	dir := project(t, map[string]string{
		"compose.yaml": "services:\n  web:\n    image: nginx\n    env_file:\n      - path: secrets.env\n        required: false\n",
		"secrets.env":  "@enc/app.env",
	})
	out, refs := run(t, dir)
	if len(refs) != 1 {
		t.Fatalf("refs = %+v, want 1", refs)
	}
	if !strings.Contains(string(out), "required: false") {
		t.Errorf("override YAML lost required:false:\n%s", out)
	}
	// Reloading proves compose-go reads it back the same way.
	p := reload(t, dir, out)
	ef := p.Services["web"].EnvFiles
	if len(ef) != 1 {
		t.Fatalf("env_file = %+v, want 1 entry", ef)
	}
	if ef[0].Path != refs[0].Path {
		t.Errorf("path = %q, want %q", ef[0].Path, refs[0].Path)
	}
	if bool(ef[0].Required) {
		t.Errorf("required = true, want false")
	}
}

func TestOverrideEncryptedSecretFile(t *testing.T) {
	dir := project(t, map[string]string{
		"compose.yaml":    "services:\n  web:\n    image: nginx\n    secrets:\n      - db_password\nsecrets:\n  db_password:\n    file: db_password.enc\n",
		"db_password.enc": "@enc/secrets.yaml",
	})
	out, refs := run(t, dir)
	if len(refs) != 1 {
		t.Fatalf("refs = %+v, want 1", refs)
	}
	if refs[0].Kind != "secret" || refs[0].Name != "db_password" {
		t.Errorf("ref = %+v, want kind secret name db_password", refs[0])
	}
	if strings.Contains(string(out), "services:") {
		t.Errorf("override YAML should not mention services:\n%s", out)
	}
	p := reload(t, dir, out)
	if got := p.Secrets["db_password"].File; got != "" {
		t.Errorf("secret file = %q, want it cleared", got)
	}
	if got := p.Secrets["db_password"].Environment; got != "DOCKER_SOPS_SECRET_db_password" {
		t.Errorf("secret environment = %q", got)
	}
	if refs[0].Env != "DOCKER_SOPS_SECRET_db_password" || refs[0].Path != "" {
		t.Errorf("ref = %+v, want Env set and no temp path", refs[0])
	}
}

func TestBuildInjectsSecretPlaintextIntoEnv(t *testing.T) {
	dir := project(t, map[string]string{
		"compose.yaml":    "services:\n  web:\n    image: nginx\nsecrets:\n  db_password:\n    file: db_password.enc\n",
		"db_password.enc": "@enc/secrets.yaml",
	})
	res, err := Build(context.Background(), Flags{Cwd: dir}, newStore(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Env) != 1 || !strings.HasPrefix(res.Env[0], "DOCKER_SOPS_SECRET_db_password=") || !strings.Contains(res.Env[0], "s3cr3t-yaml") {
		t.Fatalf("env = %q", res.Env)
	}
	if strings.Contains(string(res.YAML), "s3cr3t") {
		t.Fatalf("plaintext leaked into the override:\n%s", res.YAML)
	}
}

func TestBuildBinarySecretFallsBackToFile(t *testing.T) {
	dir := project(t, map[string]string{
		"compose.yaml": "services:\n  web:\n    image: nginx\nsecrets:\n  blob:\n    file: blob.bin\n",
		"blob.bin":     "@enc/blob-nul.bin",
	})
	res, err := Build(context.Background(), Flags{Cwd: dir}, newStore(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Refs) != 1 || res.Refs[0].Env != "" || res.Refs[0].Path == "" {
		t.Fatalf("refs = %+v, want file fallback", res.Refs)
	}
	if len(res.Env) != 0 {
		t.Fatalf("env = %q, want none", res.Env)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "blob") {
		t.Fatalf("warnings = %q", res.Warnings)
	}
	p := reload(t, dir, res.YAML)
	if p.Secrets["blob"].File != res.Refs[0].Path {
		t.Errorf("secret file = %q, want %q", p.Secrets["blob"].File, res.Refs[0].Path)
	}
}

func TestOverrideEncryptedConfigFile(t *testing.T) {
	dir := project(t, map[string]string{
		"compose.yaml": "services:\n  web:\n    image: nginx\nconfigs:\n  app_cfg:\n    file: app.ini\n",
		"app.ini":      "@enc/settings.ini",
	})
	out, refs := run(t, dir)
	if len(refs) != 1 {
		t.Fatalf("refs = %+v, want 1", refs)
	}
	if refs[0].Kind != "config" || refs[0].Name != "app_cfg" {
		t.Errorf("ref = %+v, want kind config name app_cfg", refs[0])
	}
	p := reload(t, dir, out)
	if got := p.Configs["app_cfg"].File; got != "" {
		t.Errorf("config file = %q, want it cleared", got)
	}
	if got := p.Configs["app_cfg"].Environment; got != "DOCKER_SOPS_CONFIG_app_cfg" {
		t.Errorf("config environment = %q", got)
	}
}

func TestOverrideIgnoresExternalSecret(t *testing.T) {
	dir := project(t, map[string]string{
		"compose.yaml": "services:\n  web:\n    image: nginx\nsecrets:\n  ext:\n    external: true\n",
	})
	out, refs := run(t, dir)
	if out != nil || refs != nil {
		t.Errorf("Override = (%q, %+v), want nil, nil", out, refs)
	}
}

func TestOverrideIgnoresEnvironmentSecret(t *testing.T) {
	t.Setenv("DB_PASSWORD", "from-env")
	dir := project(t, map[string]string{
		"compose.yaml": "services:\n  web:\n    image: nginx\nsecrets:\n  db:\n    environment: DB_PASSWORD\n",
	})
	out, refs := run(t, dir)
	if out != nil || refs != nil {
		t.Errorf("Override = (%q, %+v), want nil, nil", out, refs)
	}
}

func TestOverrideNothingEncrypted(t *testing.T) {
	dir := project(t, map[string]string{
		"compose.yaml": "services:\n  web:\n    image: nginx\n    env_file:\n      - plain.env\nsecrets:\n  s:\n    file: plain.yaml\n",
		"plain.env":    "@plain/app.env",
		"plain.yaml":   "@plain/secrets.yaml",
	})
	out, refs := run(t, dir)
	if out != nil || refs != nil {
		t.Errorf("Override = (%q, %+v), want nil, nil", out, refs)
	}
}

func TestOverrideSharedEnvFileDecryptedOnce(t *testing.T) {
	dir := project(t, map[string]string{
		"compose.yaml": "services:\n  web:\n    image: nginx\n    env_file:\n      - secrets.env\n  api:\n    image: nginx\n    env_file:\n      - secrets.env\n",
		"secrets.env":  "@enc/app.env",
	})
	out, refs := run(t, dir)
	if len(refs) != 2 {
		t.Fatalf("refs = %+v, want 2", refs)
	}
	if refs[0].Path != refs[1].Path {
		t.Errorf("paths = %q and %q, want one decrypted copy", refs[0].Path, refs[1].Path)
	}
	for _, svc := range []string{"web", "api"} {
		if got := envFilePaths(t, out, svc); !reflect.DeepEqual(got, []string{refs[0].Path}) {
			t.Errorf("%s env_file = %v, want [%s]", svc, got, refs[0].Path)
		}
	}
}

func TestOverrideSkipsServicesWithoutEncryptedEnvFile(t *testing.T) {
	dir := project(t, map[string]string{
		"compose.yaml": "services:\n  web:\n    image: nginx\n    env_file:\n      - secrets.env\n  api:\n    image: nginx\n    env_file:\n      - plain.env\n",
		"secrets.env":  "@enc/app.env",
		"plain.env":    "@plain/app.env",
	})
	out, _ := run(t, dir)
	if strings.Contains(string(out), "api:") {
		t.Errorf("override YAML should not include service api:\n%s", out)
	}
}

func TestOverrideLoadedWithOriginalYieldsTempPaths(t *testing.T) {
	dir := project(t, map[string]string{
		"compose.yaml":    "services:\n  web:\n    image: nginx\n    env_file:\n      - plain.env\n      - secrets.env\n    secrets:\n      - db_password\nsecrets:\n  db_password:\n    file: db_password.enc\n",
		"plain.env":       "@plain/app.env",
		"secrets.env":     "@enc/app.env",
		"db_password.enc": "@enc/secrets.yaml",
	})
	out, refs := run(t, dir)
	if len(refs) != 2 {
		t.Fatalf("refs = %+v, want 2", refs)
	}
	var envRef, secretRef Ref
	for _, r := range refs {
		switch r.Kind {
		case "env_file":
			envRef = r
		case "secret":
			secretRef = r
		}
	}
	p := reload(t, dir, out)
	gotPaths := []string{}
	for _, ef := range p.Services["web"].EnvFiles {
		gotPaths = append(gotPaths, ef.Path)
	}
	want := []string{filepath.Join(dir, "plain.env"), envRef.Path}
	if !reflect.DeepEqual(gotPaths, want) {
		t.Errorf("merged env_file = %v, want %v", gotPaths, want)
	}
	if got := p.Secrets["db_password"].Environment; got != secretRef.Env || got == "" {
		t.Errorf("merged secret environment = %q, want %q", got, secretRef.Env)
	}
}

func TestOverrideKeepsOtherSecretAttributesOnMerge(t *testing.T) {
	dir := project(t, map[string]string{
		"compose.yaml":    "services:\n  web:\n    image: nginx\nsecrets:\n  db_password:\n    file: db_password.enc\n    name: fancy_name\n",
		"db_password.enc": "@enc/secrets.yaml",
	})
	out, refs := run(t, dir)
	p := reload(t, dir, out)
	if got := p.Secrets["db_password"].Name; got != "fancy_name" {
		t.Errorf("secret name = %q, want fancy_name", got)
	}
	if got := p.Secrets["db_password"].File; got != "" {
		t.Errorf("secret file = %q, want it cleared", got)
	}
	if got := p.Secrets["db_password"].Environment; got != refs[0].Env {
		t.Errorf("secret environment = %q, want %q", got, refs[0].Env)
	}
}

func TestOverrideProjectNameFromFlag(t *testing.T) {
	dir := project(t, map[string]string{
		"compose.yaml":  "services:\n  web:\n    image: nginx\n    env_file:\n      - ${COMPOSE_PROJECT_NAME}.env\n",
		"myproject.env": "@enc/app.env",
	})
	out, refs, err := Override(context.Background(), Flags{Cwd: dir, ProjectName: "myproject"}, newStore(t), nil)
	if err != nil {
		t.Fatalf("Override: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("refs = %+v, want 1", refs)
	}
	if want := filepath.Join(dir, "myproject.env"); refs[0].Source != want {
		t.Errorf("Source = %q, want %q", refs[0].Source, want)
	}
	if !strings.Contains(string(out), refs[0].Path) {
		t.Errorf("override YAML lacks %q:\n%s", refs[0].Path, out)
	}
}

func TestOverrideExplicitFilesAndProfiles(t *testing.T) {
	dir := project(t, map[string]string{
		"stack.yaml":  "services:\n  web:\n    image: nginx\n    profiles: [dev]\n    env_file:\n      - secrets.env\n",
		"secrets.env": "@enc/app.env",
	})
	// Without the profile the service is disabled, so nothing is encrypted.
	out, refs, err := Override(context.Background(), Flags{Cwd: dir, Files: []string{"stack.yaml"}}, newStore(t), nil)
	if err != nil {
		t.Fatalf("Override: %v", err)
	}
	if out != nil || refs != nil {
		t.Errorf("Override without profile = (%q, %+v), want nil, nil", out, refs)
	}
	out, refs, err = Override(context.Background(), Flags{Cwd: dir, Files: []string{"stack.yaml"}, Profiles: []string{"dev"}}, newStore(t), nil)
	if err != nil {
		t.Fatalf("Override: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("refs = %+v, want 1", refs)
	}
	if !strings.Contains(string(out), "web:") {
		t.Errorf("override YAML lacks service web:\n%s", out)
	}
}

func TestOverrideEnvFileInterpolation(t *testing.T) {
	dir := project(t, map[string]string{
		"compose.yaml": "services:\n  web:\n    image: nginx\n    env_file:\n      - ${SECRET_FILE}\n",
		"secrets.env":  "@enc/app.env",
		"vars.env":     "SECRET_FILE=secrets.env\n",
	})
	out, refs, err := Override(context.Background(), Flags{Cwd: dir, EnvFiles: []string{filepath.Join(dir, "vars.env")}}, newStore(t), nil)
	if err != nil {
		t.Fatalf("Override: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("refs = %+v, want 1", refs)
	}
	if want := filepath.Join(dir, "secrets.env"); refs[0].Source != want {
		t.Errorf("Source = %q, want %q", refs[0].Source, want)
	}
	if !strings.Contains(string(out), refs[0].Path) {
		t.Errorf("override YAML lacks %q:\n%s", refs[0].Path, out)
	}
}

func TestOverridePatternTreatsFileAsEncrypted(t *testing.T) {
	dir := project(t, map[string]string{
		"compose.yaml": "services:\n  web:\n    image: nginx\n    env_file:\n      - app.enc.env\n",
		"app.enc.env":  "@enc/app.env",
	})
	out, refs, err := Override(context.Background(), Flags{Cwd: dir}, newStore(t), []string{"*.enc.env"})
	if err != nil {
		t.Fatalf("Override: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("refs = %+v, want 1", refs)
	}
	if !strings.Contains(string(out), refs[0].Path) {
		t.Errorf("override YAML lacks %q:\n%s", refs[0].Path, out)
	}
}

func TestOverridePatternOnPlainFileFails(t *testing.T) {
	dir := project(t, map[string]string{
		"compose.yaml": "services:\n  web:\n    image: nginx\n    env_file:\n      - app.enc.env\n",
		"app.enc.env":  "@plain/app.env",
	})
	_, _, err := Override(context.Background(), Flags{Cwd: dir}, newStore(t), []string{"*.enc.env"})
	if err == nil {
		t.Fatal("expected an error: the pattern claims the file is encrypted")
	}
	if !strings.Contains(err.Error(), "app.enc.env") {
		t.Errorf("error = %v, want it to name the file", err)
	}
}

func TestOverrideProjectDirectory(t *testing.T) {
	dir := project(t, map[string]string{
		"deploy/compose.yaml": "services:\n  web:\n    image: nginx\n    env_file:\n      - secrets.env\n",
		"secrets.env":         "@enc/app.env",
	})
	out, refs, err := Override(context.Background(), Flags{
		Cwd:              dir,
		Files:            []string{"deploy/compose.yaml"},
		ProjectDirectory: ".",
	}, newStore(t), nil)
	if err != nil {
		t.Fatalf("Override: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("refs = %+v, want 1", refs)
	}
	if want := filepath.Join(dir, "secrets.env"); refs[0].Source != want {
		t.Errorf("Source = %q, want %q", refs[0].Source, want)
	}
	if !strings.Contains(string(out), refs[0].Path) {
		t.Errorf("override YAML lacks %q:\n%s", refs[0].Path, out)
	}
}

func TestOverrideMissingProjectFails(t *testing.T) {
	dir := t.TempDir()
	_, _, err := Override(context.Background(), Flags{Cwd: dir, Files: []string{"nope.yaml"}}, newStore(t), nil)
	if err == nil {
		t.Fatal("expected an error for a missing compose file")
	}
}

// reload writes the override next to the original project and loads both with
// compose-go, exactly as `docker compose -f compose.yaml -f override.yaml`
// would.
func reload(t *testing.T, dir string, override []byte) *types.Project {
	t.Helper()
	path := filepath.Join(t.TempDir(), "docker-sops-override.yaml")
	if err := os.WriteFile(path, override, 0o600); err != nil {
		t.Fatal(err)
	}
	opts, err := cli.NewProjectOptions(
		[]string{filepath.Join(dir, "compose.yaml"), path},
		cli.WithWorkingDirectory(dir),
		cli.WithOsEnv,
		cli.WithName("test"),
	)
	if err != nil {
		t.Fatal(err)
	}
	p, err := cli.ProjectFromOptions(context.Background(), opts)
	if err != nil {
		t.Fatalf("reload merged project: %v", err)
	}
	return p
}

// envFilePaths extracts the env_file paths the override declares for a
// service, accepting both the short (string) and long (mapping) entry forms.
func envFilePaths(t *testing.T, override []byte, service string) []string {
	t.Helper()
	var doc struct {
		Services map[string]struct {
			EnvFile []any `yaml:"env_file"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(override, &doc); err != nil {
		t.Fatalf("unmarshal override: %v\n%s", err, override)
	}
	svc, ok := doc.Services[service]
	if !ok {
		t.Fatalf("override has no service %q:\n%s", service, override)
	}
	paths := []string{}
	for _, e := range svc.EnvFile {
		switch v := e.(type) {
		case string:
			paths = append(paths, v)
		case map[string]any:
			p, _ := v["path"].(string)
			paths = append(paths, p)
		default:
			t.Fatalf("unexpected env_file entry %T in:\n%s", e, override)
		}
	}
	return paths
}

func TestBuildReportsDiscoveredComposeFiles(t *testing.T) {
	dir := project(t, map[string]string{
		"secrets.env":           "@enc/app.env",
		"compose.yaml":          "services:\n  web:\n    image: alpine\n    env_file: [secrets.env]\n",
		"compose.override.yaml": "services:\n  web:\n    environment: [X=1]\n",
	})
	res, err := Build(context.Background(), Flags{Cwd: dir}, newStore(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "compose.yaml"), filepath.Join(dir, "compose.override.yaml")}
	if len(res.ComposeFiles) != 2 || res.ComposeFiles[0] != want[0] || res.ComposeFiles[1] != want[1] {
		t.Fatalf("ComposeFiles = %v, want %v", res.ComposeFiles, want)
	}
	if res.YAML == nil || len(res.Refs) != 1 {
		t.Fatalf("expected override with one ref, got %q %v", res.YAML, res.Refs)
	}
}

func TestBuildOmitsDerivedSecretName(t *testing.T) {
	dir := project(t, map[string]string{
		"compose.yaml":    "services:\n  web:\n    image: nginx\nsecrets:\n  db_password:\n    file: db_password.enc\n",
		"db_password.enc": "@enc/secrets.yaml",
	})
	res, err := Build(context.Background(), Flags{Cwd: dir, ProjectName: "p1"}, newStore(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(res.YAML), "name:") {
		t.Fatalf("derived name must not be pinned in the override:\n%s", res.YAML)
	}
}
