package server

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arkoes07/prep-mcp/internal/testvault"
)

func TestEachReadToolAnswers(t *testing.T) {
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
	for _, key := range []string{"path", "type", "title", "tags", "date", "date_source", "fields"} {
		if _, ok := got.Docs[0][key]; !ok {
			t.Errorf("doc lacks %q: %v", key, got.Docs[0])
		}
	}
	// The example config's summary fields, as far as the document sets them.
	if fields, _ := got.Docs[0]["fields"].(map[string]any); len(fields) != 1 || fields["kind"] != "deep-dive" {
		t.Errorf("fields = %v, want only kind: deep-dive", got.Docs[0]["fields"])
	}
	for _, key := range []string{"body", "content"} {
		if _, ok := got.Docs[0][key]; ok {
			t.Errorf("doc has %q; list_docs must return metadata only", key)
		}
	}
}

// Every entry has a date and says where it came from: the type's date field, or
// the file's modification time. There is no separate modified key.
func TestListDocsDateAndSource(t *testing.T) {
	cs := connect(t)
	tests := []struct {
		name   string
		args   map[string]any
		want   map[string]string // path -> date
		source string
	}{
		{
			name:   "date_field",
			args:   map[string]any{"tags": []string{"issue/"}},
			source: "field",
		},
		{
			name: "mtime",
			args: map[string]any{"where": map[string]any{"status": []string{"open", "improving"}}},
			want: map[string]string{
				"improvements/buries-headline.md":  "2026-01-10",
				"improvements/missing-tradeoff.md": "2026-01-10",
			},
			source: "mtime",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, isErr := call(t, cs, "list_docs", tc.args)
			if isErr {
				t.Fatal(out)
			}
			if strings.Contains(out, `"modified"`) {
				t.Errorf("result has a modified key:\n%s", out)
			}
			var got struct {
				Docs []struct {
					Path       string `json:"path"`
					Date       string `json:"date"`
					DateSource string `json:"date_source"`
				} `json:"docs"`
			}
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("result is not JSON: %v\n%s", err, out)
			}
			if len(got.Docs) == 0 {
				t.Fatalf("no docs:\n%s", out)
			}
			for _, d := range got.Docs {
				if d.DateSource != tc.source {
					t.Errorf(
						"%s: date_source = %q, want %q",
						d.Path,
						d.DateSource,
						tc.source,
					)
				}
				if _, err := time.Parse(time.DateOnly, d.Date); err != nil {
					t.Errorf("%s: date = %q, want YYYY-MM-DD", d.Path, d.Date)
				}
				if want, ok := tc.want[d.Path]; ok && d.Date != want {
					t.Errorf(
						"%s: date = %q, want %q",
						d.Path,
						d.Date,
						want,
					)
				}
			}
		})
	}
}

// Failures are tool results with a next step, not protocol errors.
func TestReadErrorsAreToolResultsWithHints(t *testing.T) {
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

// list_docs's description names the fields shown under fields: the summary
// fields by default, then each type with its own show list. It also explains
// the fields argument.
func TestListDocsDescriptionNamesShownFields(t *testing.T) {
	description := func(t *testing.T, cs *mcp.ClientSession) string {
		t.Helper()
		res, err := cs.ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, tool := range res.Tools {
			if tool.Name == "list_docs" {
				return tool.Description
			}
		}
		t.Fatal("no list_docs tool")
		return ""
	}

	root := t.TempDir()
	testvault.Write(
		t,
		root,
		"prep.config.yaml",
		"summary_fields: [status]\ntypes:\n"+
			"  note: {folder: notes, filename: \"{slug}.md\", show: [priority, owner]}\n"+
			"  log: {folder: logs, filename: \"{slug}.md\"}\n"+
			"  memo: {folder: memos, filename: \"{slug}.md\", show: []}\n",
	)
	tests := []struct {
		name          string
		root          string
		want, notWant []string
	}{
		{
			name:    "show_lists",
			root:    root,
			want:    []string{"by default status; for memo none; for note priority, owner)", "fields: field names"},
			notWant: []string{"for log"},
		},
		{
			name: "nothing_configured",
			root: neutralVault(t),
			want: []string{"by default none)", "fields: field names"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := description(t, connectAt(t, tc.root))
			for _, s := range tc.want {
				if !strings.Contains(got, s) {
					t.Errorf("description lacks %q:\n%s", s, got)
				}
			}
			for _, s := range tc.notWant {
				if strings.Contains(got, s) {
					t.Errorf("description has %q:\n%s", s, got)
				}
			}
		})
	}
}

// fields comes from each document's type (its show list, else
// summary_fields); the fields argument replaces it for every document.
func TestListDocsFields(t *testing.T) {
	cs := connect(t)
	tests := []struct {
		name     string
		args     map[string]any
		wantKeys []string          // the keys of the fields object of the one document returned
		wantVals map[string]string // some of its values
	}{
		{
			name:     "type_show_list",
			args:     map[string]any{"tags": []string{"company/"}},
			wantKeys: []string{"next_action", "next_action_date", "stage", "status"},
			wantVals: map[string]string{"next_action": "Send thank-you note", "next_action_date": "2026-01-16"},
		},
		{
			name:     "summary_fields",
			args:     map[string]any{"where": map[string]any{"status": "open"}},
			wantKeys: []string{"status"},
		},
		{
			name:     "argument_overrides",
			args:     map[string]any{"tags": []string{"company/"}, "fields": []string{"next_action"}},
			wantKeys: []string{"next_action"},
		},
		{
			name: "argument_overrides_every_type",
			args: map[string]any{
				"where":  map[string]any{"status": "improving"},
				"fields": []string{"exit_criteria"},
			},
			wantKeys: []string{"exit_criteria"},
			wantVals: map[string]string{"exit_criteria": "Cost stated in 5 project answers in a row"},
		},
		{
			name:     "empty_argument_shows_none",
			args:     map[string]any{"tags": []string{"company/"}, "fields": []string{}},
			wantKeys: []string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, isErr := call(t, cs, "list_docs", tc.args)
			if isErr {
				t.Fatal(out)
			}
			var got struct {
				Docs []struct {
					Fields map[string]string `json:"fields"`
				} `json:"docs"`
			}
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("result is not JSON: %v\n%s", err, out)
			}
			if len(got.Docs) != 1 {
				t.Fatalf("docs = %d, want 1:\n%s", len(got.Docs), out)
			}
			fields := got.Docs[0].Fields
			if keys := slices.Sorted(maps.Keys(fields)); !slices.Equal(keys, tc.wantKeys) {
				t.Errorf("fields keys = %q, want %q", keys, tc.wantKeys)
			}
			for k, want := range tc.wantVals {
				if fields[k] != want {
					t.Errorf("fields[%s] = %q, want %q", k, fields[k], want)
				}
			}
		})
	}
}
