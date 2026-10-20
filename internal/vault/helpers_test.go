package vault_test

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/arkoes07/prep-mcp/internal/testvault"
	"github.com/arkoes07/prep-mcp/internal/vault"
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

func node(t testing.TB, js string) *yaml.Node {
	t.Helper()
	n, err := vault.JSONToNode(json.RawMessage(js))
	if err != nil {
		t.Fatalf("JSONToNode(%s): %v", js, err)
	}
	return n
}

// mustContain fails unless err is non-nil and mentions every fragment.
func mustContain(t testing.TB, err error, fragments ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("got no error, want one containing %q", fragments)
	}
	for _, f := range fragments {
		if !strings.Contains(err.Error(), f) {
			t.Errorf("error %q does not contain %q", err, f)
		}
	}
}
