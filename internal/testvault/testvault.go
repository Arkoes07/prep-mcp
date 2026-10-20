// Package testvault is a test-only helper. It gives each test a private,
// deterministic copy of examples/vault so tests never touch real data, and
// holds the forbidden word list that keeps Go code domain-neutral.
package testvault

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/arkoes07/prep-mcp/internal/config"
	"github.com/arkoes07/prep-mcp/internal/vault"
)

// Day is the fixed "today" for tests: a Wednesday in ISO week 2026-W03.
var Day = time.Date(2026, 1, 14, 9, 0, 0, 0, time.Local)

// Mtime is the modification time given to every copied file. It is before
// Day, so documents dated by mtime sort below documents with a date field.
var Mtime = time.Date(2026, 1, 10, 12, 0, 0, 0, time.Local)

func NewClock() func() time.Time {
	return func() time.Time { return Day }
}

// ExampleDir is the absolute path of the tracked examples/vault. Tests must
// only read from it (through Copy), never write to it.
func ExampleDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "examples", "vault")
}

// Copy copies examples/vault into a fresh directory under t.TempDir() and
// returns its path. The copy is named "vault", so its parent directory is
// a private place outside the vault where tests can put files. Every file
// gets Mtime, because git does not preserve modification times.
func Copy(t testing.TB) string {
	t.Helper()
	src := ExampleDir()
	dst := filepath.Join(t.TempDir(), "vault")
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, b, 0o644); err != nil {
			return err
		}
		return os.Chtimes(target, Mtime, Mtime)
	})
	if err != nil {
		t.Fatalf("testvault.Copy: %v", err)
	}
	return dst
}

// Write creates or replaces root/rel (slash-separated) with content, creating
// parent directories and setting Mtime.
func Write(t testing.TB, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("testvault.Write: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("testvault.Write: %v", err)
	}
	if err := os.Chtimes(p, Mtime, Mtime); err != nil {
		t.Fatalf("testvault.Write: %v", err)
	}
}

// Open loads dir's prep.config.yaml and opens the vault at dir. The vault is
// closed when the test ends.
func Open(t testing.TB, dir string) (*config.Config, *vault.Vault) {
	t.Helper()
	cfg, err := config.Load(filepath.Join(dir, "prep.config.yaml"))
	if err != nil {
		t.Fatalf("testvault.Open: %v", err)
	}
	v, err := vault.Open(dir)
	if err != nil {
		t.Fatalf("testvault.Open: %v", err)
	}
	CloseOnCleanup(t, v)
	return cfg, v
}

func Symlink(t testing.TB, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("testvault.Symlink %s -> %s: %v", link, target, err)
	}
}

// CloseOnCleanup closes c when the test ends and fails the test if that fails.
func CloseOnCleanup(t testing.TB, c io.Closer) {
	t.Helper()
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
}
