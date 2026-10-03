# prep-mcp: PRD

A local MCP server, in Go, that gives an AI assistant read and **safe write** access to a folder of markdown files with YAML frontmatter.

The code knows nothing about any one vault: document types come from a config file. It ships as a public repo with a fictional example dataset.

Single user, runs locally over stdio. No auth, no network, no UI. The files are browsed and edited by a human in Obsidian or any editor.

---

## 1. Problem

Every new AI session starts from zero. The workaround is a handover prompt pasted at the start of each session, and it goes stale: it summarises instead of giving access, and nothing the AI learns in a session is written back in a structured way.

Existing markdown MCP servers (for example lethain/library-mcp) are read-only. The hard part is not reading. It is letting an agent **write** into files a human also edits, without clobbering the human's edits, and with every AI change auditable.

## 2. Goals

1. A session can get everything it needs to start work in one call (`get_context`).
2. The AI can query by type, tags, status and date, and do full-text search.
3. The AI can create files, append to them, and patch frontmatter, safely.
4. Every AI write is a git commit, so it can be reviewed and reverted.
5. Nothing in the Go code is specific to interview prep.

## 3. Non-goals (v1)

- No UI. Obsidian is the reading and editing surface.
- No database and no persisted index. Files on disk are the only source of truth.
- No delete tool. Deletion is a human action.
- No multi-user, auth, or network transport.
- No audio transcription (phase 2).

---

## 4. Data model

### 4.1 Root

One root directory (the "vault"), given by env var or flag. Every read and write is confined to it.

```
PREP_ROOT=/Users/alwi/prep  (default: current directory)
PREP_CONFIG=<root>/prep.config.yaml  (default)
```

### 4.2 Documents

A document is any `.md` file under the root that starts with YAML frontmatter:

```markdown
---
type: session
title: ...
tags: [q/failure, issue/buries-headline]
---
body...
```

- `type` is required. Files without frontmatter or without `type` are ignored by queries (but still readable by path).
- `tags` accepts a YAML list or a comma-separated string. Normalise to a lowercased, trimmed list. (Same normalisation as library-mcp.)
- Wikilinks `[[slug]]` in the body or frontmatter are left as text in v1.
- Ignore dot-folders (`.git`, `.obsidian`) and the `templates/` folder.

### 4.3 Config file (`prep.config.yaml`)

Types, their folders, filename patterns and required fields live here, not in Go code.

```yaml
tags_file: tags.md          # controlled vocabulary, optional
commit_on_write: true
author: "Claude <claude@local>"
summary_fields: [kind, stage, status]   # shown next to each document in lists, optional

types:
  story:
    folder: stories            # may contain subfolders
    filename: "{slug}.md"
    required: [title, tags]
  session:
    folder: sessions
    filename: "{date}-{kind}-{slug}.md"
    required: [kind, date]
  improvement:
    folder: improvements
    filename: "{slug}.md"
    required: [status, exit_criteria]
  week:
    folder: weeks
    filename: "{isoweek}.md"     # e.g. 2026-W41.md
    required: [week]
  application:
    folder: applications
    filename: "{slug}.md"
    required: [company, role, stage, status]
    date_field: applied        # optional: the field that dates this type (default: date)
    show: [stage, status, next_action, next_action_date]   # optional: replaces summary_fields
  learning:
    folder: learning
    filename: "{slug}.md"
    required: [title]
  reference:
    folder: reference
    filename: "{slug}.md"
    required: [title]

context:                    # what get_context returns (see 5.1)
  current_week_type: week
  open_items:
    - {type: improvement, where: {status: [open, improving]}}
    - {type: application, where: {status: [active]}}
  recent:
    - {type: session, limit: 5}
```

Placeholders: `{slug}`, `{date}` (YYYY-MM-DD), `{isoweek}` (YYYY-Www), and any frontmatter field, e.g. `{kind}`. Values are sanitised to lowercase kebab-case.

Two optional keys per type (added 2026-10-02; a config without them behaves as before):
- `date_field`: the frontmatter field that dates documents of this type, default `date`. `since`/`until`, date sorting, `get_context`'s recent queries, tag dates and the `{date}` placeholder use it. A document without it is dated by file mtime.
- `show`: the fields listed next to each document of this type in `get_context` and `list_docs`. It replaces the top-level `summary_fields` for that type; `show: []` lists none.

### 4.4 Tag vocabulary (`tags.md`)

Optional. Every line matching `` `prefix/value` `` in backticks defines an allowed tag. When the file exists, write tools **warn** (not fail) on unknown tags and return the warning in the result, so the AI can fix a typo.

---

## 5. MCP tools

Keep the set small. Every tool returns plain text or compact JSON that is readable in a chat.

### 5.1 Read

