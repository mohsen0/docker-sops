// Package argscan walks docker CLI argv looking for tokens that reference
// sops-encrypted files, decrypts each one into a tempstore.Store, and
// returns the argv rewritten to point at the decrypted copies.
//
// The scanner is deliberately dumb about Docker's flag grammar: it
// classifies tokens by shape (bare path, --flag=value, key=value lists)
// rather than by which flag they belong to, so it never has to be kept in
// sync with Docker's full flag set. The one place it does need flag
// knowledge is the run/create/exec "container command" cutoff (see
// containerCutoff), where a conservative, documented rule is used.
package argscan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mohsen0/docker-sops/internal/sopsfile"
	"github.com/mohsen0/docker-sops/internal/tempstore"
)

// Options controls how Plan decides which files to rewrite.
type Options struct {
	// Detect controls content detection via sopsfile.IsEncrypted. When
	// false only Patterns are used.
	Detect bool
	// Patterns are extra glob patterns (matched with filepath.Match
	// against the basename) that force a file to be treated as encrypted.
	Patterns []string
	// Cwd is the directory relative paths resolve against. Empty means
	// os.Getwd().
	Cwd string
}

// Decrypted records one file that Plan decrypted.
type Decrypted struct {
	Source string // path as it appeared in argv
	Path   string // path of the decrypted copy
}

// Rewrite is the result of Plan: the rewritten argv and the files that were
// decrypted to produce it.
type Rewrite struct {
	Args      []string
	Decrypted []Decrypted
}

// Plan scans argv for tokens that reference sops-encrypted files, decrypts
// each one into store, and returns the rewritten argv. argv[0] is the
// docker subcommand (e.g. "run", "build", "secret"); it is never
// inspected as a path and never rewritten.
//
// store must not be nil: Plan decrypts into it as it discovers matches, so
// there is no dry-run mode that skips writing files. Callers that only want
// to know whether argv contains encrypted references should still pass a
// real store and inspect the returned Rewrite.Decrypted.
//
// The same source file referenced more than once in argv is decrypted only
// once; every occurrence is rewritten to the same decrypted path, and only
// the first occurrence is recorded in Rewrite.Decrypted.
func Plan(argv []string, store *tempstore.Store, opts Options) (*Rewrite, error) {
	if store == nil {
		return nil, fmt.Errorf("argscan: store must not be nil")
	}
	if len(argv) == 0 {
		return &Rewrite{}, nil
	}

	cwd := opts.Cwd
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("argscan: %w", err)
		}
		cwd = wd
	}

	scanEnd := len(argv)
	switch argv[0] {
	case "run", "create", "exec":
		scanEnd = containerCutoff(argv)
	}

	args := append([]string(nil), argv...)
	var decrypted []Decrypted
	cache := map[string]string{}

	for i := 1; i < len(argv) && i < scanEnd; i++ {
		newTok, err := rewriteToken(args[i], cwd, opts, store, cache, &decrypted)
		if err != nil {
			return nil, err
		}
		args[i] = newTok
	}

	return &Rewrite{Args: args, Decrypted: decrypted}, nil
}

// rewriteToken rewrites a single argv token, preserving its shape.
func rewriteToken(tok string, cwd string, opts Options, store *tempstore.Store, cache map[string]string, decrypted *[]Decrypted) (string, error) {
	if strings.HasPrefix(tok, "-") {
		idx := strings.Index(tok, "=")
		if idx < 0 {
			// A bare flag with no attached value: nothing in this token
			// itself can be a path fragment.
			return tok, nil
		}
		prefix, value := tok[:idx], tok[idx+1:]
		newValue, err := rewriteValue(value, cwd, opts, store, cache, decrypted)
		if err != nil {
			return "", err
		}
		if newValue == value {
			return tok, nil
		}
		return prefix + "=" + newValue, nil
	}
	return rewriteValue(tok, cwd, opts, store, cache, decrypted)
}

// rewriteValue rewrites a value string, which is either a bare argv token
// or the part of a --flag=value token after the "=". It first tries the
// whole value as a path candidate, then falls back to treating it as a
// comma-separated key=value list and rewriting any src=/source= entry.
func rewriteValue(value string, cwd string, opts Options, store *tempstore.Store, cache map[string]string, decrypted *[]Decrypted) (string, error) {
	if resolved, ok := fileCandidate(cwd, value); ok {
		rewrite, err := shouldRewrite(resolved, value, opts)
		if err != nil {
			return "", err
		}
		if !rewrite {
			return value, nil
		}
		return decryptInto(resolved, value, store, cache, decrypted)
	}

	parts := strings.Split(value, ",")
	changed := false
	for i, part := range parts {
		eq := strings.Index(part, "=")
		if eq < 0 {
			continue
		}
		key, val := part[:eq], part[eq+1:]
		if key != "src" && key != "source" {
			continue
		}
		resolved, ok := fileCandidate(cwd, val)
		if !ok {
			continue
		}
		rewrite, err := shouldRewrite(resolved, val, opts)
		if err != nil {
			return "", err
		}
		if !rewrite {
			continue
		}
		newPath, err := decryptInto(resolved, val, store, cache, decrypted)
		if err != nil {
			return "", err
		}
		parts[i] = key + "=" + newPath
		changed = true
	}
	if !changed {
		return value, nil
	}
	return strings.Join(parts, ","), nil
}

