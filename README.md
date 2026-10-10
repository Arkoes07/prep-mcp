# prep-mcp

An MCP server that lets an AI assistant read **and safely write** a folder of markdown notes with YAML frontmatter: the kind of vault you already keep in Obsidian.

Most markdown MCP servers are read-only. This one is built for the harder case: an assistant that logs, appends and updates files a human also edits, without clobbering them.

- Query by type, tags (AND / OR / prefix), frontmatter fields and date. Full-text search.
- `get_context` returns everything a new session needs in one call.
- Writes are append, frontmatter patch, or full rewrite guarded by a content hash.
- Every AI write is a git commit, and `batch` makes many writes one: checked first, all or nothing. No delete tool. Paths confined to the vault.
- Document types live in a config file, so it works for any vault.

Not in scope: a UI (Obsidian is the reading and editing surface), a database or index (files on disk are the only source of truth), deleting files (a human does that), multiple users, auth or a network transport.

## Install

You need Go 1.25 or newer, and git if you want a commit per write.

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

Any folder of markdown files works. A file shows up in queries when its frontmatter has a `type` that `prep.config.yaml` at the vault root defines:

```yaml
tags_file: tags.md              # optional: allowed tags, written as `prefix/value` in backticks
commit_on_write: true           # commit each write when the vault is its git repo's root
author: "Claude <claude@local>" # author of those commits
summary_fields: [kind, stage, status] # fields shown next to each document in lists

types:
  session:
    folder: sessions
    filename: "{date}-{kind}-{slug}.md" # {slug}: slug argument or title, {date}: the type's date field or today,
    required: [kind, date]              # any other {name} is that frontmatter field; it must be required
  improvement:
    folder: improvements
    filename: "{slug}.md"
    required: [status, exit_criteria]
  week:
    folder: weeks
    filename: "{week}.md"
    required: [week]
  application:
    folder: applications
    filename: "{slug}.md"
    required: [company, role, stage, status]
    date_field: applied           # optional: dates, since/until and sorting use applied (default: date)
    show: [stage, status, next_action, next_action_date] # optional: shown instead of summary_fields

context:                          # what get_context returns
  current_week_type: week
  current_week_field: week        # compared with the ISO week, for example 2026-W03
  open_items:
    - {type: improvement, where: {status: [open, improving]}}
    - {type: application, where: {status: [active]}}
  recent:
    - {type: session, limit: 5}
```

This is part of `examples/vault/prep.config.yaml`, the config behind the examples below.

- A `CLAUDE.md` at the vault root is returned first by `get_context`, as the assistant's working rules.
- `templates/` can be read but never written. Folders starting with a dot, such as `.git` and `.obsidian`, are never read or written.
- With Obsidian, open the vault with "Open folder as vault". "Create new vault" makes a new, empty folder inside the one you pick.

`examples/vault` is a complete vault with seven types, a tag vocabulary and templates.

## Example usage

Ask in plain language and the assistant picks the tool. These are real results from the example vault. Its data is dated January 2026, so ask for that week.

**"What's on for the week of 2026-01-14?"** calls `get_context` with `date: 2026-01-14`:

```text
==== CLAUDE.md ====
# Example vault
...
==== Current week: 2026-W03 ====
path: weeks/2026-w03.md
hash: b484cc9de79003ff8871df17e6acd8ab5d744ec055d0abd3db206761d206cd1b
...
==== Open items ====
improvement (status: open, improving):
- improvements/buries-headline.md | Buries the headline | date=2026-10-02 date_source=mtime status=open | -
- improvements/missing-tradeoff.md | Trade-off not stated | date=2026-10-02 date_source=mtime status=improving | -

application (status: active):
- applications/acme-backend.md | acme-backend | date=2026-01-05 stage=interview status=active next_action=Send thank-you note next_action_date=2026-01-16 | company/acme

==== Recent ====
session (newest 5):
- sessions/2026-01-14-deep-dive-checkout-rewrite.md | 2026-01-14-deep-dive-checkout-rewrite | date=2026-01-14 kind=deep-dive | q/hardest-project, issue/missing-tradeoff
...
==== Tags ====
Top tags, last 30 days (since 2025-12-15): q/hardest-project (3), issue/missing-tradeoff (2), company/acme (1), ...
```

