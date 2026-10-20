package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/arkoes07/prep-mcp/internal/config"
	"github.com/arkoes07/prep-mcp/internal/read"
	"github.com/arkoes07/prep-mcp/internal/vault"
	"github.com/arkoes07/prep-mcp/internal/write"
)

func registerReadTools(s *mcp.Server, h *handlers) {
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}

	mcp.AddTool(s, &mcp.Tool{
		Name:  "get_context",
		Title: "Get start-of-conversation context",
		Description: "Start-of-conversation bundle in one call: the vault's working rules (CLAUDE.md), " +
			"the current ISO week's document in full, " +
			"metadata for open items and recent documents (with the fields list_docs shows, " +
			"a list field's values joined by \", \"), and the most used tags of the last 30 days. " +
			"Call this first. " +
			"Optional date (YYYY-MM-DD) builds the bundle as of another day.",
		Annotations: readOnly,
	}, guard("get_context", h.getContext))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_docs",
		Title:       "List documents",
		Description: listDocsDescription(h.cfg),
		Annotations: readOnly,
	}, guard("list_docs", h.listDocs))

	mcp.AddTool(s, &mcp.Tool{
		Name:  "read_doc",
		Title: "Read a document",
		Description: "Read one markdown file. Pass either a path exactly as another tool returned it " +
			"(for example notes/x.md), or a [[wikilink]] exactly as written in a file " +
			"([[x]], [[notes/x]], [[x|alias]]); brackets are optional. " +
			"Do not build a path by adding .md to a name: anything ending in .md is read as a path " +
			"from the vault root, so x.md only finds a file at the root. " +
			"A name that matches several files fails with their paths; pass one of them. " +
			"Returns its real path, its content hash (sha256, needed later to rewrite the file safely) " +
			"and the full content. " +
			"Any .md file in the vault can be read, including templates/.",
		Annotations: readOnly,
	}, guard("read_doc", h.readDoc))

	mcp.AddTool(s, &mcp.Tool{
		Name:  "search_text",
		Title: "Search document text",
		Description: "Case-insensitive substring search over document bodies and titles. " +
			"Returns path, title, the total number of matching lines and up to 3 of them " +
			"with 1-based file line numbers. " +
			"Other frontmatter fields are not searched. Optionally limit to one type; limit defaults to 20.",
		Annotations: readOnly,
	}, guard("search_text", h.searchText))

	mcp.AddTool(s, &mcp.Tool{
		Name:  "list_tags",
		Title: "List tags",
		Description: "Every tag in use with its document count and most recent date, most used first, " +
			"then the vocabulary file's unused tags with count 0 and no date, so you can see which tags are valid. " +
			"Optionally only tags under a prefix such as issue/. " +
			"Tags missing from the vocabulary file are marked unknown.",
		Annotations: readOnly,
	}, guard("list_tags", h.listTags))
}

// listDocsDescription names the fields each type shows under fields, so the
// model knows what fields holds without reading the config.
func listDocsDescription(cfg *config.Config) string {
	shown := []string{"by default " + listOrNone(cfg.SummaryFields)}
	for _, name := range cfg.TypeNames() {
		if show := cfg.Types[name].Show; show != nil {
			shown = append(shown, "for "+name+" "+listOrNone(show))
		}
	}
	return "List document metadata (path, type, title, tags, date, date_source (field or mtime), updated, " +
		"and under fields the configured fields each document sets: " + strings.Join(shown, "; ") + "), " +
		"never bodies. A list field shows its values joined by \", \". " +
		"fields: field names to show instead, for this call; [] shows none. " +
		"All filters combine with AND. tags: every tag must match; any_tags: at least one; " +
		"a trailing / matches a prefix (issue/ matches every issue tag). " +
		"where: frontmatter equality such as {\"status\": [\"open\", \"improving\"]}; a list means any of, " +
		"and a list field matches if any item does. Values compare as text, trimmed: " +
		"a number matches as written (1.0 does not match 1), and tags compare normalised " +
		"(lowercase; a comma-separated string is a list). " +
		"\"\" or null matches an empty or null value but not a missing key. " +
		"date and since/until use each type's date field (date unless the config names another), " +
		"else the file modification time, which date_source then says. " +
		"since/until: inclusive YYYY-MM-DD. " +
		"sort: date_desc (default), date_asc or title. limit: default 50, at most 500. " +
		"group_by: field names; instead of documents, returns one row per distinct combination of their values " +
		"with its count, most first, after the filters (sort and limit do not apply). " +
		"A missing or empty value counts as (none); a list field such as tags counts a document " +
		"once under each of its values, so counts can add up to more than the documents."
}

