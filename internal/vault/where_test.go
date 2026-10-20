package vault_test

import (
	"maps"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/arkoes07/prep-mcp/internal/vault"
)

func TestParseWhere(t *testing.T) {
	tests := []struct {
		name    string
		in      string // YAML; JSON reads the same
		want    vault.Where
		wantErr string
	}{
		{name: "scalar", in: `{status: open}`, want: vault.Where{"status": {"open"}}},
		{name: "list", in: `{status: [a, b]}`, want: vault.Where{"status": {"a", "b"}}},
		{name: "json_syntax", in: `{"status": "open", "score": 1.0}`, want: vault.Where{"status": {"open"}, "score": {"1.0"}}},
		{name: "tags_normalised", in: `{tags: "Meta/Plan, x"}`, want: vault.Where{"tags": {"meta/plan", "x"}}},
		{
			name: "nested_value", in: `{status: {a: b}}`,
			wantErr: `where.status: use one value or a list of values, such as "open" or ["open", "closed"]`,
		},
		{
			name: "nested_list_item", in: `{status: [a, [b]]}`,
			wantErr: `where.status: use one value or a list of values, such as "open" or ["open", "closed"]`,
		},
		{name: "duplicate_key", in: `{a: 1, a: 2}`, wantErr: `where.a appears twice; use each field once`},
		{name: "not_a_mapping", in: `[a]`, wantErr: `where must be field: value pairs, such as {"status": "open"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var doc yaml.Node
			if err := yaml.Unmarshal([]byte(tc.in), &doc); err != nil {
				t.Fatal(err)
			}
			got, err := vault.ParseWhere(doc.Content[0])
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !maps.EqualFunc(got, tc.want, slices.Equal[[]string]) {
				t.Errorf("ParseWhere(%s) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
