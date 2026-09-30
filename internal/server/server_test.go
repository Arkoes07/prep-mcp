package server

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arkoes07/prep-mcp/internal/config"
	"github.com/Arkoes07/prep-mcp/internal/testvault"
	"github.com/Arkoes07/prep-mcp/internal/vault"
	"github.com/Arkoes07/prep-mcp/internal/write"
)

// connect serves a temp copy of the example vault to an in-memory client.
func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	_, cs := connectRoot(t)
	return cs
}

// connectRoot is connect, also returning the vault directory so tests can look at the files.
func connectRoot(t *testing.T) (string, *mcp.ClientSession) {
	t.Helper()
	root := testvault.Copy(t)
	return root, connectAt(t, root)
}

// connectAt serves the vault at root, with its own config, to an in-memory client.
func connectAt(t *testing.T, root string) *mcp.ClientSession {
	t.Helper()
	cfg, err := config.Load(filepath.Join(root, "prep.config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(root, cfg, testvault.Clock())
	if err != nil {
		t.Fatal(err)
	}
	// A nil Git: the server tests do not commit.
	return serve(t, New(v, write.New(v, cfg, nil, testvault.Clock()), cfg, "test"))
}

func serve(t *testing.T, s *mcp.Server) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := s.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error (want a tool result): %v", name, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String(), res.IsError
}

func TestToolSet(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tool := range res.Tools {
		got = append(got, tool.Name)
	}
	slices.Sort(got)
	want := []string{"append_section", "create_doc", "get_context", "list_docs", "list_tags", "read_doc", "replace_doc", "search_text", "update_frontmatter"}
	if !slices.Equal(got, want) {
		t.Errorf("tools = %v, want %v", got, want)
	}
}

// neutralVault is a vault whose config uses no domain words, so the check
// below sees only the words the Go code itself puts into descriptions.
// create_doc's description lists the configured type names, and the example
// config's names are domain words by design.
func neutralVault(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	testvault.Write(t, root, "prep.config.yaml", "types:\n  note:\n    folder: notes\n    filename: \"{slug}.md\"\n    required: [title]\n  log:\n    folder: logs\n    filename: \"{date}-{slug}.md\"\n    required: [date]\n")
	return root
}

// Tool descriptions and schemas are shown to the model; they must stay
// domain-neutral like the rest of the Go code.
func TestDescriptionsAreDomainNeutral(t *testing.T) {
	b, err := os.ReadFile("../../cmd/prep-mcp/testdata/forbidden-words.txt")
	if err != nil {
		t.Fatal(err)
	}
	var words []string
	for _, line := range strings.Split(string(b), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			words = append(words, regexp.QuoteMeta(line))
		}
	}
	if len(words) == 0 {
		t.Fatal("no forbidden words loaded")
	}
	re := regexp.MustCompile(`(?i)\b(` + strings.Join(words, "|") + `)\b`)

	cs := connectAt(t, neutralVault(t))
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		enc, err := json.Marshal(tool)
		if err != nil {
			t.Fatal(err)
		}
		if m := re.FindString(string(enc)); m != "" {
			t.Errorf("tool %s mentions %q in its description or schema", tool.Name, m)
		}
	}
}

func TestEachToolAnswers(t *testing.T) {
	cs := connect(t)
	tests := []struct {
		tool string
		args map[string]any
		want []string // substrings of the result
	}{
		{"get_context", map[string]any{"date": "2026-01-14"}, []string{"==== CLAUDE.md ====", "path: weeks/2026-w03.md", "==== Tags ===="}},
		{"get_context", nil, []string{"==== Current week: "}},
		{"list_docs", map[string]any{"tags": []string{"issue/buries-headline"}}, []string{`"count":1`, `2026-01-12`}},
		{"list_docs", map[string]any{"where": map[string]any{"status": []string{"open", "improving"}}}, []string{`"count":2`}},
		{"read_doc", map[string]any{"path": "weeks/2026-w03.md"}, []string{"path: weeks/2026-w03.md", "hash: ", "## Log"}},
		{"search_text", map[string]any{"query": "trade-off"}, []string{`"count":5`}},
		{"list_tags", map[string]any{"prefix": "issue/"}, []string{`"count":2`, `issue/missing-tradeoff`}},
	}
	for _, tc := range tests {
		t.Run(tc.tool, func(t *testing.T) {
			out, isErr := call(t, cs, tc.tool, tc.args)
			if isErr {
				t.Fatalf("IsError: %s", out)
			}
			for _, s := range tc.want {
				if !strings.Contains(out, s) {
					t.Errorf("result lacks %q:\n%s", s, out)
				}
			}
		})
	}
}