func listOrNone(fields []string) string {
	if len(fields) == 0 {
		return "none"
	}
	return strings.Join(fields, ", ")
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
// struct tag cannot be split, so these lines stay long. Where only gives the
// schema: filter reads where from the raw arguments.
type listDocsIn struct {
	Type    string         `json:"type,omitempty" jsonschema:"Only documents of this type."`
	Tags    []string       `json:"tags,omitempty" jsonschema:"All of these tags must match. A trailing / is a prefix match."`
	AnyTags []string       `json:"any_tags,omitempty" jsonschema:"At least one of these tags must match. A trailing / is a prefix match."`
	Where   map[string]any `json:"where,omitempty" jsonschema:"Frontmatter equality, for example {\"status\": \"open\"}. A list value means any of."`
	Since   string         `json:"since,omitempty" jsonschema:"Inclusive lower date bound, YYYY-MM-DD."`
	Until   string         `json:"until,omitempty" jsonschema:"Inclusive upper date bound, YYYY-MM-DD."`
	Sort    string         `json:"sort,omitempty" jsonschema:"date_desc (default), date_asc or title."`
	Limit   int            `json:"limit,omitempty" jsonschema:"Maximum results. Default 50, at most 500."`
	Fields  []string       `json:"fields,omitempty" jsonschema:"Field names to show under fields for every document, instead of each type's configured ones. [] shows none."`
	GroupBy []string       `json:"group_by,omitempty" jsonschema:"Field names to count documents by, instead of listing them, for example [\"type\", \"status\"]."`
}

type docMeta struct {
	Path       string            `json:"path"`
	Type       string            `json:"type"`
	Title      string            `json:"title"`
	Tags       []string          `json:"tags"`
	Date       string            `json:"date"`
	DateSource string            `json:"date_source"` // field (the type's date field) or mtime
	Updated    string            `json:"updated,omitempty"`
	Fields     map[string]string `json:"fields,omitempty"` // the shown fields the document sets
}

func (h *handlers) listDocs(
	_ context.Context,
	req *mcp.CallToolRequest,
	in listDocsIn,
) (*mcp.CallToolResult, any, error) {
	f, err := in.filter(req.Params.Arguments)
	if err != nil {
		return nil, nil, err
	}
	if len(in.GroupBy) > 0 {
		return h.groupDocs(f, in.GroupBy)
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
		fields := in.Fields
		if fields == nil {
			fields = h.cfg.ShowFields(d.Type)
		}
		out.Docs = append(out.Docs, metaOf(d, fields))
	}
	out.Note = h.typeNote(in.Type, len(docs) == 0)
	return jsonResult(out)
}

// typeNote explains an empty result for a type that is not configured, and is
// empty otherwise.
func (h *handlers) typeNote(typ string, empty bool) string {
	_, known := h.cfg.Types[typ]
	unknownAndEmpty := typ != "" && !known && empty
	if !unknownAndEmpty {
		return ""
	}
	return fmt.Sprintf("type %q is not configured; configured types: %s", typ, h.cfg.TypeList())
}

// groupDocs is list_docs with group_by: counts per combination of values
// instead of documents.
func (h *handlers) groupDocs(f read.Filter, fields []string) (*mcp.CallToolResult, any, error) {
	groups, problems, err := h.r.Group(f, fields)
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(struct {
		GroupBy  []string       `json:"group_by"`
		Groups   []read.Group   `json:"groups"`
		Note     string         `json:"note,omitempty"`
		Problems []read.Problem `json:"problems,omitempty"`
	}{GroupBy: fields, Groups: groups, Note: h.typeNote(f.Type, len(groups) == 0), Problems: problems})
}

// filter builds the Filter, taking where from args, the call's raw JSON:
// in.Where has been through float64, which turns 1.0 into 1.
func (in listDocsIn) filter(args json.RawMessage) (read.Filter, error) {
	f := read.Filter{Type: in.Type, Tags: in.Tags, AnyTags: in.AnyTags, Sort: in.Sort, Limit: in.Limit}
	var err error
	if f.Since, err = parseDay("since", in.Since); err != nil {
		return read.Filter{}, err
	}
	if f.Until, err = parseDay("until", in.Until); err != nil {
		return read.Filter{}, err
	}
	if f.Where, err = whereOf(args); err != nil {
		return read.Filter{}, err
	}
	return f, nil
}

// whereOf reads the where argument as frontmatter, so its values are read the
// way the documents' fields are. It is empty when the call has no where.
func whereOf(args json.RawMessage) (map[string][]string, error) {
	raw, err := rawArg(args, "where")
	if err != nil {
		return nil, err
	}
	where := map[string][]string{}
	if raw == nil {
		return where, nil
	}
	m, err := write.JSONToNode(raw)
	if err != nil {
		return nil, fmt.Errorf("where: %w", err)
	}
	for k, val := range vault.Pairs(m) {
		if !vault.IsScalarOrList(val) {
			return nil, fmt.Errorf("where.%s: use a string, number, boolean or a list of them", k.Value)
		}
		where[k.Value] = vault.Values(m, k.Value)
	}
	return where, nil
}

// metaOf is the list_docs entry for d: its date with where it came from, and
// each of fields it sets.
func metaOf(d *read.Doc, fields []string) docMeta {
	m := docMeta{
		Path:       d.Path,
		Type:       d.Type,
		Title:      d.Title,
		Tags:       append([]string{}, d.Tags...),
		Date:       d.Date.Format(time.DateOnly),
		DateSource: d.DateSource(),
		Updated:    vault.Text(d.Frontmatter, "updated"),
		Fields:     map[string]string{},
	}
	for _, k := range fields {
		if v := d.Shown(k); v != "" {
			m.Fields[k] = v
		}
	}
	return m
}

type readDocIn struct {
	Path string `json:"path" jsonschema:"A path as a tool returned it (notes/x.md) or a link as written ([[notes/x]])."`
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
	stats, err := h.r.TagStats(in.Prefix)
	if err != nil {
		return nil, nil, err
	}
	type tagOut struct {
		Tag     string `json:"tag"`
		Count   int    `json:"count"`
		Last    string `json:"last,omitempty"`
		Unknown bool   `json:"unknown,omitempty"`
	}
	out := struct {
		Tags  []tagOut `json:"tags"`
		Count int      `json:"count"`
	}{Tags: make([]tagOut, 0, len(stats)), Count: len(stats)}
	for _, s := range stats {
		t := tagOut{Tag: s.Tag, Count: s.Count, Unknown: s.Unknown}
		if !s.Last.IsZero() {
			t.Last = s.Last.Format(time.DateOnly)
		}
		out.Tags = append(out.Tags, t)
	}
	return jsonResult(out)
}
