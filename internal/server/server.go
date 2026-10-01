// Package server registers the MCP tools and turns vault results into text.
// It holds glue only; the logic lives in internal/vault.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arkoes07/prep-mcp/internal/config"
	"github.com/Arkoes07/prep-mcp/internal/vault"
	"github.com/Arkoes07/prep-mcp/internal/write"
)

const instructions = "Call get_context first: it returns the vault's working rules, the current week, open items and recent activity in one call. " +
	"Use list_docs for metadata queries, search_text for text, read_doc for a whole file and list_tags for the tag vocabulary in use. " +
	"To save anything that belongs in this vault (a new record, a note added to a document, a status change), use create_doc, append_section, update_frontmatter or replace_doc. " +
	"Do not save it to local memory or notes files instead: the vault is the record."

// New builds the server with the read and write tools registered.
func New(v *vault.Vault, w *write.Writer, cfg *config.Config, version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "prep-mcp", Version: version}, &mcp.ServerOptions{Instructions: instructions})
	h := &handlers{v: v, w: w, cfg: cfg}
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}

	mcp.AddTool(s, &mcp.Tool{
		Name:  "get_context",
		Title: "Get start-of-conversation context",
		Description: "Start-of-conversation bundle in one call: the vault's working rules (CLAUDE.md), the current ISO week's document in full, " +
			"metadata for open items and recent documents, and the most used tags of the last 30 days. Call this first. " +
			"Optional date (YYYY-MM-DD) builds the bundle as of another day.",
		Annotations: readOnly,
	}, guard("get_context", h.getContext))

	mcp.AddTool(s, &mcp.Tool{
		Name:  "list_docs",
		Title: "List documents",
		Description: "List document metadata (path, type, title, tags, date and any kind/stage/status), never bodies. " +
			"All filters combine with AND. tags: every tag must match; any_tags: at least one; a trailing / matches a prefix (issue/ matches every issue tag). " +
			"where: frontmatter equality such as {\"status\": [\"open\", \"improving\"]}; a list means any of. " +
			"since/until: inclusive YYYY-MM-DD against the date field, else file modification time. " +
			"sort: date_desc (default), date_asc or title. limit: default 50, at most 500.",
		Annotations: readOnly,
	}, guard("list_docs", h.listDocs))

	mcp.AddTool(s, &mcp.Tool{
		Name:  "read_doc",
		Title: "Read a document",
		Description: "Read one markdown file by vault-relative path. Returns its path, its content hash (sha256, needed later to rewrite the file safely) and the full content. " +
			"Any .md file in the vault can be read, including templates/.",
		Annotations: readOnly,
	}, guard("read_doc", h.readDoc))

	mcp.AddTool(s, &mcp.Tool{
		Name:  "search_text",
		Title: "Search document text",
		Description: "Case-insensitive substring search over document bodies and titles. Returns path, title, the total number of matching lines and up to 3 of them with 1-based file line numbers. " +
			"Other frontmatter fields are not searched. Optionally limit to one type; limit defaults to 20.",
		Annotations: readOnly,
	}, guard("search_text", h.searchText))

	mcp.AddTool(s, &mcp.Tool{
		Name:  "list_tags",
		Title: "List tags",
		Description: "Every tag in use with its document count and most recent date, most used first. " +
			"Optionally only tags under a prefix such as issue/. Tags missing from the vocabulary file are marked unknown.",
		Annotations: readOnly,
	}, guard("list_tags", h.listTags))

	registerWriteTools(s, h)
	return s
}

// guard turns a panic in a handler into an error result, so one bad call
// cannot take the server down. The stack goes to stderr, never to the client.
func guard[In any](name string, h mcp.ToolHandlerFor[In, any]) mcp.ToolHandlerFor[In, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (res *mcp.CallToolResult, out any, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("panic in %s: %v\n%s", name, r, debug.Stack())
				res, out, err = nil, nil, fmt.Errorf("internal error in %s: %v. This is a bug in prep-mcp; report it and check the server's stderr for the stack", name, r)
			}
		}()
		return h(ctx, req, in)
	}
}

type handlers struct {
	v   *vault.Vault
	w   *write.Writer
	cfg *config.Config
}

