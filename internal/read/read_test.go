package read_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadDoc checks what ReadDoc adds to vault.ReadFile: the hash. Path
// checks and not-found errors are covered by TestReadFile in package vault.
func TestReadDoc(t *testing.T) {
	root, r := copyOpen(t)

	t.Run("bytes_and_hash", func(t *testing.T) {
		disk, err := os.ReadFile(filepath.Join(root, "weeks", "2026-w03.md"))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(disk)
		p, content, hash, err := r.ReadDoc("weeks/2026-w03.md")
		if err != nil {
			t.Fatal(err)
		}
		if p != "weeks/2026-w03.md" {
			t.Errorf("path = %q", p)
		}
		if !bytes.Equal(content, disk) {
			t.Errorf("content differs from the file on disk")
		}
		if want := hex.EncodeToString(sum[:]); hash != want {
			t.Errorf("hash = %q, want lowercase hex sha256 %q", hash, want)
		}
	})

	t.Run("error_passes_through", func(t *testing.T) {
		_, _, hash, err := r.ReadDoc("x.md")
		if err == nil || !strings.Contains(err.Error(), "not found") || hash != "" {
			t.Errorf("ReadDoc(x.md) = hash %q, error %v; want no hash and a not-found error", hash, err)
		}
	})
}

func TestReadDocByName(t *testing.T) {
	root, r := copyOpen(t)
	if err := os.WriteFile(filepath.Join(root, "dup.md"), []byte("# root copy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "weeks", "dup.md"), []byte("# week copy\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resolves := []struct{ in, want string }{
		{"buries-headline", "improvements/buries-headline.md"},
		{"[[Buries-Headline]]", "improvements/buries-headline.md"},
		{"[[2026-w03|this week]]", "weeks/2026-w03.md"},
		{"[[2026-w03#Log]]", "weeks/2026-w03.md"},
		{"[[weeks/2026-w03]]", "weeks/2026-w03.md"},
		{"[[weeks/dup]]", "weeks/dup.md"},
		{"[[weeks/dup.md]]", "weeks/dup.md"},
		{"tags.md", "tags.md"},
		{"templates/week.md", "templates/week.md"},
	}
	for _, tc := range resolves {
		t.Run(tc.in, func(t *testing.T) {
			p, _, hash, err := r.ReadDoc(tc.in)
			if err != nil || p != tc.want || hash == "" {
				t.Errorf("ReadDoc(%q) = %q, hash %q, error %v; want %q", tc.in, p, hash, err, tc.want)
			}
		})
	}

	fails := []struct{ in, want string }{
		{"[[dup]]", `"dup" matches several files: dup.md, weeks/dup.md; pass one of these paths`},
		{"[[Nope|alias]]", `no file named "Nope"; use list_docs or search_text to find it`},
		{"week", `no file named "week"`}, // templates/ is left out, as in Scan
		{"[[]]", "empty name"},
	}
	for _, tc := range fails {
		t.Run(tc.in, func(t *testing.T) {
			_, _, _, err := r.ReadDoc(tc.in)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("ReadDoc(%q) error = %v; want it to contain %q", tc.in, err, tc.want)
			}
		})
	}
}
