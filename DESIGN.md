# Design decisions

Each entry: the decision, what it replaced, why, and what it costs. Fill in as they are made; PRD section 6 is the starting list. Entries marked *(deviation)* differ from PRD.md. PRD.md was left as written until 2026-10-02, when sections 4.3, 5 and 8 were updated for the changes listed under "What changed after using it for N weeks"; the deviations recorded before that date still stand.

## Scan on every call, no persisted index
- Instead of: an in-memory index built at startup plus a `rebuild` tool (lethain/library-mcp), or a stored index file.
- Why: this server writes. A startup index is stale the moment the AI logs something; a stored index is a second copy of the frontmatter that can drift, which is a dual write.
- Cost: a full scan per call. Measured at 59 ms for 2,000 files (`go test -bench Scan -run ^$ ./internal/read`, Apple M4, 25 MB and 234k allocations per scan; 20 folders, about 1.2 KB per file, half with a `date`). `examples/vault` (8 documents) scans in 0.27 ms (`BenchmarkScanExample`, same machine, 118 KB and 1,162 allocations), and the first real vault, about 100 files, works out near 3 ms at the 2,000-file rate. That is far below anything a person notices next to a model call, so no cache. Add the mtime-keyed cache only if a real vault is measured slow.

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
- Cost: noisy history. Since 2026-10-02 a change to several documents can be one `batch` and one commit (see "Batch writes" below).

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
- Also decided: the temp file is created 0600 and chmod-ed to its final mode before the rename (an existing file keeps its mode; a new file gets 0644), so it is never briefly more open than the result. Its name is `.prep-tmp-<random>`.
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

## Writes: serialised, one retry, and a seam for the moment between write and rename (M2)
*Since 2026-10-02 every write runs through the batch engine ("Every write is a batch of one" below), which keeps the behaviour described here: a file that changes before its rename makes the write start over once from the new content, and `replace_doc` then fails its hash check with a `ConflictError`.*

- Instead of: unlocked writes, or a lock file.
- Why: the server may get overlapping tool calls, so one mutex per `Writer` serialises them. A human can still save the file at any time, so `append_section` and `update_frontmatter` re-hash the file after staging the temp file and just before the rename. If it changed, they start over once from the new content, so the human's save is kept. If it changes twice in a row they stop with a "re-read and retry" error and write nothing. `replace_doc` never retries: a change after the hash check is a `ConflictError`, because the caller's content was based on the old version.
- Test seam: an unexported `beforeRename` hook runs after the temp file is written and before the rename. `internal/write/seam_test.go` (package `write`) uses it to inject a failure, to simulate a human save in that window, and to check that no `.prep-tmp-*` file survives either. An injected-failure test needs some way in; this is the least invasive one, and production code never sets it.
- Cost: a package-level variable that exists only for tests. `create_doc` checks that the file does not exist, stages, and checks again before the rename, but a rename cannot refuse to overwrite, so a file created in those microseconds would be replaced. Accepted: a human creating the same filename at that instant is not a case worth a hard-link dance that some filesystems lack.

## A nil `Git` means "do not commit, do not warn" (M2)
- Instead of: always passing a `Git` and letting it decide.
- Why: `commit_on_write` is true in the example config. Once M3 gives `Git` real behaviour, a non-repo vault gets a "not committed" warning on every write, which is right for the server but would make every "no warnings" assertion in the write tests depend on git state. Tests pass `nil`; `main` passes a real `Git`.
- Cost: two ways to be "off" (a nil `Git` and `commit_on_write: false`).

## Frontmatter values: who decides how they are written (M2)
- Decided: `JSONToNode` gives each node the tag and style that make the encoder write it the way a person would; `ApplyPatch` only encodes.
  - A string that is a valid `YYYY-MM-DD` is tagged `!!timestamp`, which makes the encoder leave it plain, so Obsidian still sees a date.
  - A string that plain YAML would read as anything else is double-quoted: numbers, `true`/`null`, lists (`[[x]]`), maps, comments, empty, leading/trailing space, and `yes`/`no`/`on`/`off`. A lone `y` or `n` is left plain, since YAML 1.2 tools (Obsidian) read them as strings.
  - A list of scalars is written in flow style (`[x, y]`); a list holding lists or objects, or a mapping, in block style. Tag lists follow the rule below.
  - Multi-line strings are written as block scalars by the encoder.
