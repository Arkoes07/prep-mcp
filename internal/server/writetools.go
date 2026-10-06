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

// registerWriteTools adds the four write tools and batch. Each write tool is a
// batch of one: buildOp turns its arguments into the op. Their descriptions
// are what the model sees, so create_doc's is generated from the config: the
// model never has to read the config file.
func registerWriteTools(s *mcp.Server, h *handlers) {
	no, yes := false, true
	additive := &mcp.ToolAnnotations{DestructiveHint: &no, OpenWorldHint: &no}

	mcp.AddTool(s, &mcp.Tool{
		Name:        write.ToolCreate,
		Title:       "Create a document",
		Description: createDocDescription(h.cfg),
		Annotations: additive,
	}, writeTool[createDocIn](h, write.ToolCreate))

	mcp.AddTool(s, &mcp.Tool{
		Name:  write.ToolAppend,
		Title: "Append to a section",
		Description: "Add text to a living document without touching the rest of it. " +
			"Use this, not local memory or notes files, to add to a record that belongs in the vault. " +
			"The content goes at the end of the \"## heading\" section (before the next # or ## heading); " +
			"if the file has no such section, a new \"## heading\" section is added at the end. " +
			"Only level-2 headings match, exactly. " +
			"Frontmatter and all existing text stay byte for byte. Returns path, new hash, warnings and commit. " +
			"This is the default way to add to a document.",
		Annotations: additive,
	}, writeTool[appendSectionIn](h, write.ToolAppend))

	mcp.AddTool(s, &mcp.Tool{
		Name:  write.ToolPatch,
		Title: "Update frontmatter",
		Description: "Change frontmatter only; the body stays byte for byte and so do untouched keys. " +
			"set: object of top-level keys to set (new keys are added at the end); unset: keys to remove; " +
			"add_tags / remove_tags: change the tag list. " +
			"updated is set to today. Required fields cannot be removed or emptied and type cannot be unset. " +
			"set.tags cannot be combined with add_tags/remove_tags. " +
			"To clear a field, use unset; set with \"\" keeps the key with an empty value, " +
			"which where filters treat differently from a missing key. " +
			"Adding a tag missing from the vocabulary file fails, naming the closest known tag. " +
			"A patch that changes nothing writes nothing. Returns path, new hash and warnings.",
		Annotations: additive,
	}, writeTool[updateFrontmatterIn](h, write.ToolPatch))

	mcp.AddTool(s, &mcp.Tool{
		Name:  write.ToolReplace,
		Title: "Replace a whole document",
		Description: "Rewrite a whole file. Pass expected_hash from read_doc. " +
			"If the file changed since (someone edited it), " +
			"nothing is written and the error carries the current hash: " +
			"re-read with read_doc, merge the changes, and retry. " +
			"Prefer append_section or update_frontmatter for small changes. " +
			"The new content must keep valid frontmatter and the type's required fields. " +
			"Returns path, new hash and warnings.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, OpenWorldHint: &no},
	}, writeTool[replaceDocIn](h, write.ToolReplace))

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
			"Returns per op the path and the file's hash after the whole batch, its warnings, then the commit and its warnings.",
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
		"a tag missing from the vocabulary file fails the call, naming the closest known tag. " +
		"Returns path, hash, warnings and commit.\n")
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

// writeTool is the handler of a single write tool, run as a batch of one. In
// is the tool's argument type, for its schema; buildOp decodes the arguments,
// as it does for a batch op.
func writeTool[In any](h *handlers, tool string) mcp.ToolHandlerFor[In, any] {
	handler := func(
		ctx context.Context,
		req *mcp.CallToolRequest,
		_ In,
	) (*mcp.CallToolResult, any, error) {
		res, err := h.w.Apply(ctx, buildOp(tool, req.Params.Arguments))
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(res)
	}
	return guard(tool, handler)
}

// rawArg returns the object-valued argument key as the call's raw JSON has
// it, because decoding it into a Go map would lose the order of its keys. It
// is nil when the key is missing or null.
func rawArg(args json.RawMessage, key string) (json.RawMessage, error) {
	var all map[string]json.RawMessage
	if len(args) > 0 {
		if err := json.Unmarshal(args, &all); err != nil {
			return nil, fmt.Errorf("arguments are not a JSON object: %w", err)
		}
	}
	if v := all[key]; string(v) != "null" {
		return v, nil
	}
	return nil, nil
}

// The jsonschema tags are argument descriptions and cannot be split; see listDocsIn.
type createDocIn struct {
	Type        string         `json:"type" jsonschema:"One of the configured types listed in this tool's description."`
	Frontmatter map[string]any `json:"frontmatter" jsonschema:"Frontmatter fields as an object, in the order they should appear. Include every required field of the type."`
	Body        string         `json:"body,omitempty" jsonschema:"Markdown body written after the frontmatter."`
	Slug        string         `json:"slug,omitempty" jsonschema:"Name for {slug} in the filename pattern. Defaults to the title field."`
}

