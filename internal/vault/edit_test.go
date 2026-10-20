package vault_test

import (
	"regexp"
	"testing"

	"github.com/arkoes07/prep-mcp/internal/vault"
)

// doc builds a file from a frontmatter block (with its own newlines) and a body.
func doc(fm, body string) string { return "---\n" + fm + "---\n" + body }

func TestEdit(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		set     string   // JSON object, "" for none
		unset   []string // unset before set
		tags    []string // SetTags after set; nil for no call
		want    string   // exact output; ignored when noop
		noop    bool     // out must equal src, and Changed is false
		squeeze bool     // compare with runs of spaces before "#" collapsed (comment spacing is not pinned)
	}{
		{
			name: "set_existing", src: doc("a: 1\nb: x  # c\nc: 3\n", "body\n"), set: `{"b":"y"}`,
			want: doc("a: 1\nb: y # c\nc: 3\n", "body\n"), squeeze: true,
		},
		{name: "order_kept", src: doc("a: 1\nb: 2\nc: 3\n", "x\n"), set: `{"b":9}`, want: doc("a: 1\nb: 9\nc: 3\n", "x\n")},
		{name: "set_new", src: doc("a: 1\n", "x\n"), set: `{"z":1,"y":2}`, want: doc("a: 1\nz: 1\ny: 2\n", "x\n")},
		{name: "set_same", src: doc("a: 1\n", "x\n"), set: `{"a":1}`, noop: true},
		{name: "set_padded_same", src: doc("a: x\n", "x\n"), set: `{"a":" x"}`, noop: true},
		{name: "set_mapping_changed", src: doc("m: {b: 2}\n", "x\n"), set: `{"m":{"b":1}}`, want: doc("m:\n  b: 1\n", "x\n")},
		{name: "unset", src: doc("a: 1\nb: 2\n", "x\n"), unset: []string{"b"}, want: doc("a: 1\n", "x\n")},
		{name: "unset_block", src: doc("metrics:\n  x: 1\n  y: 2\nz: 1\n", "x\n"), unset: []string{"metrics"}, want: doc("z: 1\n", "x\n")},
		{name: "unset_missing", src: doc("a: 1\n", "x\n"), unset: []string{"q"}, noop: true},
		{
			name: "aligned_comments", src: doc("kind: x          # a | b\ndate:\ntags: []\n", "x\n"), set: `{"date":"2026-01-20"}`,
			want: doc("kind: x          # a | b\ndate: 2026-01-20\ntags: []\n", "x\n"),
		},
		{
			name: "blank_and_comment_lines", src: doc("# top\na: 1\n\n# about b\nb: 2\n", "x\n"), set: `{"a":5}`,
			want: doc("# top\na: 5\n\n# about b\nb: 2\n", "x\n"),
		},
		{name: "value_list", src: doc("a: 1\n", ""), set: `{"f":["x","y"]}`, want: doc("a: 1\nf: [x, y]\n", "")},
		{name: "value_map", src: doc("a: 1\n", ""), set: `{"m":{"p":1}}`, want: doc("a: 1\nm:\n  p: 1\n", "")},
		{name: "date_string", src: doc("a: 1\n", ""), set: `{"d":"2026-01-20"}`, want: doc("a: 1\nd: 2026-01-20\n", "")},
		{name: "ambiguous_string_word", src: doc("a: 1\n", ""), set: `{"s":"yes"}`, want: doc("a: 1\ns: \"yes\"\n", "")},
		{name: "ambiguous_string_number", src: doc("a: 1\n", ""), set: `{"s":"123"}`, want: doc("a: 1\ns: \"123\"\n", "")},
		{name: "wikilink", src: doc("a: 1\n", ""), set: `{"s":"[[x]]"}`, want: doc("a: 1\ns: '[[x]]'\n", "")},

		{name: "tags_flow", src: doc("tags: [a, b]\n", "x\n"), tags: []string{"a", "b", "c"}, want: doc("tags: [a, b, c]\n", "x\n")},
		{
			name: "tags_block", src: doc("tags:\n  - a\n  - b\n", "x\n"), tags: []string{"a", "b", "c"},
			want: doc("tags:\n  - a\n  - b\n  - c\n", "x\n"),
		},
		{name: "tags_string", src: doc("tags: a, b\n", "x\n"), tags: []string{"a", "b", "c"}, want: doc("tags: [a, b, c]\n", "x\n")},
		{name: "tags_absent", src: doc("a: 1\n", "x\n"), tags: []string{"c"}, want: doc("a: 1\ntags: [c]\n", "x\n")},
		{name: "tags_same", src: doc("tags: [a]\n", "x\n"), tags: []string{"a"}, noop: true},
		{name: "tags_emptied", src: doc("tags: [a]\n", "x\n"), tags: []string{}, want: doc("tags: []\n", "x\n")},

		{
			name: "body_edge_cases", src: doc("a: 1\n", "---\ntrailing   \n\ttabbed\nünï 世界\nno final newline"), set: `{"a":2}`,
			want: doc("a: 2\n", "---\ntrailing   \n\ttabbed\nünï 世界\nno final newline"),
		},
		{
			name: "crlf", src: "---\r\na: 1\r\nb: 2\r\n---\r\nbody\r\n", set: `{"a":5,"n":1}`,
			want: "---\r\na: 5\r\nb: 2\r\nn: 1\r\n---\r\nbody\r\n",
		},
		{name: "empty_frontmatter", src: "---\n---\n", set: `{"a":1}`, want: "---\na: 1\n---\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fm, err := vault.SplitFrontmatter([]byte(tc.src))
			if err != nil || fm == nil {
				t.Fatalf("test source has no valid frontmatter: %v", err)
			}
			parsed := vault.Encode(fm.Map)

			ed := fm.Edit()
			for _, k := range tc.unset {
				ed.Unset(k)
			}
			if tc.set != "" {
				for k, v := range vault.Pairs(node(t, tc.set)) {
					ed.Set(k.Value, v)
				}
			}
			if tc.tags != nil {
				ed.SetTags(tc.tags)
			}
			if ed.Changed() == tc.noop {
				t.Errorf("Changed() = %v, want %v", ed.Changed(), !tc.noop)
			}
			out, re, err := ed.Apply()
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if vault.Encode(fm.Map) != parsed {
				t.Errorf("the edit changed the parsed nodes:\n%s\nwant\n%s", vault.Encode(fm.Map), parsed)
			}

			got, want := string(out), tc.want
			if tc.noop {
				want = tc.src
			}
			if tc.squeeze {
				got, want = squeezeComments(got), squeezeComments(want)
			}
			if got != want {
				t.Errorf("result:\n%q\nwant:\n%q", got, want)
			}
			// Whatever changed, the body is the same bytes, and re is out re-read.
			if string(re.Body()) != string(fm.Body()) {
				t.Errorf("body bytes changed:\n%q\nwant\n%q", re.Body(), fm.Body())
			}
			again, err := vault.SplitFrontmatter(out)
			if err != nil || vault.Encode(again.Map) != vault.Encode(re.Map) {
				t.Errorf("the returned frontmatter is not the result re-read: %v", err)
			}
		})
	}
}