- Comments: a rewritten key keeps its line comment, with one space before it. The original spacing is not kept; untouched lines are byte-identical, so aligned comments on other keys survive.
- Setting a key to the value it already has (compared by what it says, not how it is quoted) is a no-op: no write, no `updated`.
- `updated` is replaced in place when present, appended otherwise, and left alone when the patch sets or unsets it.
- Tags: comparison is on normalised tags, so adding a tag that is already there, or removing one that is not, changes nothing. A tag list a patch touches is written back normalised in the style it had: a block list stays a block list at its own indent, anything else (flow list, comma string, absent, null) becomes a flow list. Comments on individual tag items are lost when the list is rewritten. Removing the last tag gives `tags: []`.
- Removing a tag the document does not have is reported by the writer as a warning; `ApplyPatch` itself has no warnings channel.
- Cost: the quoting rule is ours, not the encoder's, and needs its own tests. Rewriting a tag list drops item comments.

## Appending to a section: what "end of the section" and "blank line" mean (M2)
- Rules: the insertion point is after the last non-blank line of the section, so blank lines before the next heading stay where they were. A blank line separates the new text from the old unless both the last old line and the first new line are list items. A section with nothing in it gets a blank line after its heading. The file's line ending (LF or CRLF) is used for everything inserted. Frontmatter is never searched, so a `## x` YAML comment cannot match.
- Consequence: a file that is only frontmatter gets a blank line between the closing `---` and the new `## heading`.
- Cost: list detection is a simple prefix test (`-`, `*`, `+`, `1.` or `1)`), so an indented continuation line counts as prose.

## Replacing a document (M2)
- Rules: the hash is checked first, then the new content. Content that is not valid frontmatter, or misses a required field of a configured type, is rejected. Content with no frontmatter, or no `type`, is accepted with a warning that the document will no longer appear in queries. A type that is not configured is accepted and only checked for valid YAML. Tag warnings cover only tags the new content adds compared with the old file.
- Cost: an unconfigured type gets no required-field check, so a typo in `type` (for example `wek`) is accepted without a warning. `create_doc` is stricter because the type is an argument and must be configured. `update_frontmatter` skips the required-field check for a type that is not configured, for the same reason.

## `create_doc`'s description names the configured types; the description test uses a neutral config (M2)
- Instead of: a static description, or checking descriptions against the example config.
- Why: the description is generated from the config so the model never reads the config file, and descriptions must also be free of domain words. The example config's type names are domain words by design, so both cannot hold for the example vault. The Go code only ever adds words that come from the config; `TestDescriptionsAreDomainNeutral` therefore runs against a vault whose config uses neutral names, and a separate test checks that the description lists the example vault's types, patterns and required fields.
- Cost: the neutrality check does not see what a real config puts into the description. That text is data the owner chose.

## The server's text tells the model to write to the vault, not to its own memory (M2)
- Found in M2d: asked to "log this session", Claude Code saved a note to its own auto-memory and never called `create_doc`. The client's memory instructions outrank a tool description, and MCP tools are loaded on demand.
- Decided: the server `instructions` and the `create_doc` and `append_section` descriptions say to use the write tools, and not local memory or notes files, for anything that belongs in the vault. The words are domain-neutral. `TestTextSteersWritesToTheVault` guards them.
- Instead of: putting the rule in `examples/vault/CLAUDE.md`, which is data and only reaches the model if `get_context` runs first. The example can still name the tools.
- Cost: this steers but cannot force. The model can still ignore it, and the prompt naming the tool is the reliable fallback.

## Tag checks stay quiet without a vocabulary (M2)
- Instead of: warning about every tag when the tags file is missing or has no tags.
- Why: with nothing to compare against there is no closest tag to name, and a warning that cannot help is noise.
- Cost: a vocabulary file that exists but is empty turns the check off.