// decodeCreate builds create_doc's op, taking the frontmatter from the raw
// JSON so its keys keep their order.
func decodeCreate(args json.RawMessage) (write.Op, error) {
	var in createDocIn
	if err := decodeArgs(write.ToolCreate, args, &in); err != nil {
		return write.Op{}, err
	}
	raw, err := rawArg(args, "frontmatter")
	if err != nil {
		return write.Op{}, err
	}
	fm, err := write.JSONToNode(raw)
	if err != nil {
		return write.Op{}, fmt.Errorf("frontmatter %w", err)
	}
	return write.CreateOp(write.Draft{Type: in.Type, Frontmatter: fm, Body: in.Body, Slug: in.Slug}), nil
}

type appendSectionIn struct {
	Path    string `json:"path" jsonschema:"Vault-relative path of an existing .md file."`
	Heading string `json:"heading" jsonschema:"Section title without the ##, for example Log."`
	Content string `json:"content" jsonschema:"Markdown to add at the end of that section."`
}

func decodeAppend(args json.RawMessage) (write.Op, error) {
	var in appendSectionIn
	if err := decodeArgs(write.ToolAppend, args, &in); err != nil {
		return write.Op{}, err
	}
	return write.AppendOp(in.Path, in.Heading, in.Content), nil
}

type updateFrontmatterIn struct {
	Path       string         `json:"path" jsonschema:"Vault-relative path of an existing .md file with frontmatter."`
	Set        map[string]any `json:"set,omitempty" jsonschema:"Top-level keys to set, as an object."`
	Unset      []string       `json:"unset,omitempty" jsonschema:"Top-level keys to remove."`
	AddTags    []string       `json:"add_tags,omitempty" jsonschema:"Tags to add. Lowercased; duplicates ignored."`
	RemoveTags []string       `json:"remove_tags,omitempty" jsonschema:"Tags to remove."`
}

// decodePatch builds update_frontmatter's op, taking set from the raw JSON so
// its keys keep their order.
func decodePatch(args json.RawMessage) (write.Op, error) {
	var in updateFrontmatterIn
	if err := decodeArgs(write.ToolPatch, args, &in); err != nil {
		return write.Op{}, err
	}
	patch := write.Patch{Unset: in.Unset, AddTags: in.AddTags, RemoveTags: in.RemoveTags}
	raw, err := rawArg(args, "set")
	if err != nil {
		return write.Op{}, err
	}
	if raw != nil {
		if patch.Set, err = write.JSONToNode(raw); err != nil {
			return write.Op{}, fmt.Errorf("set %w", err)
		}
	}
	return write.PatchOp(in.Path, patch), nil
}

// The jsonschema tags are argument descriptions and cannot be split; see listDocsIn.
type replaceDocIn struct {
	Path         string `json:"path" jsonschema:"Vault-relative path of an existing .md file."`
	Content      string `json:"content" jsonschema:"The complete new file content, frontmatter included."`
	ExpectedHash string `json:"expected_hash" jsonschema:"The hash read_doc returned for the version you based this on."`
}

func decodeReplace(args json.RawMessage) (write.Op, error) {
	var in replaceDocIn
	if err := decodeArgs(write.ToolReplace, args, &in); err != nil {
		return write.Op{}, err
	}
	return write.ReplaceOp(in.Path, in.Content, in.ExpectedHash), nil
}

const batchTools = write.ToolCreate + ", " + write.ToolAppend + ", " + write.ToolPatch + " or " + write.ToolReplace

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
		ops[i] = buildOp(op.Tool, op.Args)
	}
	res, err := h.w.Batch(ctx, ops, in.Message)
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(res)
}

// buildOp builds the op of a write tool from its name and raw arguments, for
// the tool itself and for a batch op alike. Arguments that do not decode give
// a FailedOp, which Apply and Batch report as that op's failure.
func buildOp(tool string, args json.RawMessage) write.Op {
	var (
		op  write.Op
		err error
	)
	switch tool {
	case write.ToolCreate:
		op, err = decodeCreate(args)
	case write.ToolAppend:
		op, err = decodeAppend(args)
	case write.ToolPatch:
		op, err = decodePatch(args)
	case write.ToolReplace:
		op, err = decodeReplace(args)
	default:
		err = fmt.Errorf("unknown tool %q; use %s", tool, batchTools)
	}
	if err != nil {
		return write.FailedOp(tool, err)
	}
	return op
}

// decodeArgs decodes a write tool's arguments into its argument struct,
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
		return fmt.Errorf(
			"args.%s has the wrong type (a %s); pass %s's own arguments as its schema describes",
			typeErr.Field,
			typeErr.Value,
			tool,
		)
	case err != nil:
		return fmt.Errorf("args: %w; pass %s's own arguments", err, tool)
	}
	return nil
}
