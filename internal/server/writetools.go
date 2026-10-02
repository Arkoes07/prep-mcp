package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arkoes07/prep-mcp/internal/config"
	"github.com/Arkoes07/prep-mcp/internal/write"
)

// registerWriteTools adds the four write tools and batch. Their descriptions
// are what the model sees, so create_doc's is generated from the config: the
// model never has to read the config file.
func registerWriteTools(s *mcp.Server, h *handlers) {
	no, yes := false, true
	additive := &mcp.ToolAnnotations{DestructiveHint: &no, OpenWorldHint: &no}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_doc",
		Title:       "Create a document",
		Description: createDocDescription(h.cfg),
		Annotations: additive,
	}, guard("create_doc", h.createDoc))

	mcp.AddTool(s, &mcp.Tool{
		Name:  "append_section",
		Title: "Append to a section",
		Description: "Add text to a living document without touching the rest of it. " +
			"Use this, not local memory or notes files, to add to a record that belongs in the vault. " +
			"The content goes at the end of the \"## heading\" section (before the next # or ## heading); " +
			"if the file has no such section, a new \"## heading\" section is added at the end. " +
			"Only level-2 headings match, exactly. " +
			"Frontmatter and all existing text stay byte for byte. Returns path, new hash, warnings and commit. " +
			"This is the default way to add to a document.",
		Annotations: additive,
	}, guard("append_section", h.appendSection))

	mcp.AddTool(s, &mcp.Tool{
		Name:  "update_frontmatter",
		Title: "Update frontmatter",
		Description: "Change frontmatter only; the body stays byte for byte and so do untouched keys. " +
			"set: object of top-level keys to set (new keys are added at the end); unset: keys to remove; " +
			"add_tags / remove_tags: change the tag list. " +
			"updated is set to today. Required fields cannot be removed or emptied and type cannot be unset. " +
			"set.tags cannot be combined with add_tags/remove_tags. " +
			"A patch that changes nothing writes nothing. Returns path, new hash and warnings such as unknown tags.",
		Annotations: additive,
	}, guard("update_frontmatter", h.updateFrontmatter))

	mcp.AddTool(s, &mcp.Tool{
		Name:  "replace_doc",
		Title: "Replace a whole document",
		Description: "Rewrite a whole file. Pass expected_hash from read_doc. " +
			"If the file changed since (someone edited it), " +
			"nothing is written and the error carries the current hash: " +
			"re-read with read_doc, merge the changes, and retry. " +
			"Prefer append_section or update_frontmatter for small changes. " +
			"The new content must keep valid frontmatter and the type's required fields. " +
			"Returns path, new hash and warnings.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, OpenWorldHint: &no},
	}, guard("replace_doc", h.replaceDoc))

	mcp.AddTool(s, &mcp.Tool{
		Name:  "batch",
		Title: "Apply several writes at once",
		Description: "Apply several writes in one call, all or nothing, as one commit. " +
			"Use this, not shell commands or file edits, for imports and for any change to more than one document: " +
			"every write keeps its checks and the commit author. " +
			"ops: a list of {tool, args}, where tool is " + batchTools + " " +
			"and args are exactly that tool's arguments. " +
			"Ops run in order and later ops see what earlier ones did, so a path may appear more than once; " +
			"replace_doc must be the first op on its path. " +
			"Every op is checked before anything is written: if any fails, nothing is written " +
			"and every failure is listed by op number. " +
			fmt.Sprintf("At most %d ops per call. ", write.MaxBatchOps) +
			"message: the commit message (default: the number of ops per tool). " +
			"Returns per op the path, new hash and warnings, then the commit and its warnings.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, OpenWorldHint: &no},
	}, guard("batch", h.batch))
}

// createDocDescription lists every configured type with its path pattern and
// required fields, so the model can call create_doc correctly the first time.
func createDocDescription(cfg *config.Config) string {
	var b strings.Builder
	b.WriteString("Create a new document of a configured type. " +
		"Use this, not local memory or notes files, to save a new record that belongs in the vault. " +
		"The path is built from the type's folder and filename pattern. " +
		"Fails if the file already exists (the error names it). Sets created to today if absent. " +
		"Required fields must be present and not empty. " +
		"tags may be a list or a comma-separated string; " +
		"unknown tags produce a warning naming the closest known tag. Returns path, hash, warnings and commit.\n")
	names := cfg.TypeNames()
	if len(names) == 0 {
		b.WriteString("No types are configured: add a types: section to prep.config.yaml and restart the server.")
		return b.String()
	}
	b.WriteString("Types (path pattern; required frontmatter fields):\n")
	for _, name := range names {
		t := cfg.Types[name]
		required := "none"
		if len(t.Required) > 0 {
			required = strings.Join(t.Required, ", ")
		}
		fmt.Fprintf(&b, "- %s: %s/%s; required: %s\n", name, t.Folder, t.Filename, required)
	}
	b.WriteString("In a pattern, {slug} is the slug argument, else the title field; " +
		"{date} is the type's date field (date unless the config names another), else today; " +
		"any other {name} is the frontmatter field of that name. " +
		"Every value becomes lowercase kebab-case.")
	return b.String()
}

