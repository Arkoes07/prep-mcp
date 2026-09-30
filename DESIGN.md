# Design decisions

Each entry: the decision, what it replaced, why, and what it costs. Fill in as they are made; PRD section 6 is the starting list. Entries marked *(deviation)* differ from PRD.md; PRD.md itself is left as written. PLAN.md has the full reasoning for each open question referenced here (#n).

## Scan on every call, no persisted index
- Instead of: an in-memory index built at startup plus a `rebuild` tool (lethain/library-mcp), or a stored index file.
- Why: this server writes. A startup index is stale the moment the AI logs something; a stored index is a second copy of the frontmatter that can drift, which is a dual write.
- Cost: a full scan per call. Measured at ___ ms for ___ files.

## Partial writes by default
- Instead of: a single write-file tool.
- Why: a human edits the same files in Obsidian. Append and frontmatter-patch cannot clobber prose.
- Cost: four write tools instead of one; the model has to pick.

## Optimistic concurrency on full rewrite
- Instead of: last write wins.
- Why: detects a human edit made mid-session instead of silently losing it.
- Cost: the model re-reads and retries on conflict.

## Git commit per AI write
- Why: every AI change is reviewable (`git log --author=Claude`) and revertible.
- Cost: noisy history.

## No delete, confined paths
- Why: a server that writes files is a prompt-injection target. Document content can contain instructions.
- Cost: deletion is manual.

## Types in config, not code
- Why: reusable for any markdown vault.
- Cost: a config file to learn.

## MCP SDK choice
- Chose: the official `github.com/modelcontextprotocol/go-sdk` (v1.8.0, 2026-09-04).
- Instead of: `github.com/mark3labs/mcp-go` (v1.1.1, 2026-09-23).
- Why: the PRD's condition is met. The official SDK has been stable v1 since 2025-09-30, a year of compatible minor releases, while mcp-go reached v1.0 only on 2026-09-02. It tracks the spec first (2026-07-28 and all older versions) and is co-maintained with the Go team. `mcp.AddTool[In, Out]` infers the input schema from a struct and validates arguments before the handler runs. A returned `error` becomes an `IsError` tool result. `mcp.NewInMemoryTransports()` makes wiring tests easy.
- Cost:
  - The SDK does not recover handler panics, so every handler is wrapped in a small `guard` that turns a panic into an `IsError` result.
  - go.sum is larger (jwt, oauth2 and x/tools come in transitively; unused packages are not linked).
  - Frontmatter arguments must be re-decoded from the raw `Arguments` JSON into `yaml.Node` to keep key order. That would be the same with either SDK.
- Checked against proxy.golang.org and the tagged source on 2026-09-29.

## Go 1.25 minimum *(deviation)*
- Instead of: Go 1.22+ (PRD section 7).
- Why: both SDKs require it (go-sdk v1.8.0 declares `go 1.25.0`, mcp-go v1.1.1 `go 1.25.5`). It also gives the full `os.Root` API used for path confinement.
- Cost: nothing older than Go 1.25 can build it.

## Path confinement in two layers, through `os.Root`
- Instead of: checking the cleaned path string and then using ordinary `os` calls.
- Why:
  - A lexical check gives clear, actionable errors. It rejects absolute paths, `..` escapes, `\`, NUL, dot components, root-level `templates/` (case-insensitive, for APFS) and non-`.md` files.
  - All I/O then goes through `os.Root`, which the kernel confines and which follows only symlinks that stay inside the root. That closes the check-then-swap race a string check alone leaves open.
- Also decided:
  - Writes refuse any symlink component in the path.
  - Reads follow symlinks that stay inside the root.
  - The scanner never follows symlinks.
  - The root is resolved once with `EvalSymlinks`.
- Cost: two layers to keep consistent. Tests cover both.

## read_doc may read `templates/`
- Instead of: treating `templates/` as invisible to every tool.
- Why: the model can follow a template's structure when it creates a document. Queries still skip `templates/`, and writes to it are still refused.
- Cost: none worth naming. Templates hold structure, not data.

## Config loaded once at startup
- Instead of: re-reading `prep.config.yaml` on every call, the way documents are.
- Why: an invalid config fails fast with every problem listed, rather than failing tool calls one by one.
- Cost: editing the config needs a server restart.
- Precedence: `--root` > `PREP_ROOT` > cwd, and `--config` > `PREP_CONFIG` > `<root>/prep.config.yaml`. A missing *default* config starts the server with no types, so reads work. A missing *explicit* config is fatal.

## `get_context` takes an optional `date` *(deviation)*
- Instead of: `get_context()` with no parameters, always using the server clock.
- Why: it lets the model or a human ask "what did week X look like", and lets demos run against the fictional 2026-W03 example. `date` defaults to the server's local today, so the normal call is still argument-free.
- Also decided: writes (`created`, `updated`, the `{date}` default) always use the server clock. No env override exists.
- Cost: one optional parameter. A model could pass a wrong date, which is why the default is the server clock rather than a required argument.

## Current week found by field, via a new `current_week_field` key *(deviation)*
- Instead of: building the week file's path from its filename pattern and reading that path.
- Why: the lookup is "the doc of `current_week_type` whose `current_week_field` equals this ISO week". It compares case-insensitively and ignores filenames, so week files a human creates by hand in Obsidian are found whatever they are named.
- Cost: one more config key (`context.current_week_field: week`), which must be in that type's `required`. If two docs match the same week, the first by path wins and the other is reported as a problem.

## `{week}` field placeholder instead of the `{isoweek}` builtin; every filename lowercase *(deviation)*
- Instead of: a builtin `{isoweek}` computed from today, written in canonical `2026-W41` form (PRD section 4.3).
- Why:
  - The week is a parameter: the `week` frontmatter field fills the filename. This means next week's plan can be created in advance.
  - One case rule with no exceptions: every placeholder value is sanitised to lowercase kebab-case, as PRD 4.3 says, so `week: 2026-W41` → `weeks/2026-w41.md`.
  - The example file was renamed from `weeks/2026-W03.md` to `weeks/2026-w03.md` to match.
- Builtins left: `{slug}` (the `slug` argument, else `title`) and `{date}` (frontmatter `date`, else today). Every field placeholder must be in the type's `required`, which config validation enforces.
- Cost: existing uppercase week files stay as they are; they are still found by field. On a case-sensitive filesystem, create_doc would not detect `2026-W41.md` as an existing `2026-w41.md`.

## Frontmatter patch by line splice
- Instead of: decoding the frontmatter into `yaml.Node`, changing it, and re-encoding all of it.
- Why: re-encoding collapses the aligned `# a | b` comments the templates produce, so every edit to a real file would show a noisy diff. `yaml.Node` is used to find each top-level key's line span. Only changed keys are rewritten; each keeps its line comment and flow or block style. New keys go before the closing fence. The body is copied byte-for-byte.
- Safety net: before writing, re-parse and check that untouched keys are unchanged and the body bytes are identical. If not, abort with an error.
- Cost: more code than a round-trip, and multi-line flow values at column 0 are an edge case.

## Atomic writes (temp file + rename)
- Instead of: writing the target file in place.
- Why: a crash never leaves a half-written note. A hard link to a file outside the vault is replaced rather than written through. The mode of an existing file is kept.
- Cost: the file's inode changes on every write, and a dot-prefixed temp file briefly exists next to the target.

## Tag warnings only for tags a write adds; closest tag by edit distance
- Instead of: warning about every unknown tag in the file, or failing the write.
- Why: warning about tags a human already had would nag on every write. The closest known tag is chosen by Levenshtein distance over normalised tags, with ties broken alphabetically. That gives Acceptance 5 a concrete suggestion.
- Also decided: every backticked `` `x/y` `` token anywhere in `tags.md` is a vocabulary tag, because the example puts all tags on one line.
- Cost: a vocabulary typo already in a file is never flagged.

## Required fields checked on every write, not only create_doc
- Instead of: validating required fields only in create_doc (PRD 5.2).
- Why: otherwise replace_doc and update_frontmatter (for example, unsetting a field) could produce documents create_doc would reject. Absent, null, `""`, `[]` and `{}` all count as missing.
- Cost: a human-made doc that is already missing a required field must be fixed before the AI can rewrite it.

## Commit only when the vault is its git repo's root
- Instead of: committing whenever the vault is anywhere inside a git work tree.
- Why: the real vault is the root of its own private repo, so it commits. `examples/vault` sits inside the public prep-mcp repo and must never receive AI commits. Both paths are compared after `EvalSymlinks`. A vault that isn't a repo root gets the write plus a warning explaining how to fix it.
- Also decided:
  - The commit covers only the written path (`git commit --only -- <path>`), so a human's staged work is not swept in.
  - The committer identity is set to the configured author too, so commits work without `user.name`.
  - Hooks stay enabled, with a 10 s timeout and `GIT_TERMINAL_PROMPT=0`.
  - Any git failure is a warning, never a failed write.
  - Never push.
- Where the vault path lives: in the MCP registration (`claude mcp add --scope user prep -- prep-mcp --root ~/prep`), not in `prep.config.yaml`, which lives inside the vault.
- Cost: a vault kept in a subfolder of a bigger repo gets no commits until an opt-in setting is added. An uncommitted human edit to the same file is included in the AI's commit, because a commit covers the whole file.

## No domain words in any Go file, tests included
- Instead of: applying CLAUDE.md's rule to non-test code only.
- Why: Acceptance 7 says "nothing in the Go code". Expectations that name example-specific types or paths live in `testdata/*.yaml` and golden files, which are data. `TestNoDomainWords` enforces the rule using a word list kept in testdata. Tool descriptions avoid the words too.
- Cost: example-specific assertions are one step removed from the test code.

## `internal/testvault`, a test-only helper package *(deviation from PRD section 7 layout)*
- Instead of: repeating copy-and-setup code in each package's tests.
- Why: every test uses a temp copy of `examples/vault` with fixed mtimes (git doesn't preserve them) and an isolated git environment, so golden output is deterministic and your git config never leaks in.
- Cost: one extra package outside the PRD layout.

## `get_context` output format (M1)
- Instead of: JSON, or Markdown headings for the sections. PRD 5.1 lists the content but not the format.
- Format: plain text in sections introduced by `==== name ====` lines, in PRD order: `CLAUDE.md`, `Current week: <week>`, `Open items`, `Recent`, `Tags`, and a final `Problems` section only when there are any. The hand-written `internal/vault/testdata/context.golden` is the specification.
- Why:
  - `==== ... ====` cannot be confused with the `##` headings inside the embedded week document, which Markdown headings would be.
  - Text costs fewer tokens than JSON and reads well in a chat.
  - The week section carries the file's `path` and `hash`, so `replace_doc` can be used without another `read_doc`.
  - Lists are one line per document: `- path | title | attributes | tags`. Attributes are `date=`, `kind=`, `stage=`, `status=`, in that order.
  - `date=` appears only when the document has a `date` field. File mtime orders documents but is never shown as a date, because a clone or checkout resets it.
  - Each query block is labelled from its config (`improvement (status: open, improving):`, `session (newest 5):`), so the output explains itself.
- Cost: the format is ours, not a standard, and a change means regenerating the golden files by hand. The model has to read a convention instead of parsing JSON.

## Dates are calendar days in local time
- Instead of: comparing instants.
- Why: `date` fields are calendar dates, and mtime is only a fallback. `ParseDate` returns the date as written at local midnight and drops any time of day, so `2026-01-12T09:30:00Z` is 2026-01-12 in every timezone. `since` and `until` compare the local calendar day of a document's date or mtime, inclusive. Without this, the same vault would answer differently in UTC+13.
- Cost: a timestamp near midnight in a `date` field is filed under the day written, not the day in the reader's timezone.
- Consequence to know: undated documents take their mtime as date, so on a fresh clone every undated document looks as if it were touched today. `get_context` then counts their tags in the 30-day summary. The fallback is PRD 5.1, and the documents that need real dates should get a `date` field.

## Broken files are reported, not hidden
- Instead of: silently skipping a file with invalid frontmatter, or failing the whole call.
- Why: a typo in one file must not blind every query, and it must not vanish either. Such files are skipped from results and listed under `problems` in `list_docs` and in the `Problems` section of `get_context`. A file without frontmatter, or with frontmatter but no `type`, is skipped silently, because that is normal (`CLAUDE.md`, plain notes). An unparseable `date` and a second week document for the same week are also problems.
- Frontmatter errors name the line in the file, not the line inside the YAML block, so a human can jump to it. Duplicate keys are rejected, because `yaml.v3` does not detect them when decoding into a `yaml.Node`.
- Cost: the scan does a little extra work and the output can carry a `Problems` list the model must not ignore.

## Search matches bodies and the `title` field only
- Instead of: searching whole files including frontmatter, or matching file names.
- Why: PRD 5.1 says bodies and titles. Matching frontmatter would return every document that merely carries a tag or a field value, which `list_docs` already answers.
- Rules: case-insensitive substring; a document without a `title` field is not matched by its filename stem, even though the stem is shown as its title; a title match is reported at the file line of the `title:` key, and `Text` is always the verbatim file line; hits are ordered by path; each hit reports up to 3 lines and the total count.
- Cost: searching for a frontmatter value needs `list_docs` with `where`.

## Limits and defaults
- `list_docs`: 50 results by default, at most 500. `search_text`: 20 by default, at most 500. Each context query: 20 unless its config says otherwise. Context tag summary: the 10 most used tags of the last 30 days, and no upper date bound.
- Every sort breaks ties by path, ascending, so output is deterministic.
- `list_tags` treats `prefix` as a plain string prefix, so `issue` and `issue/` both work.
- The default commit author is `Claude <claude@local>`. PRD 4.3 shows it only as an example value.
- Why: PLAN.md open question 23, plus the small gaps the tests exposed.
- Cost: no truncation. A vault with hundreds of open items produces a long `get_context`.

## `list_docs` returns `date` or `modified`, never a body
- Instead of: one `date` field that is sometimes a file mtime.
- Why: a model reading `date` should be able to trust it. A document with a `date` field returns `date`; one without returns `modified` (the file's last-modified day) instead. `status`, `stage`, `kind` and `updated` appear when present. Bodies are never returned. A `type` that is not configured and matches nothing returns a `note` listing the configured types.
- Cost: a client that wants one sort key must read either field.

## Server clock
- `vault.Vault.Now()` is the one clock. Production passes `time.Now`, tests pass a fixed day. `get_context` uses it only when the caller gives no `date`.
- `server.New(v, cfg, version)` has no writer argument until M2 adds the write tools; PLAN.md section 3 shows the final signature.

## What changed after using it for N weeks
- ___
