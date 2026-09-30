package vault_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRead(t *testing.T) {
	root, v := copyOpen(t)

	t.Run("bytes_and_hash", func(t *testing.T) {
		disk, err := os.ReadFile(filepath.Join(root, "weeks", "2026-w03.md"))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(disk)
		p, content, hash, err := v.Read("weeks/2026-w03.md")
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

	for _, p := range []string{"CLAUDE.md", "templates/week.md"} {
		t.Run("readable/"+p, func(t *testing.T) {
			_, content, hash, err := v.Read(p)
			if err != nil {
				t.Fatalf("Read(%s): %v", p, err)
			}
			if len(content) == 0 || hash == "" {
				t.Errorf("Read(%s) returned %d bytes, hash %q", p, len(content), hash)
			}
		})
	}

	t.Run("missing", func(t *testing.T) {
		_, _, _, err := v.Read("x.md")
		want := "not found: x.md (use list_docs or search_text)"
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to contain %q", err, want)
		}
	})

	t.Run("outside_the_vault", func(t *testing.T) {
		_, _, _, err := v.Read("../../PRD.md")
		if err == nil || !strings.Contains(err.Error(), "outside the vault") {
			t.Errorf("error = %v, want one saying the path is outside the vault", err)
		}
	})
}