## Commit only when the vault is its git repo's root
- Instead of: committing whenever the vault is anywhere inside a git work tree.
- Why: the real vault is the root of its own private repo, so it commits. `examples/vault` sits inside the public prep-mcp repo and must never receive AI commits. Both paths are compared after `EvalSymlinks`. A vault that isn't a repo root gets the write plus a warning explaining how to fix it.
- Also decided:
  - The commit covers only the written paths (`git commit --only -- <paths>`), so a human's staged work is not swept in.
  - The committer identity is set to the configured author too, so commits work without `user.name`.
  - Hooks stay enabled, with a 10 s timeout and `GIT_TERMINAL_PROMPT=0`.
  - Any git failure is a warning, never a failed write.
  - Never push.
- Where the vault path lives: in the MCP registration (`claude mcp add --scope user prep -- prep-mcp --root ~/prep`), not in `prep.config.yaml`, which lives inside the vault.
- Cost: a vault kept in a subfolder of a bigger repo gets no commits until an opt-in setting is added. An uncommitted human edit to the same file is included in the AI's commit, because a commit covers the whole file.

## How the git step behaves (M3)
Details the entry above leaves open. The tests are `internal/write/git_test.go`.
- Steps, all inside one deadline (`Timeout`, 10 s by default, for the whole commit and not per command): find the binary, `git rev-parse --show-toplevel` and compare with the vault root after `EvalSymlinks`, `git add -- <paths>`, `git diff --cached --quiet -- <paths>`, `git commit --only -m <msg> --author=<author> -- <paths>`, `git rev-parse --short HEAD`. A single write has one path; a batch has every file it wrote. Git runs with `GIT_TERMINAL_PROMPT=0`, `LC_ALL=C` and the author as committer (`GIT_COMMITTER_NAME/EMAIL`). Output is captured, never printed, because stdout is the protocol channel.
- Every failure is a warning that starts with `not committed:` and says what to do: `git init` in the vault, fix the hook or lock, install git, or set `commit_on_write: false`. The text includes git's own output, joined onto one line and cut at 400 characters, so a hook's message reaches the human.
- **A write that changes nothing in the committed file is not a commit and not a warning.** `git diff --cached --quiet` after the `add` finds that (for example a `replace_doc` that writes the same bytes as HEAD). Instead of: letting `git commit` fail with "nothing to commit" and reporting that as a warning. No-op `update_frontmatter` calls never reach git at all.
- **On a timeout the whole process group gets SIGTERM, then SIGKILL after 2 s** (`git_unix.go`). Instead of: the standard library's default, which kills only git. Why: a hook runs as a child that holds git's output pipes open, so killing git alone made the call wait for the hook (30 s in the test) and a SIGKILL would leave `.git/index.lock` behind, which would break every later commit. With SIGTERM git removes its own lock; checked by hand: after a timeout there is no `index.lock` and the next write commits. Cost: Unix only. `git_other.go` falls back to the default kill, so on Windows a hung hook can outlive the call. Nobody runs this there yet.
- **A failed commit leaves the path staged, when `git add` got through.** After a hook rejects a new file, or `HEAD.lock` stops the commit, `git add` has already run, so the file stays in the index until a later `commit --only` for the same path, or the human, commits it. When `git add` itself fails (`index.lock`), nothing is staged. The warning says which (see the lock entry below). Instead of: running `git reset` to undo the add. Why: that is one more git call that can fail, in a repo that is already in an odd state, and the file does belong in the next commit. Cost: `git status` shows it as staged.
- `Result.Commit` is the abbreviated hash (`git rev-parse --short`), as it is easier to read in a chat. It is a prefix of the full hash.
- `Git` resolves symlinks itself, although `vault.Open` already hands it a resolved path. Why: `Git.Commit` takes any directory, and the symlinked `~/prep` case is tested on its own. Cost: one redundant `EvalSymlinks`.
- Not done: a hook that needs a terminal or a GPG pinentry will time out rather than prompt (that is the point of `GIT_TERMINAL_PROMPT=0` plus the deadline). `commit.gpgsign=true` in the user's config may therefore cost the full timeout on every write (not tested).

