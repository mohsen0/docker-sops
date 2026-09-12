// Package composefix loads the Compose project a `docker compose` invocation
// is about to run, decrypts every sops-encrypted `env_file`, `secrets.*.file`
// and `configs.*.file` it references, and emits an override Compose file that
// points at the decrypted copies. The user's own files are never modified.
package composefix

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"gopkg.in/yaml.v3"

	"github.com/mohsen0/docker-sops/internal/sopsfile"
	"github.com/mohsen0/docker-sops/internal/tempstore"
)

// Ref kinds reported by Override.
const (
	KindEnvFile = "env_file"
	KindSecret  = "secret"
	KindConfig  = "config"
)

// Flags are the docker compose global flags that affect which project is
// loaded.
type Flags struct {
	Files            []string // -f/--file, in order; empty means compose-go's default discovery
	EnvFiles         []string // --env-file (already decrypted by the caller)
	Profiles         []string // --profile
	ProjectName      string   // -p/--project-name
	ProjectDirectory string   // --project-directory
	Cwd              string   // working dir for discovery/relative paths; empty = os.Getwd()
}

// valueFlags are the docker compose global flags that consume the following
// argument. Any other token that looks like a flag is treated as a boolean, so
// the parser never has to be kept in sync with the rest of docker compose.
var valueFlags = map[string]bool{
	"--ansi":              true,
	"--env-file":          true,
	"--file":              true,
	"--parallel":          true,
	"--profile":           true,
	"--progress":          true,
	"--project-directory": true,
	"--project-name":      true,
}

// shortFlags maps the shorthand forms to their long names.
var shortFlags = map[byte]string{
	'f': "--file",
	'p': "--project-name",
}

// ParseFlags extracts the project-selecting global flags from a compose argv
// (everything after the word "compose", e.g. ["-f","a.yaml","up","-d"]) and
// returns the index of the compose subcommand, or -1 when there is none. It
// recognises both "--flag value" and "--flag=value", ignores flags it does not
// know, and leaves args untouched.
func ParseFlags(args []string) (Flags, int) {
	var f Flags
	for i := 0; i < len(args); i++ {
		tok := args[i]
		switch {
		case tok == "--":
			if i+1 < len(args) {
				return f, i + 1
			}
			return f, -1
		case tok == "-" || !strings.HasPrefix(tok, "-"):
			return f, i
		case strings.HasPrefix(tok, "--"):
			name, value, hasValue := strings.Cut(tok, "=")
			if !valueFlags[name] {
				continue // unknown, or a boolean flag: nothing to consume
			}
			if !hasValue {
				i++
				if i >= len(args) {
					return f, -1
				}
				value = args[i]
			}
			f.set(name, value)
		default:
			// A shorthand cluster such as "-f", "-fa.yaml" or "-df a.yaml".
			// The first shorthand that takes a value consumes the rest of the
			// token, or the next argument when the token ends there.
			cluster := tok[1:]
			for j := 0; j < len(cluster); j++ {
				name := shortFlags[cluster[j]]
				if name == "" {
					continue // boolean shorthand
				}
				value := strings.TrimPrefix(cluster[j+1:], "=")
				if cluster[j+1:] == "" {
					i++
					if i >= len(args) {
						return f, -1
					}
					value = args[i]
				}
				f.set(name, value)
				break
			}
		}
	}
	return f, -1
}

// set records one recognised flag. The flags whose values do not change which
// project is loaded (--ansi, --parallel, --progress) are consumed and dropped.
func (f *Flags) set(name, value string) {
	switch name {
	case "--file":
		f.Files = append(f.Files, value)
	case "--env-file":
		f.EnvFiles = append(f.EnvFiles, value)
	case "--profile":
		f.Profiles = append(f.Profiles, value)
	case "--project-name":
		f.ProjectName = value
	case "--project-directory":
		f.ProjectDirectory = value
	}
}

// Ref is one Compose reference that was redirected to a decrypted copy.
type Ref struct {
	Kind    string // "env_file", "secret", "config"
	Service string // for env_file
	Name    string // secret/config name
	Source  string // resolved absolute path of the encrypted file
	Path    string // decrypted temp path (empty when Env is used instead)
	Env     string // environment variable carrying the plaintext (secrets/configs)
}

