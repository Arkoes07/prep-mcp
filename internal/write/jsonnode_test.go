package write_test

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Arkoes07/prep-mcp/internal/vault"
	"github.com/Arkoes07/prep-mcp/internal/write"
)

func TestJSONToNode(t *testing.T) {
	keys := func(n *yaml.Node) []string {
		var out []string
		for k := range vault.Pairs(n) {
			out = append(out, k.Value)
		}
		return out
	}

	t.Run("key_order", func(t *testing.T) {
		n := node(t, `{"b":1,"a":2}`)
		if n.Kind != yaml.MappingNode {
			t.Fatalf("kind = %v, want a mapping", n.Kind)
		}
		if got := strings.Join(keys(n), ","); got != "b,a" {
			t.Errorf("keys = %s, want b,a", got)
		}
		if n.Content[1].Value != "1" || n.Content[3].Value != "2" {
			t.Errorf("values = %q, %q, want 1, 2", n.Content[1].Value, n.Content[3].Value)
		}
	})

	t.Run("nested", func(t *testing.T) {
		n := node(t, `{"m":{"p":1,"q":"x"},"l":[1,"y",{"k":true}]}`)
		if got := strings.Join(keys(n), ","); got != "m,l" {
			t.Fatalf("keys = %s, want m,l", got)
		}
		m, l := n.Content[1], n.Content[3]
		if m.Kind != yaml.MappingNode || strings.Join(keys(m), ",") != "p,q" {
			t.Errorf("m = kind %v keys %v, want a mapping with p,q", m.Kind, keys(m))
		}
		if l.Kind != yaml.SequenceNode || len(l.Content) != 3 || l.Content[2].Kind != yaml.MappingNode {
			t.Errorf("l = kind %v with %d items, want a sequence of 3 ending in a mapping", l.Kind, len(l.Content))
		}
	})

	t.Run("null_value", func(t *testing.T) {
		n := node(t, `{"a":null}`)
		if v := n.Content[1]; v.Tag != "!!null" {
			t.Errorf("tag = %q, want !!null", v.Tag)
		}
	})

	for _, tc := range []struct{ name, in string }{
		{"array", `[1]`}, {"string", `"x"`}, {"number", `1`},
	} {
		t.Run("not_an_object_"+tc.name, func(t *testing.T) {
			_, err := write.JSONToNode(json.RawMessage(tc.in))
			mustContain(t, err, "object")
		})
	}
}
