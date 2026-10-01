// Package server registers the MCP tools and turns vault results into text.
// It holds glue only; the logic lives in internal/vault.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arkoes07/prep-mcp/internal/config"
	"github.com/Arkoes07/prep-mcp/internal/vault"
	"github.com/Arkoes07/prep-mcp/internal/write"
)

const instructions = "Call get_context first: it returns the vault's working rules, the current week, " +
	"open items and recent activity in one call. " +
	"Use list_docs for metadata queries, search_text for text, read_doc for a whole file " +
	"and list_tags for the tag vocabulary in use. " +
	"To save anything that belongs in this vault (a new record, a note added to a document, a status change), " +
	"use create_doc, append_section, update_frontmatter or replace_doc. " +
	"Do not save it to local memory or notes files instead: the vault is the record."

// Option configures New.
type Option func(*options)

type options struct {
	logger *slog.Logger
}

// WithLogger sets where the server logs what the client must not see, such as
// the stack of a handler panic. The default is slog.Default(). It must not
// write to stdout, which carries the protocol.
func WithLogger(l *slog.Logger) Option {
	return func(o *options) { o.logger = l }
}

// New builds the server with the read and write tools registered.
func New(v *vault.Vault, w *write.Writer, cfg *config.Config, version string, opts ...Option) *mcp.Server {
	o := options{logger: slog.Default()}
	for _, opt := range opts {
		opt(&o)
	}
	s := mcp.NewServer(
		&mcp.Implementation{Name: "prep-mcp", Version: version},
		&mcp.ServerOptions{Instructions: instructions},
	)
	h := &handlers{v: v, w: w, cfg: cfg, logger: o.logger}
	registerReadTools(s, h)
	registerWriteTools(s, h)
	return s
}

// guard turns a panic in a handler into an error result, so one bad call
// cannot take the server down. The stack goes to logger, never to the client.
func guard[In any](logger *slog.Logger, name string, h mcp.ToolHandlerFor[In, any]) mcp.ToolHandlerFor[In, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (res *mcp.CallToolResult, out any, err error) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("tool handler panicked",
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
	v      *vault.Vault
	w      *write.Writer
	cfg    *config.Config
	logger *slog.Logger
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