// Result is the outcome of Build.
type Result struct {
	// YAML is the override compose file, or nil when nothing was encrypted.
	YAML []byte
	// Refs lists the decrypted files.
	Refs []Ref
	// Env holds NAME=value entries that must be present in the environment
	// of the docker compose process: encrypted secrets and configs are
	// turned into environment-sourced entries so their plaintext never
	// touches disk and does not depend on the temp store's lifetime.
	Env []string
	// Warnings are human-readable notes about entries that had to fall back
	// to a decrypted temp file (binary or oversized content).
	Warnings []string
	// ComposeFiles are the resolved compose files the project was loaded
	// from, including files found by default discovery. Callers that add
	// "-f override" must also pass these explicitly, because any -f flag
	// disables Compose's default discovery.
	ComposeFiles []string
}

// Override is Build without the Result wrapper: it returns the override YAML
// (nil when nothing is encrypted) and the decrypted refs.
func Override(ctx context.Context, flags Flags, store *tempstore.Store, patterns []string) ([]byte, []Ref, error) {
	res, err := Build(ctx, flags, store, patterns)
	if err != nil {
		return nil, nil, err
	}
	return res.YAML, res.Refs, nil
}

// Override loads the project described by flags, decrypts every encrypted
// env_file / secret file / config file into store, and returns the YAML of an
// override Compose file that redirects them, together with the references it
// rewrote. When nothing is encrypted it returns nil, nil, nil so the caller can
// skip the override entirely.
//
// patterns are additive basename globs (e.g. "*.enc.*"): a file whose basename
// matches one is decrypted even if content detection did not flag it, and
// failing to decrypt it is an error.
func Build(ctx context.Context, flags Flags, store *tempstore.Store, patterns []string) (*Result, error) {
	project, err := loadProject(ctx, flags)
	if err != nil {
		return nil, err
	}
	d := &decryptor{store: store, patterns: patterns, plain: map[string]string{}, projectName: project.Name}

	var refs []Ref
	services := mappingNode()
	for _, name := range slices.Sorted(maps.Keys(project.Services)) {
		entries := project.Services[name].EnvFiles
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: overrideTag}
		encrypted := false
		for _, entry := range entries {
			path := entry.Path
			plain, ok, err := d.file(path)
			if err != nil {
				return nil, err
			}
			if ok {
				encrypted = true
				refs = append(refs, Ref{Kind: KindEnvFile, Service: name, Source: path, Path: plain})
				path = plain
			}
			seq.Content = append(seq.Content, envFileNode(path, entry))
		}
		if !encrypted {
			continue // nothing to redirect: leave the service alone
		}
		svc := mappingNode()
		put(svc, "env_file", seq)
		put(services, name, svc)
	}

	secrets, secretRefs, err := d.fileObjects(KindSecret, fileObjects(project.Secrets))
	if err != nil {
		return nil, err
	}
	refs = append(refs, secretRefs...)

	configs, configRefs, err := d.fileObjects(KindConfig, fileObjects(project.Configs))
	if err != nil {
		return nil, err
	}
	refs = append(refs, configRefs...)

	if len(refs) == 0 {
		return &Result{ComposeFiles: project.ComposeFiles}, nil
	}
	env := append([]string(nil), d.env...)
	warnings := append([]string(nil), d.warnings...)

	root := mappingNode()
	for _, section := range []struct {
		name string
		node *yaml.Node
	}{{"services", services}, {"secrets", secrets}, {"configs", configs}} {
		if len(section.node.Content) > 0 {
			put(root, section.name, section.node)
		}
	}
	out, err := marshal(root)
	if err != nil {
		return nil, err
	}
	// The tag is what stops Compose from appending our env_file list to the
	// original one, so never hand back an override that lost it.
	if len(services.Content) > 0 && !bytes.Contains(out, []byte("env_file: "+overrideTag)) {
		return nil, fmt.Errorf("override file lost the %s tag:\n%s", overrideTag, out)
	}
	return &Result{YAML: out, Refs: refs, Env: env, Warnings: warnings, ComposeFiles: project.ComposeFiles}, nil
}

