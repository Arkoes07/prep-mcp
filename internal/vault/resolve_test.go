package vault_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arkoes07/prep-mcp/internal/testvault"
	"github.com/Arkoes07/prep-mcp/internal/vault"
)

// outcome is what Resolve should return for one access mode.
type outcome struct {
	path string // expected cleaned path when the call succeeds
	fail bool   // the call must return an error
	err  string // substring the error must contain; empty means any error
}

func good(p string) outcome  { return outcome{path: p} }
func bad(sub string) outcome { return outcome{fail: true, err: sub} }

func TestResolve(t *testing.T) {
	root, v := copyOpen(t)
	outside := filepath.Dir(root)
	testvault.Write(t, outside, "out.md", "outside\n")
	mustSymlink(t, filepath.Join(outside, "out.md"), filepath.Join(root, "link-out.md"))
	mustSymlink(t, outside, filepath.Join(root, "linkdir"))
	mustSymlink(t, "weeks/2026-w03.md", filepath.Join(root, "link-in.md"))

	tests := []struct {
		in          string
		read, write outcome
	}{
		{"weeks/2026-w03.md", good("weeks/2026-w03.md"), good("weeks/2026-w03.md")},
		{"./weeks/../weeks/2026-w03.md", good("weeks/2026-w03.md"), good("weeks/2026-w03.md")},

		{"../out.md", bad("outside the vault"), bad("outside the vault")},
		{"weeks/../../x.md", bad("outside the vault"), bad("outside the vault")},

		{"/etc/passwd", bad("absolute paths not allowed; use a vault-relative path"), bad("absolute paths not allowed; use a vault-relative path")},
		{root + "/weeks/2026-w03.md", bad("absolute paths not allowed; use a vault-relative path"), bad("absolute paths not allowed; use a vault-relative path")},

		{"", bad(""), bad("")},
		{"weeks/2026-w03.md/", bad(""), bad("")},
		{`a\b.md`, bad(""), bad("")},
		{"a\x00.md", bad(""), bad("")},

		{"link-out.md", bad("outside"), bad("outside")},
		{"linkdir/x.md", bad("outside"), bad("outside")},
		{"link-in.md", good("link-in.md"), bad("is a symlink; write to weeks/2026-w03.md")},

		{".git/config", bad(""), bad("")},
		{".git/x.md", bad(""), bad("")},
		{".obsidian/w.md", bad(""), bad("")},
		{"weeks/.hidden.md", bad(""), bad("")},
		{".GIT/x.md", bad(""), bad("")},

		{"templates/week.md", good("templates/week.md"), bad("templates/ is read-only")},
		// Templates/x.md does not exist, so a read reports "not found" (PLAN.md lists it as ok).
		{"Templates/x.md", bad("not found"), bad("templates/ is read-only")},

		{"weeks/2026-w03.txt", bad(".md only"), bad(".md only")},
		{"prep.config.yaml", bad(".md only"), bad(".md only")},

		{"newdir/sub/x.md", bad("not found"), good("newdir/sub/x.md")},
	}
	for _, tc := range tests {
		for _, mode := range []struct {
			name string
			a    vault.Access
			want outcome
		}{{"read", vault.Read, tc.read}, {"write", vault.Write, tc.write}} {
			t.Run(mode.name+"/"+strings.ReplaceAll(tc.in, "/", "|"), func(t *testing.T) {
				got, err := v.Resolve(tc.in, mode.a)
				if mode.want.fail {
					if err == nil {
						t.Fatalf("Resolve(%q) = %q, want an error containing %q", tc.in, got, mode.want.err)
					}
					if !strings.Contains(err.Error(), mode.want.err) {
						t.Errorf("Resolve(%q) error = %q, want it to contain %q", tc.in, err, mode.want.err)
					}
					return
				}
				if err != nil {
					t.Fatalf("Resolve(%q): %v", tc.in, err)
				}
				if got != mode.want.path {
					t.Errorf("Resolve(%q) = %q, want %q", tc.in, got, mode.want.path)
				}
			})
		}
	}
}

// TestRootEnforced checks the second layer of confinement: even after
// Resolve approves a path, the kernel-level os.Root refuses to leave the vault.
func TestRootEnforced(t *testing.T) {
	root, v := copyOpen(t)
	if _, err := v.Resolve("weeks/new.md", vault.Write); err != nil {
		t.Fatalf("Resolve before the swap: %v", err)
	}

	outside := filepath.Join(filepath.Dir(root), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "weeks")); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, outside, filepath.Join(root, "weeks"))

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
	mustSymlink(t, root, link)

	v := openAt(t, link)
	if _, err := v.Resolve("weeks/2026-w03.md", vault.Write); err != nil {
		t.Errorf("Resolve through a symlinked root: %v", err)
	}
	if _, content, _, err := v.Read("CLAUDE.md"); err != nil || len(content) == 0 {
		t.Errorf("Read through a symlinked root: %v (%d bytes)", err, len(content))
	}
}

func mustSymlink(t testing.TB, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink %s -> %s: %v", link, target, err)
	}
}
