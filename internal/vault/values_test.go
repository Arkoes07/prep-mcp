package vault_test

import (
	"slices"
	"testing"

	"github.com/Arkoes07/prep-mcp/internal/vault"
)

// TestValues is the table of what a field's values are and whether it has a
// value. Text is the one value, or "" when there are none or several.
func TestValues(t *testing.T) {
	tests := []struct {
		name string
		fm   string // the frontmatter's YAML
		key  string
		want []string
		has  bool
	}{
		{name: "missing", fm: "y: 1", key: "x"},
		{name: "null", fm: "x: null", key: "x", want: []string{""}},
		{name: "blank", fm: "x:", key: "x", want: []string{""}},
		{name: "empty", fm: `x: ""`, key: "x", want: []string{""}},
		{name: "whitespace", fm: `x: "  "`, key: "x", want: []string{""}},
		{name: "scalar_trimmed", fm: `x: " a "`, key: "x", want: []string{"a"}, has: true},
		{name: "number_as_written", fm: "x: 1.0", key: "x", want: []string{"1.0"}, has: true},
		{
			name: "list_scalar_items",
			fm:   `x: [" a", b, null, [c], {d: e}]`,
			key:  "x",
			want: []string{"a", "b", ""},
			has:  true,
		},
		{name: "list_of_one", fm: "x: [a]", key: "x", want: []string{"a"}, has: true},
		{name: "list_of_blanks", fm: `x: ["", null]`, key: "x", want: []string{"", ""}},
		{name: "list_of_mappings", fm: "x: [{a: b}]", key: "x", has: true},
		{name: "list_of_blank_lists", fm: `x: [[""], []]`, key: "x"},
		{name: "empty_list", fm: "x: []", key: "x"},
		{name: "mapping", fm: "x: {a: b}", key: "x", has: true},
		{name: "empty_mapping", fm: "x: {}", key: "x"},
		{name: "alias_scalar", fm: "a: &v b\nx: *v", key: "x", want: []string{"b"}, has: true},
		{name: "alias_list", fm: "a: &v [b, c]\nx: *v", key: "x", want: []string{"b", "c"}, has: true},
		{name: "alias_item", fm: "a: &v b\nx: [*v, c]", key: "x", want: []string{"b", "c"}, has: true},
		{name: "alias_null", fm: "a: &v null\nx: *v", key: "x", want: []string{""}},
		{name: "alias_mapping", fm: "a: &v {b: c}\nx: *v", key: "x", has: true},

		{name: "tags_list", fm: `tags: [A, " B "]`, key: "tags", want: []string{"a", "b"}, has: true},
		{name: "tags_comma_string", fm: `tags: "A,  B ,c"`, key: "tags", want: []string{"a", "b", "c"}, has: true},
		{name: "tags_bracketed_string", fm: `tags: "[a, 'b']"`, key: "tags", want: []string{"a", "b"}, has: true},
		{name: "tags_single", fm: "tags: a", key: "tags", want: []string{"a"}, has: true},
		{name: "tags_empty_entries", fm: `tags: "a,,b,"`, key: "tags", want: []string{"a", "b"}, has: true},
		{
			name: "tags_dedupe_keeps_first",
			fm:   `tags: [b, a, A, "a "]`,
			key:  "tags",
			want: []string{"b", "a"},
			has:  true,
		},
		{
			name: "tags_scalars_stringified",
			fm:   "tags: [2026, true]",
			key:  "tags",
			want: []string{"2026", "true"},
			has:  true,
		},
		{name: "tags_nested_ignored", fm: "tags: [a, [b], {c: d}]", key: "tags", want: []string{"a"}, has: true},
		{
			name: "tags_path_like",
			fm:   "tags: Issue/Buries-Headline",
			key:  "tags",
			want: []string{"issue/buries-headline"},
			has:  true,
		},
		{name: "tags_control_whitespace", fm: `tags: ["\ta\n"]`, key: "tags", want: []string{"a"}, has: true},
		{name: "tags_alias", fm: "a: &t [A]\ntags: *t", key: "tags", want: []string{"a"}, has: true},
		{name: "tags_null", fm: "tags: null", key: "tags"},
		{name: "tags_empty_list", fm: "tags: []", key: "tags"},
		{name: "tags_only_separators", fm: `tags: " , "`, key: "tags"},
		{name: "tags_mapping", fm: "tags: {a: b}", key: "tags"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fm, err := vault.SplitFrontmatter([]byte("---\n" + tc.fm + "\n---\n"))
			if err != nil || fm == nil {
				t.Fatalf("bad test frontmatter %q: %v", tc.fm, err)
			}
			if got := vault.Values(fm.Map, tc.key); !slices.Equal(got, tc.want) {
				t.Errorf("Values = %q, want %q", got, tc.want)
			}
			if got := vault.HasValue(fm.Map, tc.key); got != tc.has {
				t.Errorf("HasValue = %v, want %v", got, tc.has)
			}
			wantText := ""
			if len(tc.want) == 1 {
				wantText = tc.want[0]
			}
			if got := vault.Text(fm.Map, tc.key); got != wantText {
				t.Errorf("Text = %q, want %q", got, wantText)
			}
		})
	}
}
