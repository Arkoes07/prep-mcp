// Package server registers the MCP tools and turns read and write results
// into text. It holds glue only; the logic lives in internal/read and
// internal/write.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arkoes07/prep-mcp/internal/config"
	"github.com/Arkoes07/prep-mcp/internal/read"
	"github.com/Arkoes07/prep-mcp/internal/write"
)

const instructions = "Call get_context first: it returns the vault's working rules, the current week, " +
	"open items and recent activity in one call. " +
	"Use list_docs for metadata queries, search_text for text, read_doc for a whole file " +
	"and list_tags for the tag vocabulary in use. " +
	"To save anything that belongs in this vault (a new record, a note added to a document, a status change), " +
	"use create_doc, append_section, update_frontmatter, edit_text or replace_doc, " +
	"and batch to change several documents at once, never shell commands or file edits. " +
	"Do not save it to local memory or notes files instead: the vault is the record."

// New builds the server with the read and write tools registered. What the
// client must not see, such as the stack of a handler panic, goes to
// slog.Default(), which must not write to stdout: stdout carries the protocol.
func New(r *read.Reader, w *write.Writer, cfg *config.Config, version string) *mcp.Server {
	s := mcp.NewServer(
		&mcp.Implementation{Name: "prep-mcp", Version: version},
		&mcp.ServerOptions{Instructions: instructions},
	)
	h := &handlers{r: r, w: w, cfg: cfg}
	registerReadTools(s, h)
	registerWriteTools(s, h)
	return s
}

// guard turns a panic in a handler into an error result, so one bad call
// cannot take the server down. The stack goes to slog.Default(), never to the client.
func guard[In any](name string, h mcp.ToolHandlerFor[In, any]) mcp.ToolHandlerFor[In, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (res *mcp.CallToolResult, out any, err error) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("tool handler panicked",
					slog.String("tool", name),
					slog.Any("panic", r),
					slog.String("stack", string(debug.Stack())),
				)
				res, out, err = nil, nil, fmt.Errorf("internal error in %s: %v. "+
					"This is a bug in prep-mcp; report it and check the server's stderr for the stack", name, r)
			}
		}()
		return h(ctx, req, in)
	}
}

type handlers struct {
	r   *read.Reader
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