func text(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

func jsonResult(v any) (*mcp.CallToolResult, any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, nil, fmt.Errorf("encode result: %w", err)
	}
	return text(string(b)), nil, nil
}

// parseDay parses an optional YYYY-MM-DD argument.
func parseDay(name, s string) (*time.Time, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	t, err := vault.ParseDate(s)
	if err != nil {
		return nil, fmt.Errorf("%s: use YYYY-MM-DD (got %q)", name, s)
	}
	return &t, nil
}

type getContextIn struct {
	Date string `json:"date,omitempty" jsonschema:"Day to build the bundle for, YYYY-MM-DD. Defaults to today."`
}

func (h *handlers) getContext(_ context.Context, _ *mcp.CallToolRequest, in getContextIn) (*mcp.CallToolResult, any, error) {
	day, err := parseDay("date", in.Date)
	if err != nil {
		return nil, nil, err
	}
	if day == nil {
		now := h.v.Now()
		day = &now
	}
	out, err := h.v.Context(*day)
	if err != nil {
		return nil, nil, err
	}
	return text(out), nil, nil
}

type listDocsIn struct {
	Type    string         `json:"type,omitempty" jsonschema:"Only documents of this type."`
	Tags    []string       `json:"tags,omitempty" jsonschema:"All of these tags must match. A trailing / is a prefix match."`
	AnyTags []string       `json:"any_tags,omitempty" jsonschema:"At least one of these tags must match. A trailing / is a prefix match."`
	Where   map[string]any `json:"where,omitempty" jsonschema:"Frontmatter equality, for example {\"status\": \"open\"}. A list value means any of."`
	Since   string         `json:"since,omitempty" jsonschema:"Inclusive lower date bound, YYYY-MM-DD."`
	Until   string         `json:"until,omitempty" jsonschema:"Inclusive upper date bound, YYYY-MM-DD."`
	Sort    string         `json:"sort,omitempty" jsonschema:"date_desc (default), date_asc or title."`
	Limit   int            `json:"limit,omitempty" jsonschema:"Maximum results. Default 50, at most 500."`
}

type docMeta struct {
	Path     string   `json:"path"`
	Type     string   `json:"type"`
	Title    string   `json:"title"`
	Tags     []string `json:"tags"`
	Date     string   `json:"date,omitempty"`     // the date field
	Modified string   `json:"modified,omitempty"` // file modification day, when there is no date field
	Updated  string   `json:"updated,omitempty"`
	Kind     string   `json:"kind,omitempty"`
	Stage    string   `json:"stage,omitempty"`
	Status   string   `json:"status,omitempty"`
}

type problemOut struct {
	Path string `json:"path"`
	Msg  string `json:"msg"`
}

func problemsOut(ps []vault.Problem) []problemOut {
	var out []problemOut
	for _, p := range ps {
		out = append(out, problemOut{p.Path, p.Msg})
	}
	return out
}

func (h *handlers) listDocs(_ context.Context, _ *mcp.CallToolRequest, in listDocsIn) (*mcp.CallToolResult, any, error) {
	f := vault.Filter{Type: in.Type, Tags: in.Tags, AnyTags: in.AnyTags, Sort: in.Sort, Limit: in.Limit}
	var err error
	if f.Since, err = parseDay("since", in.Since); err != nil {
		return nil, nil, err
	}
	if f.Until, err = parseDay("until", in.Until); err != nil {
		return nil, nil, err
	}
	if len(in.Where) > 0 {
		f.Where = make(map[string][]string, len(in.Where))
		for k, val := range in.Where {
			if f.Where[k], err = whereValues(k, val); err != nil {
				return nil, nil, err
			}
		}
	}
	docs, problems, err := h.v.List(f)
	if err != nil {
		return nil, nil, err
	}

	out := struct {
		Docs     []docMeta    `json:"docs"`
		Count    int          `json:"count"`
		Note     string       `json:"note,omitempty"`
		Problems []problemOut `json:"problems,omitempty"`
	}{Docs: []docMeta{}, Count: len(docs), Problems: problemsOut(problems)}
	for _, d := range docs {
		m := docMeta{Path: d.Path, Type: d.Type, Title: d.Title, Tags: append([]string{}, d.Tags...), Updated: d.Text("updated"),
			Kind: d.Text("kind"), Stage: d.Text("stage"), Status: d.Text("status")}
		if d.DateFromMtime {
			m.Modified = d.Date.Format("2006-01-02")
		} else {
			m.Date = d.Date.Format("2006-01-02")
		}
		out.Docs = append(out.Docs, m)
	}
	if in.Type != "" {
		if _, ok := h.cfg.Type(in.Type); !ok && len(docs) == 0 {
			out.Note = fmt.Sprintf("type %q is not configured; configured types: %s", in.Type, typeNames(h.cfg))
		}
	}
	return jsonResult(out)
}

