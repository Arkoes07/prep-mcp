package write_test

import (
	"regexp"
	"slices"
	"testing"

	"github.com/arkoes07/prep-mcp/internal/vault"
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
		squeeze  bool // compare with runs of spaces before "#" collapsed (comment spacing is not pinned)
		wantErr  string
	}{
		{
			name: "set_existing", src: doc("a: 1\nb: x  # c\nc: 3\n", "body\n"), set: `{"b":"y"}`,
			want: doc("a: 1\nb: y # c\nc: 3\nupdated: 2026-01-14\n", "body\n"), squeeze: true,
		},
		{name: "order_kept", src: doc("a: 1\nb: 2\nc: 3\n", "x\n"), set: `{"b":9}`, want: doc("a: 1\nb: 9\nc: 3\nupdated: 2026-01-14\n", "x\n")},
		{name: "set_new", src: doc("a: 1\n", "x\n"), set: `{"z":1,"y":2}`, want: doc("a: 1\nz: 1\ny: 2\nupdated: 2026-01-14\n", "x\n")},
		{name: "unset", src: doc("a: 1\nb: 2\n", "x\n"), unset: []string{"b"}, want: doc("a: 1\nupdated: 2026-01-14\n", "x\n")},
		{name: "unset_block", src: doc("metrics:\n  x: 1\n  y: 2\nz: 1\n", "x\n"), unset: []string{"metrics"}, want: doc("z: 1\nupdated: 2026-01-14\n", "x\n")},
		{name: "unset_missing", src: doc("a: 1\n", "x\n"), unset: []string{"q"}, noop: true},
		{
			name: "aligned_comments", src: doc("kind: x          # a | b\ndate:\ntags: []\n", "x\n"), set: `{"date":"2026-01-20"}`,
			want: doc("kind: x          # a | b\ndate: 2026-01-20\ntags: []\nupdated: 2026-01-14\n", "x\n"),
		},
		{
			name: "blank_and_comment_lines", src: doc("# top\na: 1\n\n# about b\nb: 2\n", "x\n"), set: `{"a":5}`,
			want: doc("# top\na: 5\n\n# about b\nb: 2\nupdated: 2026-01-14\n", "x\n"),
		},
		{name: "value_list", src: doc("a: 1\n", ""), set: `{"f":["x","y"]}`, want: doc("a: 1\nf: [x, y]\nupdated: 2026-01-14\n", "")},
		{name: "value_map", src: doc("a: 1\n", ""), set: `{"m":{"p":1}}`, want: doc("a: 1\nm:\n  p: 1\nupdated: 2026-01-14\n", "")},
		{name: "date_string", src: doc("a: 1\n", ""), set: `{"d":"2026-01-20"}`, want: doc("a: 1\nd: 2026-01-20\nupdated: 2026-01-14\n", "")},
		{name: "ambiguous_string_word", src: doc("a: 1\n", ""), set: `{"s":"yes"}`, want: doc("a: 1\ns: \"yes\"\nupdated: 2026-01-14\n", "")},
		{name: "ambiguous_string_number", src: doc("a: 1\n", ""), set: `{"s":"123"}`, want: doc("a: 1\ns: \"123\"\nupdated: 2026-01-14\n", "")},
		{name: "wikilink", src: doc("a: 1\n", ""), set: `{"s":"[[x]]"}`, want: doc("a: 1\ns: '[[x]]'\nupdated: 2026-01-14\n", "")},

		{name: "add_tags_flow", src: doc("tags: [a, b]\n", "x\n"), add: []string{"C"}, want: doc("tags: [a, b, c]\nupdated: 2026-01-14\n", "x\n")},
		{
			name: "add_tags_block", src: doc("tags:\n  - a\n  - b\n", "x\n"), add: []string{"c"},
			want: doc("tags:\n  - a\n  - b\n  - c\nupdated: 2026-01-14\n", "x\n"),
		},
		{name: "add_tags_string", src: doc("tags: a, b\n", "x\n"), add: []string{"c"}, want: doc("tags: [a, b, c]\nupdated: 2026-01-14\n", "x\n")},
		{name: "add_tags_absent", src: doc("a: 1\n", "x\n"), add: []string{"c"}, want: doc("a: 1\ntags: [c]\nupdated: 2026-01-14\n", "x\n")},
		{name: "add_existing_tag", src: doc("tags: [a]\n", "x\n"), add: []string{"A"}, noop: true},
		{name: "remove_tags", src: doc("tags: [a, b]\n", "x\n"), rem: []string{"a"}, want: doc("tags: [b]\nupdated: 2026-01-14\n", "x\n")},
		{name: "remove_last", src: doc("tags: [a]\n", "x\n"), rem: []string{"a"}, want: doc("tags: []\nupdated: 2026-01-14\n", "x\n")},
		{
			name: "remove_absent", src: doc("tags: [a]\n", "x\n"), rem: []string{"Z"}, noop: true,
			warnings: []string{`tag "z" is not on this document, so there was nothing to remove`},
		},
		{
			name: "remove_some_absent", src: doc("tags: [a, b]\n", "x\n"), rem: []string{"z", "a"},
			want:     doc("tags: [b]\nupdated: 2026-01-14\n", "x\n"),
			warnings: []string{`tag "z" is not on this document, so there was nothing to remove`},
		},

		{name: "tags_conflict", src: doc("a: 1\n", ""), set: `{"tags":["x"]}`, add: []string{"y"}, wantErr: "add_tags"},
		{name: "set_unset_same", src: doc("status: x\n", ""), set: `{"status":"y"}`, unset: []string{"status"}, wantErr: "status"},
		{name: "explicit_updated", src: doc("a: 1\n", ""), set: `{"updated":"2020-01-01"}`, want: doc("a: 1\nupdated: 2020-01-01\n", "")},
		// An existing updated line is replaced in place, not duplicated.
		{name: "updated_existing", src: doc("a: 1\nupdated: 2025-12-01\n", "x\n"), set: `{"a":2}`, want: doc("a: 2\nupdated: 2026-01-14\n", "x\n")},
		{
			name: "body_edge_cases", src: doc("a: 1\n", "---\ntrailing   \n\ttabbed\nünï 世界\nno final newline"), set: `{"a":2}`,
			want: doc("a: 2\nupdated: 2026-01-14\n", "---\ntrailing   \n\ttabbed\nünï 世界\nno final newline"),
		},
		{name: "no_frontmatter", src: "# x\n", set: `{"a":1}`, wantErr: "no frontmatter"},
		{name: "invalid_frontmatter", src: "---\na: 1\nb: : x\n---\nbody\n", set: `{"a":2}`, wantErr: "line 3"},
		{name: "invalid_frontmatter_flow", src: "---\na: 1\nb: [1\nc: 2\n---\nbody\n", set: `{"a":2}`, wantErr: "invalid frontmatter near line"},
		{
			name: "crlf", src: "---\r\na: 1\r\nb: 2\r\n---\r\nbody\r\n", set: `{"a":5}`,
			want: "---\r\na: 5\r\nb: 2\r\nupdated: 2026-01-14\r\n---\r\nbody\r\n",
		},
		{name: "empty_frontmatter", src: "---\n---\n", set: `{"a":1}`, want: "---\na: 1\nupdated: 2026-01-14\n---\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := write.Patch{Unset: tc.unset, AddTags: tc.add, RemoveTags: tc.rem}
			if tc.set != "" {
				p.Set = node(t, tc.set)
			}
			out, warnings, err := write.ApplyPatch([]byte(tc.src), p, today, true)
			if tc.wantErr != "" {
				mustContain(t, err, tc.wantErr)
				if tc.name == "no_frontmatter" {
					mustContain(t, err, "replace_doc")
				}
				return
			}
			if err != nil {
				t.Fatalf("ApplyPatch: %v", err)
			}
			if !slices.Equal(warnings, tc.warnings) {
				t.Errorf("warnings = %q, want %q", warnings, tc.warnings)
			}
			got := string(out)
			if tc.noop {
				if got != tc.src {
					t.Errorf("no-op changed the bytes:\n%q\nwant\n%q", got, tc.src)
				}
				return
			}
			want := tc.want
			if tc.squeeze {
				got, want = squeezeComments(got), squeezeComments(want)
			}
			if got != want {
				t.Errorf("result:\n%q\nwant:\n%q", got, want)
			}

			// Whatever changed, the body is the same bytes and the result still parses.
			in, err := vault.SplitFrontmatter([]byte(tc.src))
			if err != nil || in == nil {
				t.Fatalf("test source has no valid frontmatter: %v", err)
			}
			re, err := vault.SplitFrontmatter(out)
			if err != nil || re == nil {
				t.Fatalf("result does not parse: %v\n%s", err, out)
			}
			if string(out[re.BodyOffset:]) != tc.src[in.BodyOffset:] {
				t.Errorf("body bytes changed:\n%q\nwant\n%q", out[re.BodyOffset:], tc.src[in.BodyOffset:])
			}
		})
	}
}

var spacesBeforeHash = regexp.MustCompile(` +#`)

func squeezeComments(s string) string { return spacesBeforeHash.ReplaceAllString(s, " #") }
