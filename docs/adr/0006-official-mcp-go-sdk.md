# The official MCP Go SDK, and so Go 1.25

Chose `github.com/modelcontextprotocol/go-sdk` (v1.8.0) over `github.com/mark3labs/mcp-go` (v1.1.1), checked on 2026-09-29. The official SDK had been a stable v1 for a year, while mcp-go reached v1.0 on 2026-09-02; it tracks the spec first and is co-maintained with the Go team. `mcp.AddTool[In, Out]` infers and validates the input schema, and `mcp.NewInMemoryTransports()` makes wiring tests easy.

Cost: Go 1.25 at least (both SDKs need it; it also brings the full `os.Root` API). The SDK does not recover handler panics, so every handler is wrapped in `guard`. Frontmatter arguments are re-decoded from the raw JSON into `yaml.Node` to keep key order, as they would be with either SDK.