func writeResult(res *write.Result, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return nil, nil, err
	}
	res.Warnings = nonNil(res.Warnings)
	return jsonResult(res)
}

// nonNil makes the client see [], not null.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// rawArgs pulls object-valued arguments out of a call's raw JSON, because
// decoding them into a Go map would lose the order of their keys.
func rawArgs(args json.RawMessage, keys ...string) (map[string]json.RawMessage, error) {
	var all map[string]json.RawMessage
	if len(args) > 0 {
		if err := json.Unmarshal(args, &all); err != nil {
			return nil, fmt.Errorf("arguments are not a JSON object: %w", err)
		}
	}
	out := map[string]json.RawMessage{}
	for _, k := range keys {
		if v, ok := all[k]; ok && string(v) != "null" {
			out[k] = v
		}
	}
	return out, nil
}

// The jsonschema tags are argument descriptions and cannot be split; see listDocsIn.
type createDocIn struct {
	Type        string         `json:"type" jsonschema:"One of the configured types listed in this tool's description."`
	Frontmatter map[string]any `json:"frontmatter" jsonschema:"Frontmatter fields as an object, in the order they should appear. Include every required field of the type."`
	Body        string         `json:"body,omitempty" jsonschema:"Markdown body written after the frontmatter."`
	Slug        string         `json:"slug,omitempty" jsonschema:"Name for {slug} in the filename pattern. Defaults to the title field."`
}

func (h *handlers) createDoc(
	ctx context.Context,
	req *mcp.CallToolRequest,
	in createDocIn,
) (*mcp.CallToolResult, any, error) {
	d, err := draftFrom(in, req.Params.Arguments)
	if err != nil {
		return nil, nil, err
	}
	return writeResult(h.w.Create(ctx, d))
}

// draftFrom turns create_doc's arguments into a draft, taking the frontmatter
// from the raw JSON so its keys keep their order.
func draftFrom(in createDocIn, args json.RawMessage) (write.Draft, error) {
	raw, err := rawArgs(args, "frontmatter")
	if err != nil {
		return write.Draft{}, err
	}
	fm, err := write.JSONToNode(raw["frontmatter"])
	if err != nil {
		return write.Draft{}, fmt.Errorf("frontmatter %w", err)
	}
	return write.Draft{Type: in.Type, Frontmatter: fm, Body: in.Body, Slug: in.Slug}, nil
}

type appendSectionIn struct {
	Path    string `json:"path" jsonschema:"Vault-relative path of an existing .md file."`
	Heading string `json:"heading" jsonschema:"Section title without the ##, for example Log."`
	Content string `json:"content" jsonschema:"Markdown to add at the end of that section."`
}

func (h *handlers) appendSection(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in appendSectionIn,
) (*mcp.CallToolResult, any, error) {
	return writeResult(h.w.AppendSection(ctx, in.Path, in.Heading, in.Content))
}

type updateFrontmatterIn struct {
	Path       string         `json:"path" jsonschema:"Vault-relative path of an existing .md file with frontmatter."`
	Set        map[string]any `json:"set,omitempty" jsonschema:"Top-level keys to set, as an object."`
	Unset      []string       `json:"unset,omitempty" jsonschema:"Top-level keys to remove."`
	AddTags    []string       `json:"add_tags,omitempty" jsonschema:"Tags to add. Lowercased; duplicates ignored."`
	RemoveTags []string       `json:"remove_tags,omitempty" jsonschema:"Tags to remove."`
}

func (h *handlers) updateFrontmatter(
	ctx context.Context,
	req *mcp.CallToolRequest,
	in updateFrontmatterIn,
) (*mcp.CallToolResult, any, error) {
	patch, err := patchFrom(in, req.Params.Arguments)
	if err != nil {
		return nil, nil, err
	}
	return writeResult(h.w.UpdateFrontmatter(ctx, in.Path, patch))
}

// patchFrom turns update_frontmatter's arguments into a patch, taking set
// from the raw JSON so its keys keep their order.
func patchFrom(in updateFrontmatterIn, args json.RawMessage) (write.Patch, error) {
	patch := write.Patch{Unset: in.Unset, AddTags: in.AddTags, RemoveTags: in.RemoveTags}
	raw, err := rawArgs(args, "set")
	if err != nil {
		return write.Patch{}, err
	}
	if set, ok := raw["set"]; ok {
		if patch.Set, err = write.JSONToNode(set); err != nil {
			return write.Patch{}, fmt.Errorf("set %w", err)
		}
	}
	return patch, nil
}

