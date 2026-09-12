package tempstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPutWritesPrivateFileKeepingBasename(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	p, err := s.Put("/some/where/app.env", []byte("A=1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "app.env" {
		t.Errorf("basename not kept: %s", p)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode %o, want 600", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Dir(p))
	if di.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %o, want 700", di.Mode().Perm())
	}
	b, _ := os.ReadFile(p)
	if string(b) != "A=1\n" {
		t.Errorf("content %q", b)
	}
}

func TestPutSameBasenameTwiceGivesDistinctPaths(t *testing.T) {
	s, _ := New(t.TempDir())
	defer func() { _ = s.Close() }()
	p1, _ := s.Put("a/secrets.yaml", []byte("1"))
	p2, _ := s.Put("b/secrets.yaml", []byte("2"))
	if p1 == p2 {
		t.Fatalf("collision: %s", p1)
	}
	if filepath.Base(p2) != "secrets.yaml" {
		t.Errorf("basename not kept: %s", p2)
	}
}

func TestCloseRemovesEverything(t *testing.T) {
	s, _ := New(t.TempDir())
	p, _ := s.Put("x.env", []byte("x"))
	dir := s.Dir()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("file still exists")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("dir still exists")
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close should be a no-op, got %v", err)
	}
}

func TestNewDefaultsToOSTempDir(t *testing.T) {
	s, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if filepath.Dir(s.Dir()) != filepath.Clean(os.TempDir()) {
		t.Errorf("dir %s not under %s", s.Dir(), os.TempDir())
	}
}
