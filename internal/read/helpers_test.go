package read_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Arkoes07/prep-mcp/internal/config"
	"github.com/Arkoes07/prep-mcp/internal/read"
	"github.com/Arkoes07/prep-mcp/internal/testvault"
	"github.com/Arkoes07/prep-mcp/internal/vault"
)

// openAt opens the vault at dir with the config found in it and returns a
// Reader on the frozen test clock.
func openAt(t testing.TB, dir string) *read.Reader {
	t.Helper()
	cfg, err := config.Load(filepath.Join(dir, "prep.config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	v, err := vault.Open(dir, cfg)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	testvault.CloseOnCleanup(t, v)
	return read.New(v, cfg, testvault.Clock())
}

// copyOpen makes a temp copy of the example vault and opens it.
func copyOpen(t testing.TB) (string, *read.Reader) {
	t.Helper()
	root := testvault.Copy(t)
	return root, openAt(t, root)
}

func paths(docs []*read.Doc) []string {
	out := make([]string, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.Path)
	}
	return out
}

func mustSymlink(t testing.TB, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink %s -> %s: %v", link, target, err)
	}
}
