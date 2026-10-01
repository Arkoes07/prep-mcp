package vault_test

import (
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Arkoes07/prep-mcp/internal/config"
	"github.com/Arkoes07/prep-mcp/internal/testvault"
	"github.com/Arkoes07/prep-mcp/internal/vault"
)

// openAt opens the vault at dir with the config found in it and the frozen test clock.
func openAt(t testing.TB, dir string) *vault.Vault {
	t.Helper()
	cfg, err := config.Load(filepath.Join(dir, "prep.config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	v, err := vault.Open(dir, cfg, testvault.Clock())
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	testvault.CloseOnCleanup(t, v)
	return v
}

// copyOpen makes a temp copy of the example vault and opens it.
func copyOpen(t testing.TB) (string, *vault.Vault) {
	t.Helper()
	root := testvault.Copy(t)
	return root, openAt(t, root)
}

func paths(docs []*vault.Doc) []string {
	out := make([]string, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.Path)
	}
	return out
}

// mapValue returns the scalar value of key in a mapping node, and whether it exists.
func mapValue(n *yaml.Node, key string) (string, bool) {
	val := vault.Field(n, key)
	if val == nil {
		return "", false
	}
	return val.Value, true
}
