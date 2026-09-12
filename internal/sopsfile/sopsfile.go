// Package sopsfile detects sops-encrypted files and decrypts them using the
// sops library, honouring every key source the sops binary supports.
package sopsfile

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getsops/sops/v3/decrypt"
	"gopkg.in/yaml.v3"
)

// ErrNotEncrypted is returned by Decrypt for files without sops metadata.
var ErrNotEncrypted = errors.New("not a sops-encrypted file")

// sniffLimit bounds how much of a file is inspected for sops metadata.
const sniffLimit = 1 << 20

// Detect reports whether data carries sops metadata: a top-level "sops"
// mapping with a "mac" entry (YAML, JSON and the JSON wrapper sops uses for
// binary files), a "sops_mac=" line (dotenv) or a [sops] section with a mac
// key (INI).
func Detect(data []byte) bool {
	if len(data) > sniffLimit {
		data = data[:sniffLimit]
	}
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) == 0 {
		return false
	}
	if trimmed[0] == '{' {
		return hasSopsMac(parseJSON(data))
	}
	if hasDotenvMac(data) || hasINIMac(data) {
		return true
	}
	return hasSopsMac(parseYAML(data))
}

func parseJSON(data []byte) map[string]any {
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	return m
}

func parseYAML(data []byte) map[string]any {
	var m map[string]any
	if yaml.Unmarshal(data, &m) != nil {
		return nil
	}
	return m
}

func hasSopsMac(m map[string]any) bool {
	meta, ok := m["sops"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = meta["mac"]
	return ok
}

func hasDotenvMac(data []byte) bool {
	return scanLines(data, func(line string) bool { return strings.HasPrefix(line, "sops_mac=") })
}

func hasINIMac(data []byte) bool {
	inSops := false
	return scanLines(data, func(line string) bool {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "["):
			inSops = line == "[sops]"
		case inSops && strings.HasPrefix(line, "mac"):
			rest := strings.TrimSpace(strings.TrimPrefix(line, "mac"))
			return strings.HasPrefix(rest, "=")
		}
		return false
	})
}

func scanLines(data []byte, pred func(string) bool) bool {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), sniffLimit)
	for sc.Scan() {
		if pred(sc.Text()) {
			return true
		}
	}
	return false
}

// IsEncrypted reports whether the regular file at path is sops-encrypted.
// Directories and other non-regular files are never encrypted; a missing
// or unreadable file is an error.
func IsEncrypted(path string) (bool, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if !fi.Mode().IsRegular() {
		return false, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, sniffLimit)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, os.ErrClosed) && n == 0 && fi.Size() != 0 {
		return false, err
	}
	return Detect(buf[:n]), nil
}

// InferFormat returns the sops store format ("yaml", "json", "dotenv", "ini"
// or "binary") for a file, from its extension first and its content second.
// A trailing ".enc" or ".sops" segment is ignored so that "secrets.yaml.enc"
// is treated as YAML.
func InferFormat(path string, data []byte) string {
	name := filepath.Base(path)
	for _, suffix := range []string{".enc", ".sops"} {
		if strings.HasSuffix(name, suffix) && strings.Count(name, ".") > 1 {
			name = strings.TrimSuffix(name, suffix)
		}
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".env":
		return "dotenv"
	case ".ini":
		return "ini"
	case "":
		if name == ".env" || strings.HasPrefix(name, ".env.") {
			return "dotenv"
		}
	}
	return sniffFormat(data)
}

func sniffFormat(data []byte) string {
	if len(data) == 0 {
		return "binary"
	}
	if m := parseJSON(data); m != nil {
		if _, isBinary := m["data"].(string); isBinary && len(m) == 2 && hasSopsMac(m) {
			return "binary"
		}
		return "json"
	}
	if hasDotenvMac(data) {
		return "dotenv"
	}
	if hasINIMac(data) {
		return "ini"
	}
	if hasSopsMac(parseYAML(data)) {
		return "yaml"
	}
	return "binary"
}

// Decrypt reads the sops-encrypted file at path and returns its plaintext in
// the same format. It fails if the file is not encrypted.
func Decrypt(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !Detect(data) {
		return nil, fmt.Errorf("%s: %w", path, ErrNotEncrypted)
	}
	format := InferFormat(path, data)
	out, err := decrypt.Data(data, format)
	if err != nil {
		return nil, fmt.Errorf("decrypt %s (%s): %w", path, format, err)
	}
	return out, nil
}
