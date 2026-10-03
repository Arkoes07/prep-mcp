package vault_test

import (
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Arkoes07/prep-mcp/internal/testvault"
	"github.com/Arkoes07/prep-mcp/internal/vault"
)

func copyOpen(t testing.TB) (string, *vault.Vault) {
	t.Helper()
	root := testvault.Copy(t)
	_, v := testvault.Open(t, root)
	return root, v
}

// mapValue returns the scalar value of key in a mapping node, and whether it exists.
func mapValue(n *yaml.Node, key string) (string, bool) {
	val := vault.Field(n, key)
	if val == nil {
		return "", false
	}
	return val.Value, true
}