**`get_context()`**
The session-start bundle. Replaces the handover prompt.
Returns, in order:
1. The contents of `CLAUDE.md` at the root, if present (the working rules).
2. The current ISO week document, in full, if it exists.
3. Metadata for every `open_items` query in the config.
4. Metadata for the `recent` queries, newest first.
5. A one-line tag summary: the 10 most-used tags in the last 30 days.
6. Problems, when there are any: files it could not use, and git lock files (`index.lock`, `HEAD.lock`) that would stop every AI commit. It never removes a lock.

Target size: under ~4k tokens for a typical vault. Metadata only for lists; full content only for the week doc and CLAUDE.md.

**`list_docs(type?, tags?, any_tags?, where?, since?, until?, sort?, limit?, fields?)`**
Metadata only: path, type, title, tags, `date`, `date_source`, `updated`, and under `fields` the shown fields the document sets (its type's `show`, else `summary_fields`).
- `date` is YYYY-MM-DD from the type's date field, else file mtime; `date_source` says which: `field` or `mtime`. Every result has both.
- `tags`: all must match. `any_tags`: at least one. Prefix match allowed with a trailing `/`, e.g. `issue/` matches every issue tag.
- `where`: frontmatter equality, e.g. `{"status": "open"}`; a list value means "any of".
- `since`/`until`: compared against that same date.
- `sort`: `date_desc` (default) | `date_asc` | `title`.
- `limit`: default 50.
- `fields`: field names to show instead of the configured ones, for every result; `[]` shows none.

**`read_doc(path)`**
Full file content plus a `hash` (sha256 of the bytes). The hash is what `replace_doc` needs.

**`search_text(query, type?, limit?)`**
Case-insensitive substring search over bodies and titles. Returns path, title, and up to 3 matching lines with line numbers.

**`list_tags(prefix?)`**
Every tag with document count and most recent date, sorted by count. With `prefix`, only tags under it (e.g. `issue/`). Unknown tags (not in `tags.md`) are marked.

### 5.2 Write

All writes: path must resolve inside the root after cleaning and symlink resolution; must end in `.md`; must not be inside `.git`, `.obsidian`, or `templates/`. Then commit (see 5.3).

**`create_doc(type, frontmatter, body, slug?)`**
Builds the path from the type's config. Validates required fields. Sets `created` if absent. Fails if the file already exists (return the existing path so the AI can decide to append instead). Returns the path, the hash and any tag warnings.

**`append_section(path, heading, content)`**
Appends `\n## {heading}\n\n{content}\n` to the end of the file. If a section with that exact heading already exists, appends the content under it instead of creating a duplicate heading. This is the default way to add to a living document: weekly review, application timeline, session feedback.

**`update_frontmatter(path, set?, unset?, add_tags?, remove_tags?)`**
Changes frontmatter only; the body bytes are untouched. Sets `updated` to today. Use for status/stage changes and tagging.

**`replace_doc(path, content, expected_hash)`**
Full rewrite. Rejected if the current file hash differs from `expected_hash` (the human edited it in the meantime). The error returns the current hash so the AI can re-read and merge. This is optimistic concurrency, the same idea as an `If-Match` header.

**`batch(ops, message?)`**
Several writes in one call, all or nothing, as one commit. Use it for imports and any change to more than one document, instead of going around the server. Each op is `{tool, args}`: one of the four tools above with exactly its arguments.
- Ops run in order against the vault as the earlier ops leave it. A path may appear more than once and is written once, with its final content. `replace_doc` must be the first op on its path.
- Every op is checked first: path safety, required fields, hashes, file-exists rules. If any fails, nothing is written and every failure is returned, labelled by op number.
- If a write fails partway, every file already written is put back to its previous bytes and every created file is removed before the error returns. A file someone saved after the batch wrote it is left alone and named in the error.
- At most 200 ops per call.
- One commit, with `message` if given, else a count of ops per tool: `batch: 2 create_doc, 9 update_frontmatter`.
- Returns `{"results": [{path, hash, warnings}, ...], "commit", "warnings"}`: one result per op in order, `hash` being the file's hash after the batch, then the commit and its warnings.

The four single write tools behave as batches of one.

### 5.3 Git

If the root is a git repo and `commit_on_write` is true, every successful write runs `git add <path>` and `git commit -m "<tool>: <path>"` with the configured author, so AI commits are distinguishable from mine in `git log`. A batch is one commit of every file it wrote. A failed commit does not fail the write; it is reported as a warning.

If git reports a lock file left by another git process (`.git/index.lock`, `.git/HEAD.lock`), the warning names it, says whether the change was staged (`index.lock` stops `git add`, so nothing is; `HEAD.lock` stops only the commit, so it is), and says to delete the lock once no git process is running. The server never deletes a lock. Never push.

### 5.4 Errors

Errors are returned as tool results with a clear message and the next action, e.g. `file exists: sessions/2026-10-05-deep-dive-ads-insight-center.md (use append_section)`. Never panic the server.

---

## 6. Design decisions

Record each of these as an ADR in `docs/adr/` with the alternative considered, why it was chosen, and its cost. Any new decision made during implementation, or any change to one below, is added to `docs/adr/` the same way.

| Decision | Instead of | Why | Cost |
|---|---|---|---|
| Scan files on every call | In-memory index at startup + `rebuild` tool (library-mcp) | The server writes; a startup index goes stale the moment the AI logs something | A full scan per call. Fine up to a few thousand files. Add an mtime-keyed cache only if measured slow |
| No persisted tag index | A `tags-index.json` | A second copy of the frontmatter can drift. That is a dual write | Tag queries cost a scan |
| Append and frontmatter-patch tools | Only full-file write | The human edits the same files; partial writes cannot clobber prose | More tools; the AI must choose the right one |
| `expected_hash` on full rewrite | Last write wins | Detects a concurrent human edit instead of silently losing it | The AI must re-read and retry on conflict |
| Git commit per write | Trusting the AI | Every AI change is reviewable and revertible | Noisy history (squash later if wanted) |
| No delete tool | A delete tool with confirmation | The file tools are a prompt-injection surface; a document could instruct the model to delete things | The human deletes manually |
| Path confinement + no writes to `.git`/`templates` | Trusting the path argument | Same threat model | None worth naming |
| Types in config | Types in Go code | Reusable for any markdown vault, not just interview prep | A config file to learn |
| Few combinable tools | One tool per filter (`get_by_tag`, `get_by_date_range`...) | Models pick tools better from a small set; filters compose | Slightly more complex parameters |

---

## 7. Stack

- Go 1.22+.
- MCP over stdio. Use the official Go SDK (`github.com/modelcontextprotocol/go-sdk`) if its API is stable at build time; otherwise `github.com/mark3labs/mcp-go`. Pick one and note why in an ADR.
- YAML: `gopkg.in/yaml.v3`. Preserve key order when rewriting frontmatter (use `yaml.Node`, not a map).
- Git: shell out to `git`. No go-git.
- No other dependencies unless justified.

Layout:

```
cmd/prep-mcp/main.go
internal/vault/      scan, parse frontmatter, query, path safety
internal/write/      create, append, patch, replace, git
internal/config/     load + validate prep.config.yaml
internal/server/     MCP tool registration and handlers
examples/vault/      fictional dataset + prep.config.yaml
docs/adr/
README.md
```

---

## 8. Tests (required, not optional)

- **Path safety**: `../`, absolute paths, symlink pointing outside the root, `.git/config`, `templates/x.md`, a non-`.md` file. All rejected.
- **Hash conflict**: read, modify the file on disk, `replace_doc` with the old hash, expect rejection and the new hash in the error.
- **Frontmatter patch preserves the body byte-for-byte** and preserves key order.
- **Append to an existing heading** does not duplicate the heading.
- **Tag normalisation**: list, comma string, mixed case, whitespace.
- **Query filters**: `tags` AND, `any_tags` OR, prefix `issue/`, `where` with list values, `since`.
- **get_context** against `examples/vault` produces a stable snapshot (golden file).
- **Batch**: one invalid op among valid ones writes nothing and reports every failure; a write failing midway restores the earlier files byte for byte and removes created ones; N ops make exactly one commit by the AI author; a `replace_doc` op with a stale hash rejects the whole batch.
- **Shown fields**: a type's `show` fields appear in `get_context` and `list_docs`; `fields` overrides them.
- **Date field**: `since`/`until` and sorting use the configured field; a document without it falls back to mtime with `date_source: "mtime"`.
- **Git lock**: with `index.lock` or `HEAD.lock` present the write succeeds, the warning names the lock, and the lock is left alone.

Run against a temp copy of `examples/vault`, never the real data.

---

## 9. Acceptance

1. `claude mcp add prep -- prep-mcp --root ~/prep` works; `get_context` returns CLAUDE.md, this week, open improvements, active applications, and the last 5 sessions.
2. "Log this session" produces a correctly named file under `sessions/` and a git commit authored by the configured AI author.
3. If I edit a file in Obsidian while a session is open, `replace_doc` refuses and the AI re-reads and merges.
4. `list_docs(type=session, tags=[issue/buries-headline])` returns every session showing that pattern, newest first.
5. An unknown tag produces a warning naming the closest known tag.
6. Pointing the server at `examples/vault` works with no other setup.
7. Nothing in the Go code mentions interviews, stories or applications.

---

## 10. Phase 2 (not now)

- `pending_recordings()` and `transcribe(path)` via whisper.cpp (or MacWhisper JSON export), returning word timings and computed metrics: wpm, pauses over 700 ms, longest pause, time to first full stop. The model should never estimate these.
- mtime-keyed scan cache, only if measured slow.
- Parse `[[wikilinks]]` into a backlink query.