// Setting a key the edit already changed applies, even to the file's value.
func TestEditSetAfterEdit(t *testing.T) {
	fm, err := vault.SplitFrontmatter([]byte(doc("a: 1\nb: 2\n", "")))
	if err != nil {
		t.Fatal(err)
	}
	ed := fm.Edit()
	ed.Unset("a")
	ed.Set("a", node(t, `{"v":1}`).Content[1])
	ed.Set("b", node(t, `{"v":3}`).Content[1])
	ed.Set("b", node(t, `{"v":2}`).Content[1])
	out, _, err := ed.Apply()
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if want := doc("a: 1\nb: 2\n", ""); string(out) != want {
		t.Errorf("result:\n%q\nwant:\n%q", out, want)
	}
}

// An edit that would not re-read as intended returns an error, not bytes.
func TestEditVerifies(t *testing.T) {
	// b points at a's anchor, which removing a takes with it.
	fm, err := vault.SplitFrontmatter([]byte(doc("a: &x 1\nb: *x\n", "")))
	if err != nil {
		t.Fatal(err)
	}
	ed := fm.Edit()
	ed.Unset("a")
	out, re, err := ed.Apply()
	mustContain(t, err, "the edited frontmatter did not re-read as intended")
	if out != nil || re != nil {
		t.Errorf("Apply returned %q and %v with its error, want nothing", out, re)
	}
}

var spacesBeforeHash = regexp.MustCompile(` +#`)

func squeezeComments(s string) string { return spacesBeforeHash.ReplaceAllString(s, " #") }
