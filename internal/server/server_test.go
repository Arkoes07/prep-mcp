package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/arkoes07/prep-mcp/internal/read"
	"github.com/arkoes07/prep-mcp/internal/testvault"
	"github.com/arkoes07/prep-mcp/internal/write"
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
	cfg, v := testvault.Open(t, root)
	r := read.New(v, cfg, testvault.NewClock())
	// A nil Git: the server tests do not commit.
	return serve(t, New(r, write.New(v, cfg, nil, testvault.NewClock()), cfg, "test"))
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

func toolDescription(t *testing.T, cs *mcp.ClientSession, name string) string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if tool.Name == name {
			return tool.Description
		}
	}
	t.Fatalf("no %s tool", name)
	return ""
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
	want := []string{
		"append_section",
		"batch",
		"create_doc",
		"edit_text",
		"get_context",
		"list_docs",
		"list_tags",
		"read_doc",
		"replace_doc",
		"search_text",
		"update_frontmatter",
	}
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
	testvault.Write(
		t,
		root,
		"prep.config.yaml",
		"types:\n"+
			"  note:\n    folder: notes\n    filename: \"{slug}.md\"\n    required: [title]\n"+
			"  log:\n    folder: logs\n    filename: \"{date}-{slug}.md\"\n    required: [date]\n",
	)
	return root
}

// Tool descriptions and schemas are shown to the model; they must stay
// domain-neutral like the rest of the Go code.
func TestDescriptionsAreDomainNeutral(t *testing.T) {
	re := testvault.LoadForbiddenWords(t)

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

func TestPanicBecomesToolError(t *testing.T) {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler)) // guard logs the stack; keep test output clean
	t.Cleanup(func() { slog.SetDefault(prev) })
	s := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	mcp.AddTool(
		s,
		&mcp.Tool{Name: "boom", Description: "panics"},
		guard("boom", func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
			panic("kaboom")
		}),
	)
	mcp.AddTool(
		s,
		&mcp.Tool{Name: "fine", Description: "works"},
		guard("fine", func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
			return renderText("still here"), nil, nil
		}),
	)
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

// An assistant asked to "save this" may reach for its own memory or notes
// instead of the vault. The text it reads first must steer it to the write tools.
func TestTextSteersWritesToTheVault(t *testing.T) {
	cs := connect(t)
	init := cs.InitializeResult()
	if init == nil || !strings.Contains(init.Instructions, "memory") {
		t.Errorf("server instructions should say to use the write tools instead of local memory or notes:\n%v", init)
	}
	// A bulk change made with shell commands or file edits skips the checks and the commit author.
	if init == nil || !strings.Contains(init.Instructions, "batch") {
		t.Errorf("server instructions should name batch for changes to several documents:\n%v", init)
	}
	if d := toolDescription(t, cs, "batch"); !strings.Contains(d, "not shell commands or file edits") {
		t.Errorf("batch description should say to use it instead of editing the files directly:\n%s", d)
	}
	// A model that turns [[x]] into x.md gets a root path, not the file the link names.
	if d := toolDescription(t, cs, "read_doc"); !strings.Contains(d, "Do not build a path by adding .md to a name") {
		t.Errorf("read_doc description should say a name plus .md is read as a root path:\n%s", d)
	}
	for _, tool := range []string{"create_doc", "append_section"} {
		res, err := cs.ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, tl := range res.Tools {
			if tl.Name == tool && !strings.Contains(tl.Description, "memory") {
				t.Errorf("%s description should say it replaces local memory or notes:\n%s", tool, tl.Description)
			}
		}
	}
}