// loadProject loads the Compose project exactly as docker compose would for
// the same global flags.
func loadProject(ctx context.Context, flags Flags) (*types.Project, error) {
	cwd := flags.Cwd
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		cwd = wd
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}

	configs := make([]string, 0, len(flags.Files))
	for _, f := range flags.Files {
		configs = append(configs, resolve(cwd, f))
	}
	envFiles := make([]string, 0, len(flags.EnvFiles))
	for _, f := range flags.EnvFiles {
		envFiles = append(envFiles, resolve(cwd, f))
	}

	// compose-go's default discovery and its relative-path resolution both
	// start from ProjectOptions.WorkingDir, which defaults to the process
	// working directory - not necessarily our Cwd. Pin it when the user gave
	// --project-directory, and when there is no -f to derive it from.
	workingDir := ""
	switch {
	case flags.ProjectDirectory != "":
		workingDir = resolve(cwd, flags.ProjectDirectory)
	case len(configs) == 0:
		if _, ok := os.LookupEnv("COMPOSE_FILE"); !ok {
			workingDir = discover(cwd)
		}
	}

	opts := []cli.ProjectOptionsFn{
		cli.WithWorkingDirectory(workingDir),
		cli.WithOsEnv,
		cli.WithEnvFiles(envFiles...),
		cli.WithDotEnv,
		cli.WithConfigFileEnv,
		cli.WithDefaultConfigPath,
		cli.WithProfiles(flags.Profiles),
	}
	if flags.ProjectName != "" {
		opts = append(opts, cli.WithName(flags.ProjectName))
	}
	options, err := cli.NewProjectOptions(configs, opts...)
	if err != nil {
		return nil, err
	}
	return cli.ProjectFromOptions(ctx, options)
}

// resolve makes a user-supplied path absolute against cwd, leaving the stdin
// marker alone.
func resolve(cwd, path string) string {
	if path == "-" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(cwd, path)
}

// discover walks up from dir looking for a default Compose file name, and
// returns the directory holding it - the project directory docker compose
// would pick. It returns dir when nothing is found, so the caller still gets
// compose-go's "no configuration file" error rather than a surprise project.
func discover(dir string) string {
	for {
		for _, name := range cli.DefaultFileNames {
			if fi, err := os.Stat(filepath.Join(dir, name)); err == nil && !fi.IsDir() {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// fileObject is the subset of a top-level secret/config entry we care about.
type fileObject struct {
	name     string
	file     string
	external bool
	cfg      types.FileObjectConfig
}

// fileObjects flattens a Secrets or Configs map into a name-sorted slice.
func fileObjects[T ~map[string]E, E any](m T) []fileObject {
	out := make([]fileObject, 0, len(m))
	for _, name := range slices.Sorted(maps.Keys(m)) {
		var obj types.FileObjectConfig
		switch v := any(m[name]).(type) {
		case types.SecretConfig:
			obj = types.FileObjectConfig(v)
		case types.ConfigObjConfig:
			obj = types.FileObjectConfig(v)
		default:
			continue
		}
		out = append(out, fileObject{name: name, file: obj.File, external: bool(obj.External), cfg: obj})
	}
	return out
}

// decryptor decrypts each distinct source file at most once.
type decryptor struct {
	store    *tempstore.Store
	patterns []string
	plain    map[string]string // source path -> decrypted temp path
	data     map[string][]byte // source path -> plaintext
	env      []string          // NAME=value for environment-sourced entries
	envNames map[string]bool
	warnings []string
	// projectName lets fileObjects tell a user-set secret name from the
	// "<project>_<key>" name the loader derives.
	projectName string
}

// envValueLimit is the largest plaintext handed over as an environment
// variable; larger or binary content falls back to a decrypted temp file.
const envValueLimit = 64 * 1024

// fileObjects returns the override mapping for the encrypted entries among
// objs. Text content becomes an environment-sourced entry (tagged !override
// so the original file: key is dropped) whose value is queued in d.env;
// binary or oversized content falls back to a decrypted temp file, keeping
// the file: key only so other attributes merge.
func (d *decryptor) fileObjects(kind string, objs []fileObject) (*yaml.Node, []Ref, error) {
	node := mappingNode()
	var refs []Ref
	for _, obj := range objs {
		// external and environment-backed entries carry no file to decrypt.
		if obj.file == "" || obj.external {
			continue
		}
		data, ok, err := d.plaintext(obj.file)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			continue
		}
		if bytes.IndexByte(data, 0) >= 0 || len(data) > envValueLimit {
			plain, _, err := d.file(obj.file)
			if err != nil {
				return nil, nil, err
			}
			d.warnings = append(d.warnings, fmt.Sprintf("%s %q is binary or larger than %d bytes; it is bind-mounted from a decrypted temp file that is removed when this command exits", kind, obj.name, envValueLimit))
			refs = append(refs, Ref{Kind: kind, Name: obj.name, Source: obj.file, Path: plain})
			entry := mappingNode()
			put(entry, "file", scalarNode(plain))
			put(node, obj.name, entry)
			continue
		}
		envName := d.envName(kind, obj.name)
		d.env = append(d.env, envName+"="+string(data))
		refs = append(refs, Ref{Kind: kind, Name: obj.name, Source: obj.file, Env: envName})
		entry := mappingNode()
		entry.Tag = overrideTag
		put(entry, "environment", scalarNode(envName))
		if obj.cfg.Name != "" && obj.cfg.Name != d.projectName+"_"+obj.name {
			put(entry, "name", scalarNode(obj.cfg.Name))
		}
		if obj.cfg.Driver != "" {
			put(entry, "driver", scalarNode(obj.cfg.Driver))
		}
		if obj.cfg.TemplateDriver != "" {
			put(entry, "template_driver", scalarNode(obj.cfg.TemplateDriver))
		}
		if len(obj.cfg.DriverOpts) > 0 {
			put(entry, "driver_opts", stringMapNode(obj.cfg.DriverOpts))
		}
		if len(obj.cfg.Labels) > 0 {
			put(entry, "labels", stringMapNode(obj.cfg.Labels))
		}
		put(node, obj.name, entry)
	}
	return node, refs, nil
}

// envName derives a unique environment variable name for a secret/config.
func (d *decryptor) envName(kind, name string) string {
	prefix := "DOCKER_SOPS_SECRET_"
	if kind == KindConfig {
		prefix = "DOCKER_SOPS_CONFIG_"
	}
	base := prefix + strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			return r
		}
		return '_'
	}, name)
	if d.envNames == nil {
		d.envNames = map[string]bool{}
	}
	candidate := base
	for i := 2; d.envNames[candidate]; i++ {
		candidate = fmt.Sprintf("%s_%d", base, i)
	}
	d.envNames[candidate] = true
	return candidate
}