// fileCandidate reports whether value is a path fragment (does not start
// with "-") that resolves, relative to cwd, to an existing regular file.
func fileCandidate(cwd, value string) (resolved string, ok bool) {
	if value == "" || strings.HasPrefix(value, "-") {
		return "", false
	}
	resolved = value
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(cwd, resolved)
	}
	fi, err := os.Stat(resolved)
	if err != nil || !fi.Mode().IsRegular() {
		return "", false
	}
	return resolved, true
}

// shouldRewrite decides whether the file at resolved should be decrypted,
// per Options: content detection (if enabled) or a basename pattern match.
func shouldRewrite(resolved, fragment string, opts Options) (bool, error) {
	if opts.Detect {
		enc, err := sopsfile.IsEncrypted(resolved)
		if err != nil {
			return false, fmt.Errorf("argscan: %w", err)
		}
		if enc {
			return true, nil
		}
	}
	base := filepath.Base(fragment)
	for _, pat := range opts.Patterns {
		matched, err := filepath.Match(pat, base)
		if err != nil {
			return false, fmt.Errorf("argscan: pattern %q: %w", pat, err)
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

// decryptInto decrypts the file at resolved (once per resolved path,
// cached) into store and records the first occurrence in decrypted.
func decryptInto(resolved, fragment string, store *tempstore.Store, cache map[string]string, decrypted *[]Decrypted) (string, error) {
	if p, ok := cache[resolved]; ok {
		return p, nil
	}
	plaintext, err := sopsfile.Decrypt(resolved)
	if err != nil {
		return "", fmt.Errorf("argscan: decrypt %s: %w", fragment, err)
	}
	newPath, err := store.Put(resolved, plaintext)
	if err != nil {
		return "", fmt.Errorf("argscan: store %s: %w", fragment, err)
	}
	cache[resolved] = newPath
	*decrypted = append(*decrypted, Decrypted{Source: fragment, Path: newPath})
	return newPath, nil
}

// booleanFlags are run/create/exec flags that never take a following value.
var booleanFlags = map[string]bool{
	"-d": true, "--detach": true,
	"-i": true, "--interactive": true,
	"-t": true, "--tty": true,
	"--rm":               true,
	"--privileged":       true,
	"--init":             true,
	"--no-healthcheck":   true,
	"--read-only":        true,
	"--sig-proxy":        true,
	"--oom-kill-disable": true,
	"-P":                 true, "--publish-all": true,
	"-q": true, "--quiet": true,
	"--help": true,
}

// shortBooleanLetters are the short boolean flags that may be combined
// into one token, e.g. "-it", "-dit", "-ti".
var shortBooleanLetters = map[byte]bool{'d': true, 'i': true, 't': true, 'P': true, 'q': true}

// isCombinedShortBoolean reports whether tok is a run of combined short
// boolean flags, e.g. "-it", "-dit".
func isCombinedShortBoolean(tok string) bool {
	if len(tok) < 2 || tok[0] != '-' || tok[1] == '-' {
		return false
	}
	for i := 1; i < len(tok); i++ {
		if !shortBooleanLetters[tok[i]] {
			return false
		}
	}
	return true
}

// flagTakesValue reports whether tok, a "-"-prefixed token in a
// run/create/exec argv, consumes the next argv token as its value. This is
// the conservative rule from the design spec: a token containing "="
// carries its own value and takes no further token; otherwise it takes a
// value unless it is a known boolean flag or a combination of short
// boolean flags.
func flagTakesValue(tok string) bool {
	if strings.Contains(tok, "=") {
		return false
	}
	if booleanFlags[tok] {
		return false
	}
	if isCombinedShortBoolean(tok) {
		return false
	}
	return true
}

// containerCutoff returns the index, exclusive, up to which argv should be
// scanned for run/create/exec: the index of the first positional argument
// (the image or container name), or of a bare "--", or len(argv) if
// neither is found.
func containerCutoff(argv []string) int {
	i := 1
	for i < len(argv) {
		tok := argv[i]
		if tok == "--" {
			return i
		}
		if !strings.HasPrefix(tok, "-") {
			return i
		}
		if flagTakesValue(tok) {
			i += 2
			continue
		}
		i++
	}
	return i
}
