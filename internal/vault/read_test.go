package vault_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arkoes07/prep-mcp/internal/vault"
)

func TestReadFile(t *testing.T) {
	root, v := copyOpen(t)

	t.Run("bytes", func(t *testing.T) {
		disk, err := os.ReadFile(filepath.Join(root, "weeks", "2026-w03.md"))
		if err != nil {
			t.Fatal(err)
		}
		p, content, err := v.ReadFile("weeks/2026-w03.md")
		if err != nil {
			t.Fatal(err)
		}
		if p != "weeks/2026-w03.md" {
			t.Errorf("path = %q", p)
		}
		if !bytes.Equal(content, disk) {
			t.Errorf("content differs from the file on disk")
		}
	})

	for _, p := range []string{"CLAUDE.md", "templates/week.md"} {
		t.Run("readable/"+p, func(t *testing.T) {
			_, content, err := v.ReadFile(p)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", p, err)
			}
			if len(content) == 0 {
				t.Errorf("ReadFile(%s) returned no bytes", p)
			}
		})
	}

	t.Run("missing", func(t *testing.T) {
		_, _, err := v.ReadFile("x.md")
		want := "not found: x.md (use list_docs or search_text)"
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to contain %q", err, want)
		}
	})

	t.Run("outside_the_vault", func(t *testing.T) {
		_, _, err := v.ReadFile("../../README.md")
		if err == nil || !strings.Contains(err.Error(), "outside the vault") {
			t.Errorf("error = %v, want one saying the path is outside the vault", err)
		}
	})
}

func TestHash(t *testing.T) {
	b := []byte("x\n")
	sum := sha256.Sum256(b)
	if got, want := vault.Hash(b), hex.EncodeToString(sum[:]); got != want {
		t.Errorf("Hash = %q, want lowercase hex sha256 %q", got, want)
	}
}
