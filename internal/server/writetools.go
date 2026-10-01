package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arkoes07/prep-mcp/internal/config"
	"github.com/Arkoes07/prep-mcp/internal/write"
)

// registerWriteTools adds the four write tools. Their descriptions are what
// the model sees, so create_doc's is generated from the config: the model
// never has to read the config file.
func registerWriteTools(s *mcp.Server, h *handlers) {
	no, yes := false, true
	additive := &mcp.ToolAnnotations{DestructiveHint: &no, OpenWorldHint: &no}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_doc",
		Title:       "Create a document",
		Description: createDocDescription(h.cfg),
		Annotations: additive,
	}, guard(h.logger, "create_doc", h.createDoc))

	mcp.AddTool(s, &mcp.Tool{
		Name:  "append_section",
		Title: "Append to a section",
		Description: "Add text to a living document without touching the rest of it. Use this, not local memory or notes files, to add to a record that belongs in the vault. The content goes at the end of the \"## heading\" section (before the next # or ## heading); " +
			"if the file has no such section, a new \"## heading\" section is added at the end. Only level-2 headings match, exactly. " +
			"Frontmatter and all existing text stay byte for byte. Returns path, new hash, warnings and commit. This is the default way to add to a document.",
		Annotations: additive,
	}, guard(h.logger, "append_section", h.appendSection))

	mcp.AddTool(s, &mcp.Tool{
		Name:  "update_frontmatter",
		Title: "Update frontmatter",
		Description: "Change frontmatter only; the body stays byte for byte and so do untouched keys. " +
			"set: object of top-level keys to set (new keys are added at the end); unset: keys to remove; add_tags / remove_tags: change the tag list. " +
			"updated is set to today. Required fields cannot be removed or emptied and type cannot be unset. set.tags cannot be combined with add_tags/remove_tags. " +
			"A patch that changes nothing writes nothing. Returns path, new hash and warnings such as unknown tags.",
		Annotations: additive,
	}, guard(h.logger, "update_frontmatter", h.updateFrontmatter))

	mcp.AddTool(s, &mcp.Tool{
		Name:  "replace_doc",
		Title: "Replace a whole document",
		Description: "Rewrite a whole file. Pass expected_hash from read_doc. If the file changed since (someone edited it), nothing is written and the error carries the current hash: " +
			"re-read with read_doc, merge the changes, and retry. Prefer append_section or update_frontmatter for small changes. " +
			"The new content must keep valid frontmatter and the type's required fields. Returns path, new hash and warnings.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, OpenWorldHint: &no},
	}, guard(h.logger, "replace_doc", h.replaceDoc))
}

// createDocDescription lists every configured type with its path pattern and
// required fields, so the model can call create_doc correctly the first time.
func createDocDescription(cfg *config.Config) string {
	var b strings.Builder
	b.WriteString("Create a new document of a configured type. Use this, not local memory or notes files, to save a new record that belongs in the vault. The path is built from the type's folder and filename pattern. " +
		"Fails if the file already exists (the error names it). Sets created to today if absent. Required fields must be present and not empty. " +
		"tags may be a list or a comma-separated string; unknown tags produce a warning naming the closest known tag. Returns path, hash, warnings and commit.\n")
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
	b.WriteString("In a pattern, {slug} is the slug argument, else the title field; {date} is the date field, else today; any other {name} is the frontmatter field of that name. " +
		"Every value becomes lowercase kebab-case.")
	return b.String()
}

// writeOut is the JSON every write returns.
type writeOut struct {
	Path     string   `json:"path"`
	Hash     string   `json:"hash"`
	Warnings []string `json:"warnings"`
	Commit   string   `json:"commit,omitempty"`
}

func writeResult(res *write.Result, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return nil, nil, err
	}
	warnings := res.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	return jsonResult(writeOut{Path: res.Path, Hash: res.Hash, Warnings: warnings, Commit: res.Commit})
}

// rawArgs pulls object-valued arguments out of the call's raw JSON, because
// decoding them into a Go map would lose the order of their keys.
func rawArgs(req *mcp.CallToolRequest, keys ...string) (map[string]json.RawMessage, error) {
	var all map[string]json.RawMessage
	if len(req.Params.Arguments) > 0 {
		if err := json.Unmarshal(req.Params.Arguments, &all); err != nil {
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
	args, err := rawArgs(req, "frontmatter")
	if err != nil {
		return nil, nil, err
	}
	fm, err := write.JSONToNode(args["frontmatter"])
	if err != nil {
		return nil, nil, fmt.Errorf("frontmatter %w", err)
	}
	return writeResult(h.w.Create(ctx, in.Type, fm, in.Body, in.Slug))
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
	patch := write.Patch{Unset: in.Unset, AddTags: in.AddTags, RemoveTags: in.RemoveTags}
	args, err := rawArgs(req, "set")
	if err != nil {
		return nil, nil, err
	}
	if raw, ok := args["set"]; ok {
		if patch.Set, err = write.JSONToNode(raw); err != nil {
			return nil, nil, fmt.Errorf("set %w", err)
		}
	}
	return writeResult(h.w.UpdateFrontmatter(ctx, in.Path, patch))
}

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
