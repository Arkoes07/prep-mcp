package server

import (
	"encoding/json"
	"strings"
	"testing"
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
