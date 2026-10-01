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
