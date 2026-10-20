# prep-mcp

An MCP server that lets an AI assistant read **and safely write** a folder of markdown notes with YAML frontmatter: the kind of vault you already keep in Obsidian.

Most markdown MCP servers are read-only. This one is built for the harder case: an assistant that logs, appends and updates files a human also edits, without clobbering them.

- Query by type, tags (AND / OR / prefix), frontmatter fields and date. Full-text search.
- `get_context` returns everything a new session needs in one call.
- Writes are create, append, frontmatter patch, one exact text replacement, or full rewrite guarded by a content hash.
- A write may not make a document worse: one that would break a required field, a date or the tag vocabulary fails, writes nothing and says what to do next.
- Every AI write is a git commit, and `batch` makes many writes one: checked first, all or nothing. No delete tool. Paths confined to the vault.
- Document types live in a config file, so it works for any vault.

Not in scope: a UI (Obsidian is the reading and editing surface), a database or index (files on disk are the only source of truth), deleting files (a human does that), multiple users, auth or a network transport.

## Install

You need Go 1.26.9 or newer, and git if you want a commit per write. An older `go` (1.21 or later) fetches it on its own unless `GOTOOLCHAIN=local` is set.

```bash
git clone https://github.com/arkoes07/prep-mcp.git && cd prep-mcp
go install ./cmd/prep-mcp        # installs $(go env GOPATH)/bin/prep-mcp
```

Without a checkout, `go install github.com/arkoes07/prep-mcp/cmd/prep-mcp@latest` installs the same binary, but you won't have the example vault.

### Try it on the example vault

```bash
cp -R examples/vault ~/prep-demo
git -C ~/prep-demo init -q && git -C ~/prep-demo add . && git -C ~/prep-demo commit -qm init
```

Copy the vault out first: writes are only committed when the vault is the root of its own git repository, and `examples/vault` is a folder inside this one.

### Claude Code

```bash
claude mcp add --scope user prep -- "$(go env GOPATH)/bin/prep-mcp" --root ~/prep-demo
claude mcp list                  # prep: ✓ Connected
```

### Claude Desktop

Open Settings → Developer → Edit Config, and add the server under `mcpServers`, next to any settings already in the file:

```json
{
  "mcpServers": {
    "prep": {
      "command": "/Users/you/go/bin/prep-mcp",
      "args": ["--root", "/Users/you/prep-demo"]
    }
  }
}
```

Use absolute paths: Claude Desktop does not expand `~` or load your shell's `PATH`. Quit it fully (⌘Q) and reopen it. Settings → Developer shows whether the server is running, and its log is `~/Library/Logs/Claude/mcp-server-prep.log`.

### Options

| Flag | Environment | Default |
|---|---|---|
| `--root` | `PREP_ROOT` | the current directory |
| `--config` | `PREP_CONFIG` | `<root>/prep.config.yaml` |
| `--version` | | |

The config is read once at startup, so restart the client after you change it. A config with mistakes stops the server and lists every problem. A missing default config starts it with no types, so reads still work.

## Your own vault

Any folder of markdown files works. A file shows up in queries when its frontmatter has a `type` that `prep.config.yaml` at the vault root defines. Start from [`examples/vault/prep.config.yaml`](examples/vault/prep.config.yaml), which explains every key.

- A `CLAUDE.md` at the vault root is returned first by `get_context`, as the assistant's working rules.
- `templates/` can be read and written, but queries leave it out. Folders starting with a dot, such as `.git` and `.obsidian`, are never read or written.
- With Obsidian, open the vault with "Open folder as vault". "Create new vault" makes a new, empty folder inside the one you pick.

`examples/vault` is a complete vault with seven types, a tag vocabulary and templates.

## Example session

A real Claude Code session against a copy of `examples/vault`, trimmed at each `…` but otherwise unedited: `>` is the prompt, `→` a tool call, `←` its result, and the last line is the reply. The vault's data is dated January 2026, and the tag in the prompt is misspelt on purpose.