func typeNames(cfg *config.Config) string {
	if names := cfg.TypeNames(); len(names) > 0 {
		return strings.Join(names, ", ")
	}
	return "none"
}

// whereValues turns a JSON scalar or list of scalars into the texts to compare.
func whereValues(key string, val any) ([]string, error) {
	one := func(x any) (string, bool) {
		switch t := x.(type) {
		case string:
			return t, true
		case bool:
			return strconv.FormatBool(t), true
		case float64:
			return strconv.FormatFloat(t, 'f', -1, 64), true
		}
		return "", false
	}
	if s, ok := one(val); ok {
		return []string{s}, nil
	}
	if list, ok := val.([]any); ok {
		out := make([]string, 0, len(list))
		for _, x := range list {
			s, ok := one(x)
			if !ok {
				return nil, fmt.Errorf("where.%s: list items must be strings, numbers or booleans", key)
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, fmt.Errorf("where.%s: use a string, number, boolean or a list of them", key)
}

type readDocIn struct {
	Path string `json:"path" jsonschema:"Vault-relative path of a .md file, for example notes/x.md."`
}

func (h *handlers) readDoc(_ context.Context, _ *mcp.CallToolRequest, in readDocIn) (*mcp.CallToolResult, any, error) {
	p, content, hash, err := h.v.Read(in.Path)
	if err != nil {
		return nil, nil, err
	}
	return text(fmt.Sprintf("path: %s\nhash: %s\n\n%s", p, hash, content)), nil, nil
}

type searchTextIn struct {
	Query string `json:"query" jsonschema:"Text to find, case-insensitive."`
	Type  string `json:"type,omitempty" jsonschema:"Only documents of this type."`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum documents. Default 20."`
}

func (h *handlers) searchText(_ context.Context, _ *mcp.CallToolRequest, in searchTextIn) (*mcp.CallToolResult, any, error) {
	hits, err := h.v.Search(in.Query, in.Type, in.Limit)
	if err != nil {
		return nil, nil, err
	}
	type lineOut struct {
		N    int    `json:"n"`
		Text string `json:"text"`
	}
	type hitOut struct {
		Path  string    `json:"path"`
		Title string    `json:"title"`
		Total int       `json:"total"`
		Lines []lineOut `json:"lines"`
	}
	out := struct {
		Hits  []hitOut `json:"hits"`
		Count int      `json:"count"`
	}{Hits: []hitOut{}, Count: len(hits)}
	for _, hit := range hits {
		ho := hitOut{Path: hit.Path, Title: hit.Title, Total: hit.Total, Lines: []lineOut{}}
		for _, l := range hit.Lines {
			ho.Lines = append(ho.Lines, lineOut{l.N, l.Text})
		}
		out.Hits = append(out.Hits, ho)
	}
	return jsonResult(out)
}

type listTagsIn struct {
	Prefix string `json:"prefix,omitempty" jsonschema:"Only tags starting with this, for example issue/."`
}

func (h *handlers) listTags(_ context.Context, _ *mcp.CallToolRequest, in listTagsIn) (*mcp.CallToolResult, any, error) {
	stats, err := h.v.TagStats(in.Prefix, nil)
	if err != nil {
		return nil, nil, err
	}
	type tagOut struct {
		Tag     string `json:"tag"`
		Count   int    `json:"count"`
		Last    string `json:"last"`
		Unknown bool   `json:"unknown,omitempty"`
	}
	out := struct {
		Tags  []tagOut `json:"tags"`
		Count int      `json:"count"`
	}{Tags: []tagOut{}, Count: len(stats)}
	for _, s := range stats {
		out.Tags = append(out.Tags, tagOut{s.Tag, s.Count, s.Last.Format("2006-01-02"), s.Unknown})
	}
	return jsonResult(out)
}