func stringMapNode(m map[string]string) *yaml.Node {
	node := mappingNode()
	for _, k := range slices.Sorted(maps.Keys(m)) {
		put(node, k, scalarNode(m[k]))
	}
	return node
}

// plaintext decrypts path when it is sops-encrypted (or matches a pattern)
// and reports whether it was. A path that is not a readable regular file is
// left alone: Compose itself decides whether a missing optional file is fatal.
func (d *decryptor) plaintext(path string) ([]byte, bool, error) {
	if data, ok := d.data[path]; ok {
		return data, true, nil
	}
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return nil, false, nil
	}
	encrypted := d.matches(filepath.Base(path))
	if !encrypted {
		if encrypted, err = sopsfile.IsEncrypted(path); err != nil {
			return nil, false, fmt.Errorf("inspect %s: %w", path, err)
		}
	}
	if !encrypted {
		return nil, false, nil
	}
	data, err := sopsfile.Decrypt(path)
	if err != nil {
		return nil, false, err
	}
	if d.data == nil {
		d.data = map[string][]byte{}
	}
	d.data[path] = data
	return data, true, nil
}

// file decrypts path into the store when it is sops-encrypted, and reports
// whether it was. Each source is written at most once.
func (d *decryptor) file(path string) (string, bool, error) {
	if plain, ok := d.plain[path]; ok {
		return plain, true, nil
	}
	data, ok, err := d.plaintext(path)
	if err != nil || !ok {
		return "", ok, err
	}
	plain, err := d.store.Put(path, data)
	if err != nil {
		return "", false, fmt.Errorf("store decrypted %s: %w", path, err)
	}
	d.plain[path] = plain
	return plain, true, nil
}

// matches reports whether a basename matches one of the additive patterns.
func (d *decryptor) matches(base string) bool {
	for _, pattern := range d.patterns {
		if ok, err := filepath.Match(pattern, base); err == nil && ok {
			return true
		}
	}
	return false
}

const overrideTag = "!override"

// envFileNode renders one env_file entry, keeping the short string form unless
// the entry carries attributes that only the long form can express.
func envFileNode(path string, entry types.EnvFile) *yaml.Node {
	if bool(entry.Required) && entry.Format == "" {
		return scalarNode(path)
	}
	node := mappingNode()
	put(node, "path", scalarNode(path))
	if !bool(entry.Required) {
		put(node, "required", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"})
	}
	if entry.Format != "" {
		put(node, "format", scalarNode(entry.Format))
	}
	return node
}

func mappingNode() *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func put(mapping *yaml.Node, key string, value *yaml.Node) {
	mapping.Content = append(mapping.Content, scalarNode(key), value)
}

func marshal(root *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, fmt.Errorf("encode override file: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode override file: %w", err)
	}
	return buf.Bytes(), nil
}
