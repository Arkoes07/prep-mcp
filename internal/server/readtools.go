package server

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arkoes07/prep-mcp/internal/read"
)

// registerReadTools adds the five read tools. None of them changes the read.
func registerReadTools(s *mcp.Server, h *handlers) {
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}

	mcp.AddTool(s, &mcp.Tool{
		Name:  "get_context",
		Title: "Get start-of-conversation context",
		Description: "Start-of-conversation bundle in one call: the vault's working rules (CLAUDE.md), " +
			"the current ISO week's document in full, " +
			"metadata for open items and recent documents, and the most used tags of the last 30 days. " +
			"Call this first. " +
			"Optional date (YYYY-MM-DD) builds the bundle as of another day.",
		Annotations: readOnly,
	}, guard(h.logger, "get_context", h.getContext))

	mcp.AddTool(s, &mcp.Tool{
		Name:  "list_docs",
		Title: "List documents",
		Description: "List document metadata (path, type, title, tags, date and any kind/stage/status), " +
			"never bodies. " +
			"All filters combine with AND. tags: every tag must match; any_tags: at least one; " +
			"a trailing / matches a prefix (issue/ matches every issue tag). " +
			"where: frontmatter equality such as {\"status\": [\"open\", \"improving\"]}; a list means any of. " +
			"since/until: inclusive YYYY-MM-DD against the date field, else file modification time. " +
			"sort: date_desc (default), date_asc or title. limit: default 50, at most 500.",
		Annotations: readOnly,
	}, guard(h.logger, "list_docs", h.listDocs))

	mcp.AddTool(s, &mcp.Tool{
		Name:  "read_doc",
		Title: "Read a document",
		Description: "Read one markdown file by vault-relative path. " +
			"Returns its path, its content hash (sha256, needed later to rewrite the file safely) " +
			"and the full content. " +
			"Any .md file in the vault can be read, including templates/.",
		Annotations: readOnly,
	}, guard(h.logger, "read_doc", h.readDoc))

	mcp.AddTool(s, &mcp.Tool{
		Name:  "search_text",
		Title: "Search document text",
		Description: "Case-insensitive substring search over document bodies and titles. " +
			"Returns path, title, the total number of matching lines and up to 3 of them " +
			"with 1-based file line numbers. " +
			"Other frontmatter fields are not searched. Optionally limit to one type; limit defaults to 20.",
		Annotations: readOnly,
	}, guard(h.logger, "search_text", h.searchText))

	mcp.AddTool(s, &mcp.Tool{
		Name:  "list_tags",
		Title: "List tags",
		Description: "Every tag in use with its document count and most recent date, most used first. " +
			"Optionally only tags under a prefix such as issue/. " +
			"Tags missing from the vocabulary file are marked unknown.",
		Annotations: readOnly,
	}, guard(h.logger, "list_tags", h.listTags))
}

// parseDay parses an optional YYYY-MM-DD argument.
func parseDay(name, s string) (*time.Time, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	t, err := read.ParseDate(s)
	if err != nil {
		return nil, fmt.Errorf("%s: use YYYY-MM-DD (got %q)", name, s)
	}
	return &t, nil
}

type getContextIn struct {
	Date string `json:"date,omitempty" jsonschema:"Day to build the bundle for, YYYY-MM-DD. Defaults to today."`
}

func (h *handlers) getContext(
	_ context.Context,
	_ *mcp.CallToolRequest,
	in getContextIn,
) (*mcp.CallToolResult, any, error) {
	day, err := parseDay("date", in.Date)
	if err != nil {
		return nil, nil, err
	}
	if day == nil {
		now := h.r.Now()
		day = &now
	}
	out, err := h.r.Context(*day)
	if err != nil {
		return nil, nil, err
	}
	return text(out), nil, nil
}

// The jsonschema tags are the argument descriptions the model reads, and a
// struct tag cannot be split, so these lines stay long.
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

func (h *handlers) listDocs(
	_ context.Context,
	_ *mcp.CallToolRequest,
	in listDocsIn,
) (*mcp.CallToolResult, any, error) {
	f, err := in.filter()
	if err != nil {
		return nil, nil, err
	}
	docs, problems, err := h.r.List(f)
	if err != nil {
		return nil, nil, err
	}

	out := struct {
		Docs     []docMeta      `json:"docs"`
		Count    int            `json:"count"`
		Note     string         `json:"note,omitempty"`
		Problems []read.Problem `json:"problems,omitempty"`
	}{Docs: make([]docMeta, 0, len(docs)), Count: len(docs), Problems: problems}
	for _, d := range docs {
		out.Docs = append(out.Docs, metaOf(d))
	}
	if in.Type != "" {
		if _, ok := h.cfg.Types[in.Type]; !ok && len(docs) == 0 {
			out.Note = fmt.Sprintf("type %q is not configured; configured types: %s", in.Type, h.cfg.TypeList())
		}
	}
	return jsonResult(out)
}

// filter checks the arguments and turns them into a vault filter.
func (in listDocsIn) filter() (read.Filter, error) {
	f := read.Filter{Type: in.Type, Tags: in.Tags, AnyTags: in.AnyTags, Sort: in.Sort, Limit: in.Limit}
	var err error
	if f.Since, err = parseDay("since", in.Since); err != nil {
		return read.Filter{}, err
	}
	if f.Until, err = parseDay("until", in.Until); err != nil {
		return read.Filter{}, err
	}
	if len(in.Where) > 0 {
		f.Where = make(map[string][]string, len(in.Where))
		for k, val := range in.Where {
			if f.Where[k], err = whereValues(k, val); err != nil {
				return read.Filter{}, err
			}
		}
	}
	return f, nil
}

// metaOf is the list_docs entry for d: its date field as date, or its file
// modification day as modified when it has none.
func metaOf(d *read.Doc) docMeta {
	m := docMeta{
		Path:    d.Path,
		Type:    d.Type,
		Title:   d.Title,
		Tags:    append([]string{}, d.Tags...),
		Updated: d.Text("updated"),
		Kind:    d.Text("kind"),
		Stage:   d.Text("stage"),
		Status:  d.Text("status"),
	}
	if d.DateFromMtime {
		m.Modified = d.Date.Format(time.DateOnly)
	} else {
		m.Date = d.Date.Format(time.DateOnly)
	}
	return m
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
	p, content, hash, err := h.r.ReadDoc(in.Path)
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

func (h *handlers) searchText(
	_ context.Context,
	_ *mcp.CallToolRequest,
	in searchTextIn,
) (*mcp.CallToolResult, any, error) {
	hits, err := h.r.Search(in.Query, in.Type, in.Limit)
	if err != nil {
		return nil, nil, err
	}
	out := struct {
		Hits  []read.Hit `json:"hits"`
		Count int        `json:"count"`
	}{Hits: hits, Count: len(hits)}
	return jsonResult(out)
}

type listTagsIn struct {
	Prefix string `json:"prefix,omitempty" jsonschema:"Only tags starting with this, for example issue/."`
}

func (h *handlers) listTags(
	_ context.Context,
	_ *mcp.CallToolRequest,
	in listTagsIn,
) (*mcp.CallToolResult, any, error) {
	stats, err := h.r.TagStats(in.Prefix, nil)
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
	}{Tags: make([]tagOut, 0, len(stats)), Count: len(stats)}
	for _, s := range stats {
		out.Tags = append(out.Tags, tagOut{
			Tag:     s.Tag,
			Count:   s.Count,
			Last:    s.Last.Format(time.DateOnly),
			Unknown: s.Unknown,
		})
	}
	return jsonResult(out)
}
