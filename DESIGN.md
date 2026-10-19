# Design

Each decision is an ADR in [docs/adr](docs/adr/), with what it costs. This page indexes them, explains how writes behave, records what real use changed, and lists the limits still open. The terms are defined in [CONTEXT.md](CONTEXT.md).

## Decisions

| ADR | Decision | Cost |
|---|---|---|
| [0001](docs/adr/0001-scan-on-every-call.md) Scan on every call | Every call scans the files; there is no index, at startup or on disk. | A full scan per call. A cache waits until a real vault measures slow ([#2](https://github.com/arkoes07/prep-mcp/issues/2)). |
| [0002](docs/adr/0002-partial-writes-and-hash-guarded-rewrite.md) Partial writes, hash-guarded rewrite | Writes append or patch, which cannot clobber prose; a full rewrite needs the hash the caller read. | Several write tools instead of one; after a conflict the model re-reads and retries. |
| [0003](docs/adr/0003-git-commit-per-ai-write.md) A git commit per AI write | Every AI write is a commit, so it can be reviewed and reverted. | Noisy history, which `batch` reduces. |
| [0004](docs/adr/0004-no-delete-confined-paths.md) No delete, confined paths | No delete tool; no path leaves the vault or writes in `.git` or `templates/`. | Deletion is manual. |
| [0005](docs/adr/0005-types-in-config.md) Types in config | Types, folders, file names and required fields come from the config; the Go code names none. | A config file to learn. |
| [0006](docs/adr/0006-official-mcp-go-sdk.md) The official MCP Go SDK | `modelcontextprotocol/go-sdk` over `mark3labs/mcp-go`. | Go 1.25 or newer; every handler needs a panic guard. |
| [0007](docs/adr/0007-path-confinement-in-two-layers.md) Confinement in two layers | A check of the path text gives clear errors; `os.Root` then confines all I/O in the kernel. | Two layers to keep consistent. |
| [0008](docs/adr/0008-frontmatter-patch-by-line-splice.md) Frontmatter patch by line splice | A changed key is rewritten in place, the rest is copied byte for byte, and the result is re-parsed to check. | More code than a YAML round trip; multi-line flow values at column 0 are an edge case. |
| [0009](docs/adr/0009-commit-only-at-the-repo-root.md) Commit only at the repo root | Commit only when the vault is its repo's root, and only the written paths. | A vault in a subfolder of a bigger repo gets no commits; a human's uncommitted edit to a written file goes into the AI's commit. |
| [0010](docs/adr/0010-get-context-is-plain-text.md) `get_context` is plain text | Sections under `====` lines, one line per document. | A format of our own, specified by a golden file. |
| [0011](docs/adr/0011-batch-all-or-nothing.md) Batch: all or nothing | Every op is checked before anything is written, then one commit; at most 200 ops. | Atomic per file, not per batch: a crash mid-write leaves a partial batch. |
| [0012](docs/adr/0012-never-delete-a-git-lock.md) Never delete a git lock | A lock is reported with the command that commits afterwards, never removed. | Until the human removes it, writes are saved but not committed. |
| [0013](docs/adr/0013-few-combinable-tools.md) Few combinable tools | One `list_docs` with filters that combine, not one tool per filter. | More complex parameters. |
| [0014](docs/adr/0014-edit-text-without-a-hash.md) `edit_text` without a hash | The exact text it must match is the guard. | An edit can land on a file that changed elsewhere since the model read it. |
| [0015](docs/adr/0015-a-write-may-not-make-a-document-worse.md) A write may not make a document worse | An op fails only on a rule it breaks; a rule the document already broke is a warning. | A document can keep a broken rule indefinitely, warned about on every write. |

## How writes behave

**Every write keeps the document rules**: the frontmatter parses, the type is one the config defines, the type's required fields have values, the type's date field holds a date (YYYY-MM-DD) when set, and the tags are in the vocabulary. A write that breaks one fails and writes nothing. One the document already broke, say a field a human left empty or a tag from before the vocabulary, is only a warning: the write goes through, and can fix it. A file without a type, such as the vault's working rules, is not warned about for having none.

**Rewriting a whole file** takes the hash that `read_doc` returned. If someone changed the file since, `replace_doc` writes nothing and returns the current hash, so the assistant re-reads the file and merges.

**Dates** come from each type's date field (`date`, unless the type's `date_field` names another). A type without one is dated by the file's modification time, and listings say so with `date_source=mtime`.

## What use changed

In use on a real vault since 2 Oct 2026, through Claude Desktop and Claude Code. Each change started with a problem in use. All of them landed between 2 and 6 Oct; since then, use has only turned up the limits in the next section.

| Date | Trigger | Change |
|---|---|---|
| 2 Oct | A 60-file import had to go around the server: no checks, no AI author, and git lock files left behind. Nine frontmatter edits made nine commits. | `batch`: many writes, all checked first, all or nothing, one commit ([ADR 0011](docs/adr/0011-batch-all-or-nothing.md)). A lock git leaves behind is named in the write's warning and in `get_context` ([ADR 0012](docs/adr/0012-never-delete-a-git-lock.md)). |
| 2 Oct | `get_context` listed documents without the field that mattered for their type, so an overdue `next_action` was invisible. | A type's `show` picks the fields listed next to its documents. |
| 6 Oct | Tool descriptions misled the model: `batch` reported a per-op hash that was really the file's hash after the whole batch, and nothing said how to clear a field. | Wording only: the batch hash is explained, and `update_frontmatter` says to use `unset`, since `set ""` keeps an empty key that `where` treats differently. |
| 6 Oct | `list_tags` showed only tags in use, so the model could not see the valid ones before writing one, and an unknown tag only warned, so fixing it took a second commit. | `list_tags` includes unused vocabulary tags at count 0, and a write that adds an unknown tag fails, naming the closest known tag. |
| 6 Oct | `read_doc` needed an exact path, while files link to each other as `[[name]]`. | `read_doc` takes `[[name]]`, `[[folder/name]]`, aliases and headings; a name that matches several files fails with their paths. |
| 6 Oct | Fixing one line took a full `replace_doc` with a hash. | `edit_text` replaces one exact piece of text, which must match exactly once, with no hash ([ADR 0014](docs/adr/0014-edit-text-without-a-hash.md)). |
| 6 Oct | Counting documents by a field meant tallying a listing by hand. Then `where` and `group_by` read the same field differently: `" open"` grouped as open but did not match. | `list_docs` takes `group_by`, and one reader decides field values for every caller. |
| 6 Oct | `create_doc` kebab-cased the whole slug, so `books/ch05` became `books-ch05.md` and a folder convention could not be followed through the tool. | Each `/` segment of a slug is kebab-cased on its own, `..` is rejected, and both `[[ch05]]` and `[[books/ch05]]` resolve. |
| 6 Oct | Each write tool checked its own subset of rules in its own words. A file with broken frontmatter and an unknown tag could not be repaired. A date field such as `next week` named a file from its raw text. | Every op is checked once against the document rules: a write may not make a document worse, so it can repair one ([ADR 0015](docs/adr/0015-a-write-may-not-make-a-document-worse.md)). The file name and the scan use the same day, and setting a non-date fails. |

## Still open

- **The config is read only at startup.** A config change applies after the client restarts, and nothing warns that the running config is out of date.
- **No move tool.** Moving a file means `create_doc` at the new path, `edit_text` on each link to it, then a manual `git rm`. A move that runs `git mv` and rewrites inbound links would keep the no-delete boundary; it needs the backlinks of [#3](https://github.com/arkoes07/prep-mcp/issues/3).
- **Append-only documents outgrow one read.** `read_doc` returns the whole file. On 11 Oct a log that had reached 132 KB after 32 appends in a week was cut at the client's 25,000-token limit, and the cut lost the newest entries, the ones a session needs. Reading one section by heading, or the last few, would fix it.
- **`get_context` grows with the week.** It returns the current week's document in full, which is two thirds of the bundle by the end of a busy week (see below).
- **Frontmatter key order.** `create_doc` says it keeps the order the keys are sent in. On 11 Oct a document created through a client came out with its keys in alphabetical order. On 19 Oct, through Claude Code, a create kept the order sent. The server takes the frontmatter from the raw JSON to keep the order, and its tests check that, so the 11 Oct keys were reordered before the server decoded them; the cause is not traced.
- **`batch` reads the tag vocabulary once, at the start.** A batch whose first op adds a tag to the tags file fails on later ops that use the tag. Adding the tag in its own call first works.
- **Refused writes leave no trace.** A refused write commits nothing, so git cannot count hash conflicts or refusals; the only record is the client's MCP log.

## Measured, 19 Oct 2026

On an Apple M4. The real vault has 173 markdown files outside `templates/`. Estimates are marked.

- **Scan** (`go test -bench Scan -run '^$' ./internal/read`): 58.2 ms for 2,000 generated documents, about 29 µs each, and 0.27 ms for `examples/vault`. The real vault: about 5 ms at the same rate (estimate).
- **`get_context`**, a real response from the real vault: 44,956 characters at the end of a full week, about 11,200 tokens (estimate: characters / 4), 30 KB of it the week's document. 19,825 characters on the week's first day.
- **Largest document**: an append-only log of 212 KB, about 54,000 tokens (estimate), more than twice what the client returns from one read.
- **Writes** in the last full week (12 to 18 Oct): 63 of 81 commits were batches, so the commit messages no longer show which writes they held. `replace_doc`, the only full rewrite, made 3. The week added 2,988 lines and removed 407.
