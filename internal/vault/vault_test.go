package vault_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Arkoes07/prep-mcp/internal/testvault"
	"github.com/Arkoes07/prep-mcp/internal/vault"
)

// TestRootEnforced checks the second layer of confinement: even after
// Resolve approves a path, the kernel-level os.Root refuses to leave the vault.
func TestRootEnforced(t *testing.T) {
	root, v := copyOpen(t)
	if _, err := v.Resolve("weeks/new.md", vault.AccessWrite); err != nil {
		t.Fatalf("Resolve before the swap: %v", err)
	}

	outside := filepath.Join(filepath.Dir(root), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "weeks")); err != nil {
		t.Fatal(err)
	}
	testvault.Symlink(t, outside, filepath.Join(root, "weeks"))

	r := v.Root()
	if r == nil {
		t.Fatal("Root() returned nil")
	}
	if err := r.WriteFile("weeks/new.md", []byte("escaped\n"), 0o644); err == nil {
		t.Error("write through a symlink that leaves the vault succeeded, want an error")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("outside dir was modified: %v", entries)
	}
}

func TestOpenViaSymlinkedRoot(t *testing.T) {
	root, _ := copyOpen(t)
	link := filepath.Join(filepath.Dir(root), "link")
	testvault.Symlink(t, root, link)

	_, v := testvault.Open(t, link)
	if _, err := v.Resolve("weeks/2026-w03.md", vault.AccessWrite); err != nil {
		t.Errorf("Resolve through a symlinked root: %v", err)
	}
	if _, content, err := v.ReadFile("CLAUDE.md"); err != nil || len(content) == 0 {
		t.Errorf("ReadFile through a symlinked root: %v (%d bytes)", err, len(content))
	}
}