```text
> Log a deep-dive session on the checkout rewrite for 2026-01-16, tagged issue/missing-tradof, and add a line to that week's log.

→ get_context {"date":"2026-01-16"}
← ==== CLAUDE.md ====
  # Example vault
  …
  ==== Current week: 2026-W03 ====
  path: weeks/2026-w03.md
  …
→ list_tags {"prefix":"issue/"}
← {"tags":[{"tag":"issue/missing-tradeoff","count":2,"last":"2026-01-14"},{"tag":"issue/buries-headline","count":1,"last":"2026-01-12"}],"count":2}
→ read_doc {"path":"sessions/2026-01-14-deep-dive-checkout-rewrite.md"}
← path: sessions/2026-01-14-deep-dive-checkout-rewrite.md
  …
→ batch {"ops":[
    {"tool":"create_doc","args":{"type":"session","slug":"checkout-rewrite","frontmatter":{"type":"session","kind":"deep-dive","date":"2026-01-16","subject":"[[checkout-rewrite]]","tags":["issue/missing-tradeoff"]}}},
    {"tool":"append_section","args":{"path":"weeks/2026-w03.md","heading":"Log","content":"- Fri: deep dive, trade-off missing"}}],
  "message":"Log 2026-01-16 checkout rewrite deep dive"}
← {"results":[{"path":"sessions/2026-01-16-deep-dive-checkout-rewrite.md",…},{"path":"weeks/2026-w03.md",…}],"commit":"19fdacb","warnings":[]}

I used the tag `issue/missing-tradeoff` instead of `issue/missing-tradof`. That's the existing tag and matches `improvements/missing-tradeoff.md`, so the typo would have been rejected.
```

Both files landed in one commit, authored by Claude. In three runs of this prompt, the model checked the tag vocabulary first every time. Sent as typed, the same batch fails and nothing is written:

```text
nothing was written; 1 of 2 ops failed (fix them and send the whole batch again):
op 1 (create_doc session): unknown tag "issue/missing-tradof"; closest known tag: "issue/missing-tradeoff". Use a known tag (list_tags shows them all), or add it to tags.md first
```

## Tools

| Tool | What it does |
|---|---|
| `get_context` | Working rules, current week, open items, recent activity |
| `list_docs` | Metadata query: type, tags, any_tags, where, since, until; `fields` picks what is shown; `group_by` returns counts instead |
| `read_doc` | Full file plus content hash, by path or by `[[name]]` |
| `search_text` | Substring search with matching lines |
| `list_tags` | Tags with counts and last use |
| `create_doc` | New file at the path its type defines |
| `append_section` | Append under a heading |
| `update_frontmatter` | Change fields or tags, body untouched |
| `edit_text` | Replace one exact piece of text, for small fixes |
| `replace_doc` | Full rewrite, rejected if the file changed since you read it |
| `batch` | Up to 200 of the five writes above in one call: all or nothing, one commit |

Why the tools are shaped this way is in [DESIGN.md](DESIGN.md).

## Develop

Start with [DESIGN.md](DESIGN.md) for how the packages fit together.

```bash
go test ./...                                       # all tests; the ones that touch files use a temp copy of examples/vault
go test -race ./...
go vet ./... && gofmt -l .
go run golang.org/x/vuln/cmd/govulncheck@latest ./...  # reachable vulnerabilities, as CI checks
go test ./internal/read -run TestContextGolden -update   # rewrite the golden files after an intended format change
go test -bench Scan -run '^$' ./internal/read       # scan cost: 2,000 generated files, and examples/vault
go build -ldflags "-X main.version=v0.1.0" ./cmd/prep-mcp
```

Rules for working on the code are in [CLAUDE.md](CLAUDE.md).

To try a change in a client, run `go install ./cmd/prep-mcp` and restart the client so it starts the new binary. To talk to the server without a client:

```bash
{ printf '%s\n' \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"sh","version":"0"}}}' \
    '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
    '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_context","arguments":{"date":"2026-01-14"}}}'
  sleep 1; } | "$(go env GOPATH)/bin/prep-mcp" --root examples/vault
```

The `sleep` keeps input open long enough for the answer: the server stops at the end of its input. Logs go to stderr, and stdout carries only the protocol.

## Status

Personal project. Issues welcome.
