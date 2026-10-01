# prep-mcp

An MCP server that lets an AI assistant read **and safely write** a folder of markdown notes with YAML frontmatter: the kind of vault you already keep in Obsidian.

Most markdown MCP servers are read-only. This one is built for the harder case: an assistant that logs, appends and updates files a human also edits, without clobbering them.

- Query by type, tags (AND / OR / prefix), frontmatter fields and date. Full-text search.
- `get_context` returns everything a new session needs in one call.
- Writes are append, frontmatter patch, or full rewrite guarded by a content hash.
- Every AI write is a git commit. No delete tool. Paths confined to the vault.
- Document types live in a config file, so it works for any vault.

## Quick start

```bash
go install github.com/Arkoes07/prep-mcp/cmd/prep-mcp@latest
cp -R examples/vault ~/prep-demo
git -C ~/prep-demo init -q && git -C ~/prep-demo add . && git -C ~/prep-demo commit -qm init
claude mcp add prep -- prep-mcp --root ~/prep-demo
```

Copy the vault out first: writes are only committed when the vault is the root of its own git repository, and `examples/vault` is a folder inside this one.

Then ask: "What's on this week, and what should I work on first?"

## Tools

| Tool | What it does |
|---|---|
| `get_context` | Working rules, current week, open items, recent activity |
| `list_docs` | Metadata query: type, tags, any_tags, where, since, until |
| `read_doc` | Full file plus content hash |
| `search_text` | Substring search with matching lines |
| `list_tags` | Tags with counts and last use |
| `create_doc` | New file at the path its type defines |
| `append_section` | Append under a heading |
| `update_frontmatter` | Change fields or tags, body untouched |
| `replace_doc` | Full rewrite, rejected if the file changed since you read it |

See [DESIGN.md](DESIGN.md) for why each of these exists and what it costs.

## Status

Personal project. Issues welcome.
