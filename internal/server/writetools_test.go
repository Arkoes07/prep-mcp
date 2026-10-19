package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/arkoes07/prep-mcp/internal/testvault"
	"github.com/arkoes07/prep-mcp/internal/vault"
	"github.com/arkoes07/prep-mcp/internal/write"
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
		"must hold a date, YYYY-MM-DD, when set",
		"slug may hold folders (folder/name)",
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
	made := callOK("create_doc", map[string]any{
		"type":        "learning",
		"frontmatter": json.RawMessage(`{"track":"ddia","title":"DDIA ch5","tags":["issue/buries-headline"]}`),
		"body":        "notes",
	})
	if made.Path != "learning/ddia-ch5.md" {
		t.Errorf("create_doc path = %q", made.Path)
	}
	want := "---\ntype: learning\ntrack: ddia\ntitle: DDIA ch5\ntags: [issue/buries-headline]\n" +
		"created: 2026-01-14\n---\nnotes\n"
	if read(made.Path) != want {
		t.Errorf("created file:\n%q\nwant:\n%q", read(made.Path), want)
	}
	if len(made.Warnings) != 0 {
		t.Errorf("create_doc warnings = %q, want none", made.Warnings)
	}

	callOK("append_section", map[string]any{"path": "weeks/2026-w03.md", "heading": "Log", "content": "- Tue: x"})
	if got := read("weeks/2026-w03.md"); !strings.HasSuffix(got, "- Mon: deep dive, headline late\n- Tue: x\n") {
		t.Errorf("append_section result:\n%s", got)
	}

	callOK("update_frontmatter", map[string]any{"path": "improvements/buries-headline.md", "set": map[string]any{"status": "improving"}})
	if got := read("improvements/buries-headline.md"); !strings.Contains(got, "status: improving\n") || !strings.Contains(got, "updated: 2026-01-14\n") {
		t.Errorf("update_frontmatter result:\n%s", got)
	}

	callOK("edit_text", map[string]any{"path": "weeks/2026-w03.md", "old": "- Tue: x", "new": "- Tue: mock"})
	if got := read("weeks/2026-w03.md"); !strings.HasSuffix(got, "- Tue: mock\n") {
		t.Errorf("edit_text result:\n%s", got)
	}

	tmpl := callOK("edit_text", map[string]any{"path": "templates/week.md", "old": "Weekly review", "new": "Retro"})
	if got := read("templates/week.md"); !strings.Contains(got, "| Retro |") || len(tmpl.Warnings) != 0 {
		t.Errorf("edit_text on a template: warnings %q, file:\n%s", tmpl.Warnings, got)
	}

	out, _ := call(t, cs, "read_doc", map[string]any{"path": "weeks/2026-w03.md"})
	hash := regexp.MustCompile(`hash: ([0-9a-f]{64})`).FindStringSubmatch(out)[1]
	content := read("weeks/2026-w03.md") + "- Wed: y\n"
	replaced := callOK("replace_doc", map[string]any{"path": "weeks/2026-w03.md", "content": content, "expected_hash": hash})
	if read("weeks/2026-w03.md") != content || replaced.Hash == hash {
		t.Errorf("replace_doc did not write the new content")
	}
}

