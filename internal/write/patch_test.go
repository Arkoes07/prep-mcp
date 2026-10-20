package write_test

import (
	"slices"
	"testing"

	"github.com/arkoes07/prep-mcp/internal/write"
)

// doc builds a file from a frontmatter block (with its own newlines) and a body.
func doc(fm, body string) string { return "---\n" + fm + "---\n" + body }

func TestApplyPatch(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		set      string // JSON object, "" for none
		unset    []string
		add, rem []string
		want     string // exact output; for a no-op it is ignored and out must equal src
		noop     bool   // out must equal src
		warnings []string
		wantErr  []string
	}{
		// How an edit splices is vault's (TestEdit); these are what a patch means.
		{name: "stamp_new", src: doc("a: 1\n", "x\n"), set: `{"a":2}`, want: doc("a: 2\nupdated: 2026-01-14\n", "x\n")},
		// An existing updated line is replaced in place, not duplicated.
		{name: "updated_existing", src: doc("a: 1\nupdated: 2025-12-01\n", "x\n"), set: `{"a":2}`, want: doc("a: 2\nupdated: 2026-01-14\n", "x\n")},
		{name: "explicit_updated", src: doc("a: 1\n", ""), set: `{"updated":"2020-01-01"}`, want: doc("a: 1\nupdated: 2020-01-01\n", "")},
		// Setting updated to the value it has keeps it, even alongside another change.
		{
			name: "explicit_updated_unchanged", src: doc("a: 1\nupdated: 2020-01-01\n", ""), set: `{"a":2,"updated":"2020-01-01"}`,
			want: doc("a: 2\nupdated: 2020-01-01\n", ""),
		},
		{name: "unset_updated", src: doc("a: 1\nupdated: 2025-12-01\n", ""), unset: []string{"updated"}, want: doc("a: 1\n", "")},
		// A value that reads the same is no change, so no stamp either.
		{name: "set_padded_same", src: doc("a: x\n", "x\n"), set: `{"a":" x"}`, noop: true},
		{name: "unset_missing", src: doc("a: 1\n", "x\n"), unset: []string{"q"}, noop: true},

		{name: "add_tags", src: doc("tags: [a, b]\n", "x\n"), add: []string{"C"}, want: doc("tags: [a, b, c]\nupdated: 2026-01-14\n", "x\n")},
		{name: "add_existing_tag", src: doc("tags: [a]\n", "x\n"), add: []string{"A"}, noop: true},
		{name: "remove_tags", src: doc("tags: [a, b]\n", "x\n"), rem: []string{"a"}, want: doc("tags: [b]\nupdated: 2026-01-14\n", "x\n")},
		{
			name: "remove_absent", src: doc("tags: [a]\n", "x\n"), rem: []string{"Z"}, noop: true,
			warnings: []string{`tag "z" is not on this document, so there was nothing to remove`},
		},
		{
			name: "remove_some_absent", src: doc("tags: [a, b]\n", "x\n"), rem: []string{"z", "a"},
			want:     doc("tags: [b]\nupdated: 2026-01-14\n", "x\n"),
			warnings: []string{`tag "z" is not on this document, so there was nothing to remove`},
		},
		{name: "set_tags", src: doc("tags: [a]\n", "x\n"), set: `{"tags":["B","a"]}`, want: doc("tags: [b, a]\nupdated: 2026-01-14\n", "x\n")},

		{name: "tags_conflict", src: doc("a: 1\n", ""), set: `{"tags":["x"]}`, add: []string{"y"}, wantErr: []string{"add_tags"}},
		{name: "set_unset_same", src: doc("status: x\n", ""), set: `{"status":"y"}`, unset: []string{"status"}, wantErr: []string{"status"}},
		{name: "no_frontmatter", src: "# x\n", set: `{"a":1}`, wantErr: []string{"no frontmatter", "replace_doc"}},
		{name: "invalid_frontmatter", src: "---\na: 1\nb: : x\n---\nbody\n", set: `{"a":2}`, wantErr: []string{"line 3"}},
		{name: "invalid_frontmatter_flow", src: "---\na: 1\nb: [1\nc: 2\n---\nbody\n", set: `{"a":2}`, wantErr: []string{"invalid frontmatter near line"}},
		// Removing a takes the anchor b points at, so the edit does not re-read.
		{
			name: "not_verified", src: doc("a: &x 1\nb: *x\n", ""), unset: []string{"a"},
			wantErr: []string{"did not re-read as intended", "nothing was written", "replace_doc"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := write.Patch{Unset: tc.unset, AddTags: tc.add, RemoveTags: tc.rem}
			if tc.set != "" {
				p.Set = node(t, tc.set)
			}
			out, warnings, err := write.ApplyPatch([]byte(tc.src), p, today, true)
			if tc.wantErr != nil {
				mustContain(t, err, tc.wantErr...)
				return
			}
			if err != nil {
				t.Fatalf("ApplyPatch: %v", err)
			}
			if !slices.Equal(warnings, tc.warnings) {
				t.Errorf("warnings = %q, want %q", warnings, tc.warnings)
			}
			want := tc.want
			if tc.noop {
				want = tc.src
			}
			if got := string(out); got != want {
				t.Errorf("result:\n%q\nwant:\n%q", got, want)
			}
		})
	}
}