The application is dated by its `applied` field and lists its next action, because its type sets `date_field` and `show`. The improvements have no date field, so their date is the file's modification time (the day you copied the vault), marked `date_source=mtime`.

**"Which sessions show the buries-headline issue?"** calls `list_docs` with `{"type": "session", "tags": ["issue/buries-headline"]}`:

```json
{"docs":[{"path":"sessions/2026-01-12-deep-dive-checkout-rewrite.md","type":"session","title":"2026-01-12-deep-dive-checkout-rewrite","tags":["q/hardest-project","issue/buries-headline","issue/missing-tradeoff"],"date":"2026-01-12","date_source":"field","fields":{"kind":"deep-dive"}}],"count":1}
```

**"Mark buries-headline as improving."** calls `update_frontmatter`. Only that line changes, and `updated` is stamped:

```diff
 title: Buries the headline
-status: open
+status: improving
 exit_criteria: Headline in the first sentence in 5 sessions in a row
 tags: []
+updated: 2026-10-02
```

**"Add '- Fri: mock, headline first' to this week's log."** calls `append_section`, which adds the line at the end of the existing `## Log` section.

**"Log a deep-dive session on the checkout rewrite for 2026-01-16, tagged issue/missing-tradof."** calls `create_doc`. The typo in the tag is caught, and nothing is written or committed:

```text
unknown tag "issue/missing-tradof"; closest known tag: "issue/missing-tradeoff". Use a known tag (list_tags shows them all), or add it to tags.md first
```

Each write that succeeds is its own commit:

```text
$ git -C ~/prep-demo log --format='%h | %an | %s' -2
13cbe6e | Claude | append_section: weeks/2026-w03.md
e4b41e9 | Claude | update_frontmatter: improvements/buries-headline.md
```

**"Move the Acme application to offer, log the offer call in its timeline, and add it to this week's log."** calls `batch` with three ops, one of them on a file another op already changed:

```json
{"ops":[
  {"tool":"update_frontmatter","args":{"path":"applications/acme-backend.md","set":{"stage":"offer"}}},
  {"tool":"append_section","args":{"path":"applications/acme-backend.md","heading":"Timeline","content":"- 2026-01-16 offer call"}},
  {"tool":"append_section","args":{"path":"weeks/2026-w03.md","heading":"Log","content":"- Fri: Acme offer call"}}]}
```

Every op is checked before anything is written; had one failed, nothing would have been written and every failure would have been listed. Both files land in one commit:

```text
ae0dfba | Claude | batch: 2 append_section, 1 update_frontmatter
```

**Rewriting a whole file** takes the hash that `read_doc` returned. If someone changed the file since, `replace_doc` writes nothing and returns the current hash, so the assistant re-reads the file and merges.

**Every write keeps the document rules**: the frontmatter parses, the type is one the config defines, the type's required fields have values, the type's date field holds a date (YYYY-MM-DD) when set, and the tags are in the vocabulary. A write that breaks one fails and writes nothing. One the document already broke, say a field a human left empty or a tag from before the vocabulary, is only a warning: the write goes through, and can fix it. A file without a type, such as the vault's working rules, is not warned about for having none.

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

See [docs/adr](docs/adr/) for why each of these exists and what it costs.

## Develop

```bash
go test ./...                                       # all tests; the ones that touch files use a temp copy of examples/vault
go test -race ./...
go vet ./... && gofmt -l .
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
  sleep 1; } | prep-mcp --root examples/vault
```

The `sleep` keeps input open long enough for the answer: the server stops at the end of its input. Logs go to stderr, and stdout carries only the protocol.

## Status

Personal project. Issues welcome.