// Each write tool's failures are tool results that say what to do next, never
// protocol errors, and nothing on disk changes. What each failure says is
// tested in the write package.
func TestWriteErrorsAreToolResultsWithHints(t *testing.T) {
	root, cs := connectRoot(t)
	before, _ := os.ReadFile(filepath.Join(root, "weeks/2026-w03.md"))
	tests := []struct {
		name, tool string
		args       map[string]any
		want       []string
	}{
		{"path_outside", "append_section", map[string]any{"path": "../x.md", "heading": "Log", "content": "x"}, []string{"outside the vault; use a path relative"}},
		{"exists", "create_doc", map[string]any{"type": "week", "frontmatter": map[string]any{"week": "2026-W03"}}, []string{"file exists: weeks/2026-w03.md", "append_section"}},
		{"stale_hash", "replace_doc", map[string]any{"path": "weeks/2026-w03.md", "content": "---\ntype: week\nweek: 2026-W03\n---\n", "expected_hash": "abc"}, []string{"re-read with read_doc, merge, retry"}},
		{
			"edit_not_found",
			"edit_text",
			map[string]any{"path": "weeks/2026-w03.md", "old": "nope", "new": "x"},
			[]string{"read_doc it and copy the text exactly"},
		},
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

func batchOp(tool string, args any) map[string]any {
	return map[string]any{"tool": tool, "args": args}
}

// batch takes the single tools' arguments, keeps frontmatter key order, and
// returns a result per op plus the commit fields, with lists never null.
func TestBatchTool(t *testing.T) {
	root, cs := connectRoot(t)
	ops := []any{
		batchOp("create_doc", map[string]any{
			"type":        "learning",
			"frontmatter": json.RawMessage(`{"track":"ddia","title":"DDIA ch5","tags":["issue/buries-headline"]}`),
			"body":        "notes",
		}),
		batchOp("append_section", map[string]any{"path": "weeks/2026-w03.md", "heading": "Log", "content": "- Tue: x"}),
		batchOp("update_frontmatter", map[string]any{
			"path": "improvements/buries-headline.md",
			"set":  map[string]any{"status": "improving"},
		}),
		batchOp("edit_text", map[string]any{"path": "templates/week.md", "old": "Weekly review", "new": "Retro"}),
	}

	out, isErr := call(t, cs, "batch", map[string]any{"ops": ops})

	if isErr {
		t.Fatalf("IsError: %s", out)
	}
	var got struct {
		Results []struct {
			Path     string   `json:"path"`
			Hash     string   `json:"hash"`
			Warnings []string `json:"warnings"`
		} `json:"results"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, out)
	}
	wantPaths := []string{"learning/ddia-ch5.md", "weeks/2026-w03.md", "improvements/buries-headline.md", "templates/week.md"}
	if len(got.Results) != len(wantPaths) {
		t.Fatalf("results = %d, want %d:\n%s", len(got.Results), len(wantPaths), out)
	}
	for i, r := range got.Results {
		b, err := os.ReadFile(filepath.Join(root, r.Path))
		if err != nil {
			t.Fatal(err)
		}
		if r.Path != wantPaths[i] || r.Hash != vault.Hash(b) {
			t.Errorf("result %d = %s %s, want %s with the hash of the file", i, r.Path, r.Hash, wantPaths[i])
		}
		if r.Warnings == nil {
			t.Errorf("result %d: warnings must be a list, not null:\n%s", i, out)
		}
	}
	if got.Warnings == nil || strings.Contains(out, `"commit"`) {
		t.Errorf("want warnings [] and no commit without git:\n%s", out)
	}
	if w := got.Results[0].Warnings; len(w) != 0 {
		t.Errorf("create_doc op warnings = %q, want none", w)
	}
	if w := got.Results[3].Warnings; len(w) != 0 {
		t.Errorf("template op warnings = %q, want none", w)
	}
	created, err := os.ReadFile(filepath.Join(root, "learning/ddia-ch5.md"))
	if err != nil {
		t.Fatal(err)
	}
	wantCreated := "---\ntype: learning\ntrack: ddia\ntitle: DDIA ch5\ntags: [issue/buries-headline]\n" +
		"created: 2026-01-14\n---\nnotes\n"
	if string(created) != wantCreated {
		t.Errorf("created file:\n%q\nwant (frontmatter keys in the order sent):\n%q", created, wantCreated)
	}
}

// Ops that do not decode and ops that fail their checks come back together,
// as one tool error, and nothing is written.
func TestBatchToolErrors(t *testing.T) {
	root, cs := connectRoot(t)
	files := []string{"weeks/2026-w03.md", "improvements/buries-headline.md"}
	before := map[string]string{}
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatal(err)
		}
		before[f] = string(b)
	}
	appendLog := map[string]any{"path": "weeks/2026-w03.md", "heading": "Log", "content": "- Tue: x"}
	tooMany := make([]any, 201)
	for i := range tooMany {
		tooMany[i] = batchOp("append_section", appendLog)
	}
	tests := []struct {
		name string
		args map[string]any
		want []string
	}{
		{
			name: "unknown_tool",
			args: map[string]any{"ops": []any{
				batchOp("append_section", appendLog),
				batchOp("delete_doc", map[string]any{"path": "weeks/2026-w03.md"}),
			}},
			want: []string{
				"1 of 2 ops failed",
				`op 2 (delete_doc): unknown tool "delete_doc"`,
				"create_doc, append_section, update_frontmatter, replace_doc or edit_text",
			},
		},
		{
			name: "unknown_argument",
			args: map[string]any{"ops": []any{
				batchOp("append_section", map[string]any{
					"path":    "weeks/2026-w03.md",
					"heading": "Log",
					"contnet": "x",
				}),
			}},
			want: []string{"op 1 (append_section)", `unknown field "contnet"`, "append_section's own arguments"},
		},
		{
			name: "decode_and_check_failures_together",
			args: map[string]any{"ops": []any{
				batchOp("update_frontmatter", map[string]any{"path": "improvements/buries-headline.md", "set": "open"}),
				batchOp("append_section", map[string]any{"path": "nope.md", "heading": "Log", "content": "x"}),
				batchOp("append_section", appendLog),
			}},
			want: []string{
				"2 of 3 ops failed",
				"op 1 (update_frontmatter): args.set has the wrong type (a string)",
				"op 2 (append_section nope.md): not found",
			},
		},
		{
			name: "empty",
			args: map[string]any{"ops": []any{}},
			want: []string{"pass at least one op"},
		},
		{
			name: "too_many",
			args: map[string]any{"ops": tooMany},
			want: []string{"201 ops", "at most 200 per call"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, isErr := call(t, cs, "batch", tc.args)
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
	for _, f := range files {
		if b, _ := os.ReadFile(filepath.Join(root, f)); string(b) != before[f] {
			t.Errorf("a failed batch changed %s", f)
		}
	}
}