// The jsonschema tags are argument descriptions and cannot be split; see listDocsIn.
type replaceDocIn struct {
	Path         string `json:"path" jsonschema:"Vault-relative path of an existing .md file."`
	Content      string `json:"content" jsonschema:"The complete new file content, frontmatter included."`
	ExpectedHash string `json:"expected_hash" jsonschema:"The hash read_doc returned for the version you based this on."`
}

func (h *handlers) replaceDoc(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in replaceDocIn,
) (*mcp.CallToolResult, any, error) {
	return writeResult(h.w.Replace(ctx, in.Path, in.Content, in.ExpectedHash))
}

// batchTools names the tools a batch op may use.
const batchTools = "create_doc, append_section, update_frontmatter or replace_doc"

// The jsonschema tags are argument descriptions and cannot be split; see listDocsIn.
type batchIn struct {
	Ops     []batchOpIn `json:"ops" jsonschema:"The writes, in the order to apply them."`
	Message string      `json:"message,omitempty" jsonschema:"Commit message. Defaults to the number of ops per tool."`
}

type batchOpIn struct {
	Tool string         `json:"tool" jsonschema:"create_doc, append_section, update_frontmatter or replace_doc."`
	Args map[string]any `json:"args" jsonschema:"That tool's arguments, exactly as the tool itself takes them."`
}

func (h *handlers) batch(
	ctx context.Context,
	req *mcp.CallToolRequest,
	in batchIn,
) (*mcp.CallToolResult, any, error) {
	// The ops are read again from the raw JSON, so frontmatter keeps its key order.
	var raw struct {
		Ops []struct {
			Tool string          `json:"tool"`
			Args json.RawMessage `json:"args"`
		} `json:"ops"`
	}
	if err := json.Unmarshal(req.Params.Arguments, &raw); err != nil {
		return nil, nil, fmt.Errorf("arguments are not a JSON object: %w", err)
	}
	ops := make([]write.Op, len(raw.Ops))
	for i, op := range raw.Ops {
		ops[i] = opFrom(op.Tool, op.Args)
	}
	res, err := h.w.Batch(ctx, ops, in.Message)
	if err != nil {
		return nil, nil, err
	}
	for i := range res.Results {
		res.Results[i].Warnings = nonNil(res.Results[i].Warnings)
	}
	res.Warnings = nonNil(res.Warnings)
	return jsonResult(res)
}

// opFrom builds a batch op from a write tool's name and raw arguments,
// decoded as that tool decodes them. Arguments that do not decode give a
// FailedOp, which the batch reports with the other failures.
func opFrom(tool string, args json.RawMessage) write.Op {
	op, err := buildOp(tool, args)
	if err != nil {
		return write.FailedOp(tool, err)
	}
	return op
}

func buildOp(tool string, args json.RawMessage) (write.Op, error) {
	switch tool {
	case "create_doc":
		var in createDocIn
		if err := decodeArgs(tool, args, &in); err != nil {
			return write.Op{}, err
		}
		d, err := draftFrom(in, args)
		if err != nil {
			return write.Op{}, err
		}
		return write.CreateOp(d), nil
	case "append_section":
		var in appendSectionIn
		if err := decodeArgs(tool, args, &in); err != nil {
			return write.Op{}, err
		}
		return write.AppendOp(in.Path, in.Heading, in.Content), nil
	case "update_frontmatter":
		var in updateFrontmatterIn
		if err := decodeArgs(tool, args, &in); err != nil {
			return write.Op{}, err
		}
		patch, err := patchFrom(in, args)
		if err != nil {
			return write.Op{}, err
		}
		return write.PatchOp(in.Path, patch), nil
	case "replace_doc":
		var in replaceDocIn
		if err := decodeArgs(tool, args, &in); err != nil {
			return write.Op{}, err
		}
		return write.ReplaceOp(in.Path, in.Content, in.ExpectedHash), nil
	}
	return write.Op{}, fmt.Errorf("unknown tool %q; use %s", tool, batchTools)
}

// decodeArgs decodes a batch op's args into the tool's own argument struct,
// rejecting unknown keys so a misspelt argument is not silently dropped.
func decodeArgs(tool string, args json.RawMessage, v any) error {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &typeErr):
		return fmt.Errorf("args.%s has the wrong type (a %s); pass %s's own arguments as its schema describes",
			typeErr.Field, typeErr.Value, tool)
	case err != nil:
		return fmt.Errorf("args: %w; pass %s's own arguments", err, tool)
	}
	return nil
}