## No domain words in any Go file, tests included
- Instead of: applying CLAUDE.md's rule to non-test code only.
- Why: Acceptance 7 says "nothing in the Go code". Expectations that name example-specific types or paths live in `testdata/*.yaml` and golden files, which are data. `TestNoDomainWords` enforces the rule using a word list kept in `internal/testvault/testdata`. Tool descriptions avoid the words too, which `TestDescriptionsAreDomainNeutral` checks with the same list through `testvault.ForbiddenWords`.
- Cost: example-specific assertions are one step removed from the test code.

## `internal/testvault`, a test-only helper package *(deviation from PRD section 7 layout)*
- Instead of: repeating copy-and-setup code in each package's tests.
- Why: every test uses a temp copy of `examples/vault` with fixed mtimes (git doesn't preserve them) and a frozen clock, so golden output is deterministic. The git tests also cut out your git config and environment, but that is `isolateGit` in `internal/write/git_test.go`, not this package: only they run git.
- It also owns the forbidden word list and its matcher (`ForbiddenWords`), because two packages check against it. Instead of: each test reading `cmd/prep-mcp/testdata` with its own parser, which let the two checks drift apart.
- Cost: one extra package outside the PRD layout.

## `internal/read` split out of `internal/vault` *(deviation from PRD section 7 layout)*
- Instead of: one `vault` package holding both the shared core and every query (scan, filter, search, tag counts, `get_context`).
- Layout: `server -> read -> vault` and `server -> write -> vault`. `vault` keeps what both sides need: `Open`, path confinement (`Resolve`, `Root`, `ReadFile`), `Hash`, frontmatter parsing and tag normalisation and vocabulary. It imports no other package of this module: callers pass it the tags file instead of the config. `read` holds `Reader` (`List`, `Search`, `TagStats`, `Context`, `ReadDoc`, `Now`), `Scan`, `Filter`, and the date helpers. `write` is unchanged.
- Why: `write` never used the query code, and nothing in the query code uses `write`; the package boundary now says so, and `server` imports `read` and `write` only. `vault` loses its clock and the query code, and stays the small part both sides must trust. The planned scan cache is keyed by mtime, so it can live in `read` without `write` having to invalidate it.
- Names: `read.Reader` and `read.New` mirror `write.Writer` and `write.New`. The single-file read is `ReadDoc`, not `Read`, because `Read` on a type named `Reader` suggests `io.Reader`. `vault.Read` became `vault.ReadFile` (no hash; `ReadDoc` adds it). `vault.Apply(docs, f)` became `Filter.Apply(docs)`. `scalar` is exported as `vault.Scalar` because both packages need it.
- Cost: one more package outside the PRD layout, and `vault` exports `ReadFile` and `Scalar` for `read`.

## `get_context` output format (M1)
- Instead of: JSON, or Markdown headings for the sections. PRD 5.1 lists the content but not the format.
- Format: plain text in sections introduced by `==== name ====` lines, in PRD order: `CLAUDE.md`, `Current week: <week>`, `Open items`, `Recent`, `Tags`, and a final `Problems` section only when there are any. The hand-written `internal/read/testdata/context.golden` is the specification.
- Why:
  - `==== ... ====` cannot be confused with the `##` headings inside the embedded week document, which Markdown headings would be.
  - Text costs fewer tokens than JSON and reads well in a chat.
  - The week section carries the file's `path` and `hash`, so `replace_doc` can be used without another `read_doc`.
  - Lists are one line per document: `- path | title | attributes | tags`. Attributes are `date=`, then the type's `show` fields, else `summary_fields`, in config order (see below).
  - ~~`date=` appears only when the document has a `date` field.~~ Since 2026-10-02 every line shows `date=`, followed by `date_source=mtime` when the date is the file's mtime (see "`date` and `date_source`" below).
  - Each query block is labelled from its config (`improvement (status: open, improving):`, `session (newest 5):`), so the output explains itself.
- Cost: the format is ours, not a standard, and a change means regenerating the golden files by hand. The model has to read a convention instead of parsing JSON.

## Dates are calendar days in local time
- Instead of: comparing instants.
- Why: `date` fields are calendar dates, and mtime is only a fallback. `ParseDate` returns the date as written at local midnight and drops any time of day, so `2026-01-12T09:30:00Z` is 2026-01-12 in every timezone. `since` and `until` compare the local calendar day of a document's date or mtime, inclusive. Without this, the same vault would answer differently in UTC+13.
- Cost: a timestamp near midnight in a `date` field is filed under the day written, not the day in the reader's timezone. Whatever follows the date after a `T` or a space is not checked, so `2026-01-12T99:99` reads as 2026-01-12 instead of being reported.
- Consequence to know: undated documents take their mtime as date, so on a fresh clone every undated document looks as if it were touched today. `get_context` then counts their tags in the 30-day summary. The fallback is PRD 5.1, and the documents that need real dates should get a `date` field.

## Broken files are reported, not hidden
- Instead of: silently skipping a file with invalid frontmatter, or failing the whole call.
- Why: a typo in one file must not blind every query, and it must not vanish either. Such files are skipped from results and listed under `problems` in `list_docs` and in the `Problems` section of `get_context`. A file without frontmatter, or with frontmatter but no `type`, is skipped silently, because that is normal (`CLAUDE.md`, plain notes). An unparseable `date` and a second week document for the same week are also problems. So is a folder or file the server cannot read (for example a permissions problem): it is skipped and reported, and every other document is still returned.
- Frontmatter errors name the line in the file, not the line inside the YAML block, so a human can jump to it. The line is `yaml.v3`'s own, shifted past the opening fence, and the message says "near line N". Instead of: re-parsing ever shorter prefixes of the block to find the exact line, which took about 30 lines and up to 1000 parses. Cost: `yaml.v3` counts from 0 for parser errors (an unclosed `[` or `{`), so those point one line too early; scanner errors (a tab, `: :`) are exact. Duplicate keys are rejected, because `yaml.v3` does not detect them when decoding into a `yaml.Node`.
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
- Why: the PRD sets only `list_docs`' default of 50. The caps, the other defaults, inclusive date bounds and exact `where` matching (a list field matches if any element does) had to be chosen, and the tests exposed the small gaps.
- Cost: no truncation. A vault with hundreds of open items produces a long `get_context`.

## `list_docs` returns `date` or `modified`, never a body
*Superseded on 2026-10-02 by "`date` and `date_source` in every list result" below: the `modified` key is gone. Bodies are still never returned, and the `note` for an unknown type stays.*

- Instead of: one `date` field that is sometimes a file mtime.
- Why: a model reading `date` should be able to trust it. A document with a `date` field returns `date`; one without returns `modified` (the file's last-modified day) instead. `updated` appears when present, and so do the configured summary fields, under `fields`. Bodies are never returned. A `type` that is not configured and matches nothing returns a `note` listing the configured types.
- Cost: a client that wants one sort key must read either field.

## Summary fields come from config *(deviation from PRD 5.1)*
*Amended on 2026-10-02: a type's `show` list replaces `summary_fields` for that type (see "Per-type `show` fields" below).*

- Instead of: showing `status`, `stage` and `kind` whenever a document has them, as PRD 5.1 lists for `list_docs`.
- Why: those three names come from one vault's types, and nothing in the code acts on them; they are only displayed. Fixed in Go, they broke goal 5 in a way the forbidden-word test cannot see, because the words are generic. A vault with `priority` or `owner` fields would have had to rename them to get them shown.
- How: a top-level `summary_fields` list in `prep.config.yaml`. `get_context` shows them as attributes in config order after `date=`. `list_docs` returns the ones a document sets in a `fields` object, and its tool description is generated from the config so the model knows the names. The example config sets `[kind, stage, status]`, so the example output is unchanged.
- Fields that carry meaning stay fixed, because the data model defines them: `type`, `title`, `tags`, `date`, `created`, `updated`. Validation rejects listing an always-shown one (`type`, `title`, `tags`, `date`, `updated`), an empty name or a repeat.
- Cost: one more config key, and the default is no summary fields at all. In `list_docs` the values moved from top-level keys (`"status": "open"`) into `"fields": {"status": "open"}`. Only scalar values are shown.

## Batch writes: all or nothing, one commit (2026-10-02)
- Instead of: a commit per call, which made nine frontmatter edits nine commits and pushed a 60-file import around the server, without its checks or the AI author; or a write-ahead journal that could finish or undo a batch after a crash.
- How: `batch(ops, message?)` takes `{tool, args}` ops, where `args` are exactly a write tool's arguments.
  - Every op is checked first, in order, against an in-memory view of the vault as the earlier ops leave it: path safety, required fields, hashes, file-exists rules. Any failure writes nothing and returns every failure, labelled by op number. An op whose arguments do not decode is one of those failures, in the same response.
  - Then each changed file is written once, with its final content, through the same atomic replace and rename-time checks as a single write.
  - If a write fails, the files already written are put back in reverse order and created files removed.
  - One commit covers every file written, with the caller's message or `batch: 2 create_doc, 9 update_frontmatter`. At most 200 ops per call.
- Also decided:
  - A path may appear more than once, and later ops see earlier ones, so "set the stage and append to the timeline" is one batch. `replace_doc` must be the first op on its path, because its hash is of the file as the caller read it.
  - Putting a file back is guarded by the hash of what the batch wrote, so a human's save made after the batch's write is never overwritten; that file is left as it is and named in the error.
  - A file that changes between the checks and its rename (a human saving in Obsidian) makes the batch put back what it wrote and start over once from disk, as single writes always did. Twice in a row, it gives up with nothing written.
  - A per-op `hash` is the file's hash after the whole batch, so two ops on one path report the same hash.
- Cost:
  - Atomic per file, not per batch: rollback is best effort, not a transaction. A crash, `kill -9` or power loss while files are being written leaves a partial batch on disk, with nothing to finish or undo it; `git status` shows which files changed. A journal would close that at the price of a recovery step at every start.
  - A file a human saved after the batch wrote it is not rolled back, so a failed batch can leave that one file with the batch's change under the human's edit. The error names it.
  - A put-back that itself fails (disk full, permissions) is reported, not retried.
  - A restored file has its old bytes and mode but a new inode and mtime, so Obsidian sees it touched; git sees no change.
  - Folders made for new files stay behind, empty, after a rollback, as after a failed `create_doc`.
  - The whole batch is held in memory; the 200-op cap bounds it.
  - One commit means one `git revert` undoes the whole batch, never part of it.

## Every write is a batch of one (2026-10-02)
- Instead of: keeping a code path per write tool next to the batch engine.
- How: `Writer.Apply(ctx, op)` runs one op, `Writer.Batch(ctx, ops, message)` several; `CreateOp`, `AppendOp`, `PatchOp` and `ReplaceOp` describe every write, each carrying the function that applies it to the plan. In the server, a write tool's arguments and a batch op's go through the same decoder (`buildOp`).
- Why: one set of guarantees and one set of tests. The existing write, seam and git tests passed through the engine unchanged and now guard it too. Single tools keep their own error texts (unlabelled) and commit messages (`<tool>: <path>`).
- Cost: a single write builds a one-op plan it does not need. A `replace_doc` whose content equals the file is now a no-op rather than a rewrite of the same bytes; there was no commit either way.

## Per-type `show` fields (2026-10-02)
- Instead of: only the top-level `summary_fields`, which showed an application's `stage` and `status` but never `next_action`, the field that says a follow-up is due.
- How: a type may set `show: [fields]`, listed in that order in `get_context` and under `fields` in `list_docs`. It replaces `summary_fields` for that type; `show: []` lists nothing; other types keep `summary_fields`. `list_docs` takes `fields: [...]` to choose the fields for one call, for every document; `[]` lists none. Validation is that of `summary_fields`, plus the type's own date field, which is already shown as `date`.
- Why replace rather than merge: a type's list then says exactly what its lines show, and `show: []` can hide fields.
- Cost: a type with `show` must repeat any summary field it still wants. `list_docs`' `fields` is a JSON object, which `encoding/json` writes with sorted keys, so only `get_context` keeps the configured order.

## Per-type `date_field` (2026-10-02)
- Instead of: the fixed field `date`, which made the real vault rename `applied` to `date` so applications could be filtered by date.
- How: a type may set `date_field`, default `date`. The scan reads it, so `since`/`until`, date sorting, `get_context`'s recent queries and the tag dates follow it, and so does the `{date}` filename placeholder. A document without the field is dated by mtime; a `date` key on a type whose date field is another is an ordinary field. Validation rejects a blank `date_field` and `type`, `title` or `tags`, which never hold a date.
- Why `{date}` follows it: otherwise a type with `date_field: applied` and `{date}-{slug}.md` would name its files with today's date.
- Cost: one more config key. Without a `type`, `list_docs` dates each document by its own type's field, so one `since` can compare different fields for different types.

## `date` and `date_source` in every list result (2026-10-02)
- Instead of: `date` for a document with a date field and `modified` for one without, so results carried one key or the other depending on the file.
- How: every `list_docs` entry has `date` (YYYY-MM-DD) and `date_source`, `field` or `mtime`. Every `get_context` line shows `date=`, followed by `date_source=mtime` when the date is the file's mtime; a field date prints `date=` alone, to save tokens.
- Why: one key to sort and read by, while the source still says when not to trust it (a clone or checkout resets mtime).
- Cost: the `modified` key is gone, so anything that read it breaks; only the model did. Lines for undated documents in `get_context` are about 30 characters longer.

## A git lock: warn, never delete; flag it at session start (2026-10-02)
- Instead of: deleting a stale `index.lock` or `HEAD.lock`, or passing git's raw message on as the warning.
- Why: a live lock cannot be told from a stale one, and deleting the lock of a running git (a GUI, the Obsidian Git plugin, an open commit editor) can corrupt the index. The lock is the human's to remove. On the first day a manual commit left locks that would have blocked every later AI commit, behind a generic warning on each write.
- How: when git says `Unable to create '<path>.lock': File exists`, the write's warning names the lock, says whether the change was staged, and says to delete the lock once no git process is running, with the command that commits afterwards: paths shell-quoted (or `<the paths in results>` past five), the write's message and the configured author, so it runs as shown and keeps the AI author. Timeout and cancel warnings carry the same command. `get_context` lists `.git/HEAD.lock` and `.git/index.lock` under Problems when commits are on. It only looks.
- Cost: until the human removes the lock, every AI write is saved but not committed.
  - With `index.lock`, `git add` fails, so nothing is staged: the writes are plain working-tree changes.
  - With `HEAD.lock` (or a branch's ref lock), `git add` succeeds and only the commit fails, so each write stays staged. The next successful AI commit of the same path picks it up; other paths stay staged.
  - Either way, writes the human commits by hand without that command lose the AI author.
  - The write warning relies on git's English message, which `LC_ALL=C` pins and a test checks. The session-start check knows only those two lock names, and only in a `.git` folder: a vault that is a worktree or submodule (`.git` is a file) gets no session-start check, though its write warnings still name the lock.

## Server clock
- `read.Reader.Now()` is the read side's clock. Production passes `time.Now`, tests pass a fixed day. `get_context` uses it only when the caller gives no `date`.
- `server.New(r, w, cfg, version)` takes the `*read.Reader` and the `*write.Writer`. The writer has its own clock, injected the same way, and uses it for `created`, `updated` and the `{date}` default.

## What changed after using it for N weeks

### 2026-10-02: first day, a vault of about 100 files

| Real use exposed | Answered by |
|---|---|
| A 60-file import went around the server with shell and file tools, so it skipped the path, required-field and tag checks, and its commit had no AI author. | `batch`: up to 200 checked writes in one call, all or nothing, one commit by the AI author. |
| Nine frontmatter edits made nine commits. | `batch`. |
| A file with invalid YAML was caught and listed under Problems, and every other document was still served. | Nothing: it worked as designed ("Broken files are reported, not hidden"). |
| A write into `templates/` was refused. | Nothing: the read-only guard worked as designed. |
| `get_context` listed applications without `next_action`, so a due follow-up did not show. | Per-type `show`. |
| Applications could only be filtered by date after renaming `applied` to `date`. | Per-type `date_field`. |
| Some list results had `date` and others `modified`, depending on the file. | `date` and `date_source` in every result. |
| A manual commit left git lock files that would have blocked every later AI commit. | A lock warning that names the lock and never deletes it, and a check in `get_context`. |

Measured the same day: a scan of `examples/vault` takes 0.27 ms; 2,000 files take 59 ms, so the real vault is near 3 ms ("Scan on every call").
