package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mohsen0/docker-sops/internal/argscan"
	"github.com/mohsen0/docker-sops/internal/composefix"
	"github.com/mohsen0/docker-sops/internal/tempstore"
)

const overrideName = "docker-sops.override.yaml"

// composeNoProject lists compose subcommands that never load a project.
var composeNoProject = map[string]bool{"version": true, "help": true, "ls": true, "completion": true}

// composeRewrite is the outcome of rewriteCompose.
type composeRewrite struct {
	argv         []string
	decrypted    []argscan.Decrypted
	overridePath string   // empty when no override was needed
	env          []string // NAME=value entries to add to the child environment
	warnings     []string
}

// rewriteCompose handles encrypted files referenced from inside the compose
// project (service env_file entries, secrets and configs with file:). It
// decrypts env files into store, turns secrets and configs into
// environment-sourced entries, writes an override compose file and inserts
// "-f <override>" before the compose subcommand. argv[0] is "compose".
func rewriteCompose(ctx context.Context, argv []string, decrypted []argscan.Decrypted, store *tempstore.Store, opts wrapOptions) (*composeRewrite, error) {
	unchanged := &composeRewrite{argv: argv, decrypted: decrypted}
	flags, idx := composefix.ParseFlags(argv[1:])
	if idx < 0 || composeNoProject[argv[1+idx]] {
		return unchanged, nil
	}

	// A compose file that was itself decrypted lives in the temp store; keep
	// the project directory where the user's files are so relative paths in
	// it still resolve.
	extra := []string{}
	if flags.ProjectDirectory == "" && anyUnder(flags.Files, store.Dir()) {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		flags.ProjectDirectory = cwd
		extra = append(extra, "--project-directory", cwd)
	}

	res, err := composefix.Build(ctx, flags, store, opts.patterns)
	if err != nil {
		return nil, fmt.Errorf("compose: %w\nhint: run `docker compose config` to check the project loads", err)
	}
	if res.YAML == nil {
		return unchanged, nil
	}
	overridePath, err := store.Put(overrideName, res.YAML)
	if err != nil {
		return nil, err
	}
	// Any -f disables default discovery, so files Compose found on its own
	// must now be named explicitly, ahead of the override.
	if len(flags.Files) == 0 && os.Getenv("COMPOSE_FILE") == "" {
		for _, f := range res.ComposeFiles {
			extra = append(extra, "-f", f)
		}
	}

	seen := map[string]bool{}
	for _, d := range decrypted {
		seen[d.Path] = true
	}
	for _, r := range res.Refs {
		if r.Path != "" && seen[r.Path] {
			continue
		}
		seen[r.Path] = true
		decrypted = append(decrypted, argscan.Decrypted{Source: r.Source, Path: r.Path})
	}

	sub := 1 + idx
	newArgv := append([]string{}, argv[:sub]...)
	newArgv = append(newArgv, extra...)
	newArgv = append(newArgv, "-f", overridePath)
	newArgv = append(newArgv, argv[sub:]...)
	return &composeRewrite{argv: newArgv, decrypted: decrypted, overridePath: overridePath, env: res.Env, warnings: res.Warnings}, nil
}

func anyUnder(paths []string, dir string) bool {
	for _, p := range paths {
		if rel, err := filepath.Rel(dir, p); err == nil && !strings.HasPrefix(rel, "..") {
			return true
		}
	}
	return false
}