func TestListDocsIsMetadataOnly(t *testing.T) {
	cs := connect(t)
	out, isErr := call(t, cs, "list_docs", map[string]any{"tags": []string{"issue/"}})
	if isErr {
		t.Fatal(out)
	}
	var got struct {
		Docs []map[string]any `json:"docs"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, out)
	}
	if len(got.Docs) != 2 {
		t.Fatalf("docs = %d, want 2", len(got.Docs))
	}
	for _, key := range []string{"path", "type", "title", "tags", "date", "kind"} {
		if _, ok := got.Docs[0][key]; !ok {
			t.Errorf("doc lacks %q: %v", key, got.Docs[0])
		}
	}
	for _, key := range []string{"body", "content"} {
		if _, ok := got.Docs[0][key]; ok {
			t.Errorf("doc has %q; list_docs must return metadata only", key)
		}
	}
}

// Failures are tool results with a next step, not protocol errors.
func TestErrorsAreToolResultsWithHints(t *testing.T) {
	cs := connect(t)
	tests := []struct {
		name string
		tool string
		args map[string]any
		want string
	}{
		{"path_outside_vault", "read_doc", map[string]any{"path": "../../PRD.md"}, "outside the vault; use a path relative"},
		{"missing_file", "read_doc", map[string]any{"path": "nope.md"}, "use list_docs or search_text"},
		{"bad_since", "list_docs", map[string]any{"since": "13-01-2026"}, "since: use YYYY-MM-DD"},
		{"bad_date", "get_context", map[string]any{"date": "yesterday"}, "date: use YYYY-MM-DD"},
		{"bad_sort", "list_docs", map[string]any{"sort": "newest"}, "date_desc"},
		{"empty_query", "search_text", map[string]any{"query": ""}, "pass the text to search for"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, isErr := call(t, cs, tc.tool, tc.args)
			if !isErr {
				t.Fatalf("want IsError, got: %s", out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("error %q lacks %q", out, tc.want)
			}
		})
	}
}

func TestUnknownTypeNamesTheConfiguredOnes(t *testing.T) {
	cs := connect(t)
	out, isErr := call(t, cs, "list_docs", map[string]any{"type": "nope"})
	if isErr {
		t.Fatal(out)
	}
	for _, s := range []string{`"count":0`, `is not configured; configured types:`, "improvement"} {
		if !strings.Contains(out, s) {
			t.Errorf("result lacks %q: %s", s, out)
		}
	}
}

func TestPanicBecomesToolError(t *testing.T) {
	log.SetOutput(io.Discard) // guard logs the stack; keep test output clean
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	s := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "boom", Description: "panics"},
		guard("boom", func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
			panic("kaboom")
		}))
	mcp.AddTool(s, &mcp.Tool{Name: "fine", Description: "works"},
		guard("fine", func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
			return text("still here"), nil, nil
		}))
	cs := serve(t, s)

	out, isErr := call(t, cs, "boom", map[string]any{})
	if !isErr || !strings.Contains(out, "internal error in boom") {
		t.Errorf("boom = %q (IsError %v), want an internal error result", out, isErr)
	}
	if strings.Contains(out, "goroutine") {
		t.Errorf("the stack trace leaked to the client: %s", out)
	}
	out, isErr = call(t, cs, "fine", map[string]any{})
	if isErr || out != "still here" {
		t.Errorf("call after a panic = %q (IsError %v), want the server to keep working", out, isErr)
	}
}

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
	callOK := func(tool string, args map[string]any) writeOut {
		t.Helper()
		out, isErr := call(t, cs, tool, args)
		if isErr {
			t.Fatalf("%s: IsError: %s", tool, out)
		}
		var got writeOut
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
