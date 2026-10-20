package vault_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/arkoes07/prep-mcp/internal/testvault"
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
		// Escaped, as Edit cannot change a block holding them raw; a key with a break is an explicit "? key".
		{
			name: "line_and_paragraph_separators", src: doc("a: 1\n", ""), set: `{"s\u2029":"x\u2028y"}`,
			want: doc("a: 1\n? \"s\\P\"\n: \"x\\Ly\"\n", ""),
		},

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

// For any frontmatter, key and JSON value, Set and Apply fail or give a file
// that re-reads with the key set to the value and the rest unchanged: the
// other keys in order, the body, and no line ending the file did not use.
func FuzzEditSet(f *testing.F) {
	f.Add(doc("a: 1\nb: x  # c\nc: 3\n", "body\n"), "b", `"y"`)
	f.Add(doc("kind: x          # a | b\ndate:\ntags: []\n", "x\n"), "date", `"2026-01-20"`)
	f.Add(doc("# top\na: 1\n\n# about b\nb: 2\n", "x\n"), "a", `5`)
	f.Add(doc("metrics:\n  x: 1\n  y: 2\nz: 1\n", ""), "metrics", `{"p":[1,"q",{"r":null}]}`)
	f.Add(doc("s: |\n  one\n  two\n# after\nt: 1\n", "x"), "s", `"yes"`)
	f.Add(doc("tags:\n  - a\n  - b\n", "---\nbody"), "tags", `["c"]`)
	f.Add(doc("m: {b: 2}\n", "x\n"), "m", `{"b":1}`)
	f.Add(doc("a: &x 1\nb: *x\n", ""), "a", `2`)
	f.Add(doc("a: 1\n", ""), "key: with colon", `true`)
	f.Add("---\r\na: 1\r\nb: 2\r\n---\r\nbody\r\n", "n", `"two\nlines"`)
	f.Add("\xef\xbb\xbf---\ntype: x\n---\nb\n", "type", `null`)
	f.Add("---\n---\n", "a", `[]`)
	f.Add("---\n{a: 1, b: 2}\n---\n", "a", `5`)
	f.Add("---\na: 1\n\r\r\r\rb: 2\n---\n", "a", `5`)
	f.Add("---\na: 1\u2028\u2028\u2028b: 2\n---\n", "b", `5`)
	f.Add("---\n0:\n 0:\n #\n---", "1", `""`) // yaml.v3 hangs the # on another node, but no value changes

	templates, err := filepath.Glob(filepath.Join(testvault.Copy(f), "templates", "*.md"))
	if err != nil {
		f.Fatal(err)
	}
	for _, p := range templates {
		b, err := os.ReadFile(p)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(string(b), "status", `"open"`)
	}

	f.Fuzz(func(t *testing.T, src, key, value string) {
		fm, err := vault.SplitFrontmatter([]byte(src))
		if fm == nil || err != nil {
			return
		}

		// The key and value arrive as update_frontmatter gets them: as JSON,
		// which holds only valid UTF-8.
		quoted, _ := json.Marshal(key)
		set, err := vault.JSONToNode(json.RawMessage("{" + string(quoted) + ":" + value + "}"))
		if err != nil {
			return
		}
		key = set.Content[0].Value
		val := set.Content[1]

		ed := fm.Edit()
		ed.Set(key, val)
		out, _, err := ed.Apply()
		if err != nil {
			return
		}

		re, err := vault.SplitFrontmatter(out)
		if re == nil || err != nil {
			t.Fatalf("the result does not re-read (%v):\n%q", err, out)
		}
		if !vault.Equal(vault.Field(re.Map, key), val) {
			t.Errorf("%q does not re-read as %q:\n%q", key, vault.Encode(val), out)
		}
		if old, now := listKeysExcept(fm.Map, key), listKeysExcept(re.Map, key); !slices.Equal(old, now) {
			t.Errorf("other keys changed:\n got  %q\n want %q", now, old)
		}
		for k, v := range vault.Pairs(fm.Map) {
			if k.Value != key && !vault.Equal(v, vault.Field(re.Map, k.Value)) {
				t.Errorf("%q changed:\n%q", k.Value, out)
			}
		}
		if !bytes.Equal(re.Body(), fm.Body()) {
			t.Errorf("body changed:\n%q\nwant\n%q", re.Body(), fm.Body())
		}
		lf, crlf := countEndings([]byte(src))
		outLF, outCRLF := countEndings(out)
		if lf == 0 && outLF > 0 || crlf == 0 && outCRLF > 0 {
			t.Errorf("a line ending the file did not use was written:\n%q", out)
		}
	})
}

// listKeysExcept returns the top-level keys of m but skip, in order.
func listKeysExcept(m *yaml.Node, skip string) []string {
	var out []string
	for k := range vault.Pairs(m) {
		if k.Value != skip {
			out = append(out, k.Value)
		}
	}
	return out
}

// countEndings counts the lines of b that end in LF alone and those that end in CRLF.
func countEndings(b []byte) (lf, crlf int) {
	crlf = bytes.Count(b, []byte("\r\n"))
	return bytes.Count(b, []byte("\n")) - crlf, crlf
}

// Frontmatter that is not one key per line cannot be edited line by line, and
// the error says how to make it so.
func TestEditNeedsAKeyPerLine(t *testing.T) {
	for name, src := range map[string]string{
		"flow_mapping":       "---\n{a: 1, b: 2}\n---\n",
		"flow_mapping_lines": "---\n{a: 1,\n b: 2}\n---\n",
		"empty_flow_mapping": "---\n{}\n---\n",
		"lone_cr":            "---\na: 1\rb: 2\n---\n",
		"nel":                "---\na: \"x\u0085y\"\nb: 2\n---\n",
		"ls":                 "---\na: 1 # x\u2028\nb: 2\n---\n",
		"ps":                 "---\na: 1\u2029\nb: 2\n---\n",
	} {
		t.Run(name, func(t *testing.T) {
			fm, err := vault.SplitFrontmatter([]byte(src))
			if fm == nil || err != nil {
				t.Fatalf("test source has no valid frontmatter: %v", err)
			}
			ed := fm.Edit()
			ed.Set("b", node(t, `{"v":3}`).Content[1])
			_, _, err = ed.Apply()
			var fe *vault.FrontmatterError
			if !errors.As(err, &fe) {
				t.Fatalf("Apply error = %v, want a *vault.FrontmatterError", err)
			}
			mustContain(t, err, "cannot be edited key by key", "replace_doc")
		})
	}
}

var spacesBeforeHash = regexp.MustCompile(` +#`)

func squeezeComments(s string) string { return spacesBeforeHash.ReplaceAllString(s, " #") }
