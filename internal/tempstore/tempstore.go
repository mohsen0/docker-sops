// Package tempstore holds decrypted files in a private directory for the
// lifetime of one command and removes them on Close.
package tempstore

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Store is a private temporary directory (mode 0700) of decrypted files.
type Store struct {
	dir    string
	mu     sync.Mutex
	n      int
	closed bool
}

// New creates a store under parent, or under the OS temp directory when
// parent is empty.
func New(parent string) (*Store, error) {
	if parent == "" {
		parent = os.TempDir()
	}
	dir, err := os.MkdirTemp(parent, "docker-sops-")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Dir returns the store directory.
func (s *Store) Dir() string { return s.dir }

// Put writes data to a new 0600 file whose basename matches source, so tools
// that infer format from the file name keep working. Each Put lands in its
// own subdirectory, so identical basenames never collide.
func (s *Store) Put(source string, data []byte) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", fmt.Errorf("tempstore closed")
	}
	sub := filepath.Join(s.dir, fmt.Sprintf("%d", s.n))
	s.n++
	if err := os.Mkdir(sub, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(sub, filepath.Base(source))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// Close removes the directory and everything in it. It is safe to call twice.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return os.RemoveAll(s.dir)
}
