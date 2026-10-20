package vault_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/arkoes07/prep-mcp/internal/vault"
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

// TestDay is the table of the day a date field holds: the calendar date as
// written, whatever follows it, or none when the field has no value.
func TestDay(t *testing.T) {
	const listErr = "d holds a list or mapping; use one date, YYYY-MM-DD"
	tests := []struct {
		name    string
		fm      string // the frontmatter's YAML
		want    string // the day as YYYY-MM-DD; "" for none
		wantErr string
	}{
		{name: "date", fm: "d: 2026-01-12", want: "2026-01-12"},
		{name: "quoted", fm: `d: "2026-01-12"`, want: "2026-01-12"},
		{name: "timestamp_zone", fm: "d: 2026-01-12T09:30:00Z", want: "2026-01-12"},
		{name: "timestamp_cut_unread", fm: "d: 2026-01-12T99:99", want: "2026-01-12"},
		{name: "space_timestamp", fm: `d: " 2026-01-12 09:30 "`, want: "2026-01-12"},
		{name: "list_of_one", fm: "d: [2026-01-12]", want: "2026-01-12"},
		{name: "missing", fm: "x: 1"},
		{name: "null", fm: "d: null"},
		{name: "blank", fm: `d: " "`},
		{name: "empty_list", fm: "d: []"},
		{name: "words", fm: "d: next week", wantErr: `d "next week" is not a date; use YYYY-MM-DD`},
		{name: "day_first", fm: "d: 12/01/2026", wantErr: "use YYYY-MM-DD"},
		{name: "no_such_month", fm: "d: 2026-13-01", wantErr: "use YYYY-MM-DD"},
		{name: "list_of_two", fm: "d: [2026-01-12, 2026-01-13]", wantErr: listErr},
		{name: "list_with_null", fm: "d: [2026-01-12, null]", wantErr: listErr},
		{name: "mapping", fm: "d: {a: 2026-01-12}", wantErr: listErr},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fm, err := vault.SplitFrontmatter([]byte("---\n" + tc.fm + "\n---\n"))
			if err != nil || fm == nil {
				t.Fatalf("bad test frontmatter %q: %v", tc.fm, err)
			}
			day, err := vault.Day(fm.Map, "d")
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Day = %v, %v; want an error containing %q", day, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Day: %v", err)
			}
			got := ""
			if !day.IsZero() {
				got = day.Format(time.DateOnly)
			}
			if got != tc.want {
				t.Errorf("Day = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEqual: two nodes are equal when they hold the same values the way
// Values reads them, whatever their quoting, aliases or tags.
func TestEqual(t *testing.T) {
	tests := []struct {
		name, a, b string // each the YAML of field x
		want       bool
	}{
		{name: "same", a: "x: a", b: "x: a", want: true},
		{name: "trimmed", a: `x: " a"`, b: "x: a", want: true},
		{name: "quoted", a: `x: "1"`, b: "x: 1", want: true},
		{name: "null_is_blank", a: "x: null", b: `x: ""`, want: true},
		{name: "blank_is_null", a: "x:", b: "x: ~", want: true},
		{name: "different", a: "x: a", b: "x: b"},
		{name: "scalar_vs_list", a: "x: a", b: "x: [a]"},
		{name: "null_vs_empty_list", a: "x:", b: "x: []"},
		{name: "list_styles", a: "x: [a, b]", b: "x:\n  - a\n  - b", want: true},
		{name: "list_order", a: "x: [a, b]", b: "x: [b, a]"},
		{name: "list_length", a: "x: [a]", b: "x: [a, b]"},
		{name: "mapping", a: "x: {b: 1}", b: "x:\n  b: 1", want: true},
		{name: "mapping_value", a: "x: {b: 1}", b: "x: {b: 2}"},
		{name: "mapping_key", a: "x: {b: 1}", b: "x: {c: 1}"},
		{name: "alias", a: "y: &v [a]\nx: *v", b: "x: [a]", want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, b := field(t, tc.a, "x"), field(t, tc.b, "x")
			if got := vault.Equal(a, b); got != tc.want {
				t.Errorf("Equal(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
			if got := vault.Equal(b, a); got != tc.want {
				t.Errorf("Equal(%q, %q) = %v, want %v", tc.b, tc.a, got, tc.want)
			}
		})
	}
	if !vault.Equal(nil, nil) || vault.Equal(nil, field(t, "x:", "x")) {
		t.Error("a missing field equals only a missing field")
	}
}

func field(t *testing.T, fm, key string) *yaml.Node {
	t.Helper()
	parsed, err := vault.SplitFrontmatter([]byte("---\n" + fm + "\n---\n"))
	if err != nil {
		t.Fatalf("SplitFrontmatter(%q): %v", fm, err)
	}
	return vault.Field(parsed.Map, key)
}
