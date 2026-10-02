package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Arkoes07/prep-mcp/internal/testvault"
	"github.com/Arkoes07/prep-mcp/internal/write"
)

// create_doc's description is generated from the config, so the model never
// has to read it.
func TestCreateDocDescriptionComesFromConfig(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var desc string
	for _, tool := range res.Tools {
		if tool.Name == "create_doc" {
			desc = tool.Description
		}
	}
	for _, want := range []string{
		"- improvement: improvements/{slug}.md; required: status, exit_criteria",
		"- week: weeks/{week}.md; required: week",
		"- learning: learning/{slug}.md; required: title, track",
		"lowercase kebab-case",
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("create_doc description lacks %q:\n%s", want, desc)
		}
	}

	root := t.TempDir() // a vault whose config file is empty
	testvault.Write(t, root, "prep.config.yaml", "")
	cs = connectAt(t, root)
	res, err = cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if tool.Name == "create_doc" && !strings.Contains(tool.Description, "No types are configured") {
			t.Errorf("with no config the description should say how to add types:\n%s", tool.Description)
		}
	}
}

// One valid call per write tool succeeds and changes the right file.
func TestEachWriteToolAnswers(t *testing.T) {
	root, cs := connectRoot(t)
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	callOK := func(tool string, args map[string]any) write.Result {
		t.Helper()
		out, isErr := call(t, cs, tool, args)
		if isErr {
			t.Fatalf("%s: IsError: %s", tool, out)
		}
		var got write.Result
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("%s: result is not JSON: %v\n%s", tool, err, out)
		}
		if got.Warnings == nil {
			t.Errorf("%s: warnings must be a list, not null: %s", tool, out)
		}
		return got
	}

	// create_doc keeps the order of the frontmatter keys as sent.
	made := callOK("create_doc", map[string]any{"type": "learning", "frontmatter": json.RawMessage(`{"track":"ddia","title":"DDIA ch5","tags":["issue/buries-headlin"]}`), "body": "notes"})
	if made.Path != "learning/ddia-ch5.md" {
		t.Errorf("create_doc path = %q", made.Path)
	}
	if want := "---\ntype: learning\ntrack: ddia\ntitle: DDIA ch5\ntags: [issue/buries-headlin]\ncreated: 2026-01-14\n---\nnotes\n"; read(made.Path) != want {
		t.Errorf("created file:\n%q\nwant:\n%q", read(made.Path), want)
	}
	if len(made.Warnings) != 1 || !strings.Contains(made.Warnings[0], "issue/buries-headline") {
		t.Errorf("create_doc warnings = %q, want one naming issue/buries-headline", made.Warnings)
	}

	callOK("append_section", map[string]any{"path": "weeks/2026-w03.md", "heading": "Log", "content": "- Tue: x"})
	if got := read("weeks/2026-w03.md"); !strings.HasSuffix(got, "- Mon: deep dive, headline late\n- Tue: x\n") {
		t.Errorf("append_section result:\n%s", got)
	}

	callOK("update_frontmatter", map[string]any{"path": "improvements/buries-headline.md", "set": map[string]any{"status": "improving"}})
	if got := read("improvements/buries-headline.md"); !strings.Contains(got, "status: improving\n") || !strings.Contains(got, "updated: 2026-01-14\n") {
		t.Errorf("update_frontmatter result:\n%s", got)
	}

	out, _ := call(t, cs, "read_doc", map[string]any{"path": "weeks/2026-w03.md"})
	hash := regexp.MustCompile(`hash: ([0-9a-f]{64})`).FindStringSubmatch(out)[1]
	content := read("weeks/2026-w03.md") + "- Wed: y\n"
	replaced := callOK("replace_doc", map[string]any{"path": "weeks/2026-w03.md", "content": content, "expected_hash": hash})
	if read("weeks/2026-w03.md") != content || replaced.Hash == hash {
		t.Errorf("replace_doc did not write the new content")
	}
}

// Failures are tool results that say what to do next, never protocol errors,
// and nothing on disk changes.
func TestWriteErrorsAreToolResultsWithHints(t *testing.T) {
	root, cs := connectRoot(t)
	before, _ := os.ReadFile(filepath.Join(root, "weeks/2026-w03.md"))
	tests := []struct {
		name, tool string
		args       map[string]any
		want       []string
	}{
		{"path_outside", "append_section", map[string]any{"path": "../x.md", "heading": "Log", "content": "x"}, []string{"outside the vault; use a path relative"}},
		{"templates_read_only", "update_frontmatter", map[string]any{"path": "templates/week.md", "set": map[string]any{"a": 1}}, []string{"templates/ is read-only"}},
		{"missing_field", "create_doc", map[string]any{"type": "learning", "frontmatter": map[string]any{"title": "T"}}, []string{"missing required fields for learning: track"}},
		{"unknown_type", "create_doc", map[string]any{"type": "nope", "frontmatter": map[string]any{}}, []string{"configured types:", "improvement"}},
		{"exists", "create_doc", map[string]any{"type": "week", "frontmatter": map[string]any{"week": "2026-W03"}}, []string{"file exists: weeks/2026-w03.md", "append_section"}},
		{"stale_hash", "replace_doc", map[string]any{"path": "weeks/2026-w03.md", "content": "---\ntype: week\nweek: 2026-W03\n---\n", "expected_hash": "abc"}, []string{"re-read with read_doc, merge, retry"}},
		{"no_hash", "replace_doc", map[string]any{"path": "weeks/2026-w03.md", "content": "x", "expected_hash": ""}, []string{"expected_hash is required"}},
		{"set_not_object", "update_frontmatter", map[string]any{"path": "weeks/2026-w03.md", "add_tags": []string{"a"}, "set": map[string]any{"tags": []string{"b"}}}, []string{"set.tags cannot be combined"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, isErr := call(t, cs, tc.tool, tc.args)
			if !isErr {
				t.Fatalf("want IsError, got: %s", out)
			}
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("error %q lacks %q", out, w)
				}
			}
		})
	}
	after, _ := os.ReadFile(filepath.Join(root, "weeks/2026-w03.md"))
	if string(after) != string(before) {
		t.Error("a failed write changed the file")
	}
}
