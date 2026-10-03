# MCP SDK choice
- Chose: the official `github.com/modelcontextprotocol/go-sdk` (v1.8.0, 2026-09-04).
- Instead of: `github.com/mark3labs/mcp-go` (v1.1.1, 2026-09-23).
- Why: the PRD's condition is met. The official SDK has been stable v1 since 2025-09-30, a year of compatible minor releases, while mcp-go reached v1.0 only on 2026-09-02. It tracks the spec first (2026-07-28 and all older versions) and is co-maintained with the Go team. `mcp.AddTool[In, Out]` infers the input schema from a struct and validates arguments before the handler runs. A returned `error` becomes an `IsError` tool result. `mcp.NewInMemoryTransports()` makes wiring tests easy.
- Cost:
  - The SDK does not recover handler panics, so every handler is wrapped in a small `guard` that turns a panic into an `IsError` result.
  - go.sum is larger (jwt, oauth2 and x/tools come in transitively; unused packages are not linked).
  - Frontmatter arguments must be re-decoded from the raw `Arguments` JSON into `yaml.Node` to keep key order. That would be the same with either SDK.
- Checked against proxy.golang.org and the tagged source on 2026-09-29.
