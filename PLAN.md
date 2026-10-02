# Plan: day-one fixes (batch, show, date_field, date_source, git lock)

## Context

The first day on a real 100-file vault turned up five gaps:
- A 60-file import bypassed the server, which skipped the guardrails and lost the AI commit author.
- Nine frontmatter edits made nine commits.
- `get_context` hid `next_action`.
- Dates only worked because `applied` was renamed to `date`.
- `list_docs` returned `date` for some files and `modified` for others.
- A manual commit left lock files behind.

Goal: fix all five without weakening path confinement, `templates/` read-only, no-delete, or the tests-against-a-temp-copy rule.

Facts from the code that shape the plan:
- **Change 5 is mostly there already.** A failed commit is a warning, not a failed write, and the server never deletes a lock. `TestGitIndexLock` (`internal/write/git_test.go:362`) proves both. What is missing: a message that names the lock and says how to clear it safely, and coverage for `HEAD.lock`. Today the warning is git's raw output plus a generic "fix the cause".
- **`summary_fields` already exists** as a top-level config key (DESIGN "Summary fields come from config"). `show` builds on it.
- **Dates come from one place.** `parseDoc` (`internal/read/scan.go:150`) reads the field `date` and sets `Doc.Date`/`DateFromMtime`. `since`/`until`, sorting, "recent" and the tag stats all read `Doc.Date`, so `date_field` is a scan-time change only.
- **The write tools already split into transform + atomic write.** That is `modify`'s transform closures in `writer.go`, plus `createFile`/`replaceFile` with their rename-time checks in `atomic.go`. The `beforeRename` seam (`atomic.go:22`) already lets a test fail a write midway.
- **The only `___` placeholder** is DESIGN's "What changed after using it for N weeks". The SDK choice is already recorded in DESIGN ("MCP SDK choice", go-sdk v1.8.0), so that needs nothing.

## Open questions

You answered these four:
1. **The same path more than once in a batch: allowed.** Ops run in order. Each file is written once, with its final bytes.
2. **`show` vs `summary_fields`: `show` overrides.** A type's `show` is used as is (`show: []` hides everything). Types without `show` fall back to `summary_fields`.
3. **`get_context` lines for undated docs** show `date=2026-01-10 date_source=mtime`. Lines with a date field keep `date=…` only.
4. **Batch ops are `{tool, args}`.** `args` are exactly that tool's arguments, decoded strictly.

Defaults I took. Say if any is wrong:
5. **`{date}` in filename patterns reads the type's `date_field`.** Otherwise a type with `date_field: applied` and `{date}-{slug}.md` would quietly name the file with today's date.
6. **Single write tools become batches of one,** running through the same engine. Arguments, results, error texts and commit messages are unchanged. This gives one code path, and the ~40 existing write/seam/git tests then guard the batch engine.
7. **Rollback leaves empty folders behind.** These are folders that `create_doc` made for files the rollback removes, the same as a failed `create_doc` today. This is recorded as a cost.
8. **A per-op `hash` is the path's hash on disk after the batch.** For a path touched twice, both ops report the final hash.
9. **Batch commit message:** your `message` (trimmed) if given, else `batch: 2 create_doc, 9 update_frontmatter`, counting per tool with tools sorted by name.
10. **Lock detection parses git's own message** (`Unable to create '<path>.lock': File exists`, stable under `LC_ALL=C`). It does not stat fixed paths. This names whichever lock git hit (`index.lock`, `HEAD.lock`, `refs/heads/main.lock`) and works when `.git` is a file.
11. **Lock in `get_context` Problems: yes (your call).** At session start, check `index.lock` and `HEAD.lock` under the git dir. Warn only, never delete. **Not doing:** `date` on `search_text` hits.

## Workflow (same as before)

For each milestone:
1. Write the failing tests and stop so you can review them.
2. Implement, then run `go test -race ./...`, `go vet ./...` and `gofmt -l .`.
3. Regenerate the golden files with `-update` and show you the diff.
4. Stop with the hand-check prompt.

On approval I copy this plan to `PLAN.md`, as for the first build, and remove it after M5. Every test runs on `testvault.Copy`. No domain words go in Go code (`TestNoDomainWords`).

Since M1 and M2 change `examples/vault`, re-copy it to `~/prep-demo` before the hand checks (README "Try it on the example vault").

---

## M1: changes 4 and 3 (date_source, date_field)

**Example data.**
- `applications/acme-backend.md` gets `applied: 2026-01-05`.
- `prep.config.yaml` gets `date_field: applied` under `application`.

**Tests first**
- `internal/config/config_test.go`:
  - `date_field` is parsed.
  - It defaults to `date`.
  - `Config.DateField(typ)` returns `date` for an unconfigured type.
- `internal/read/testdata/scan.yaml` + `scan_test.go`:
  - The application is dated 2026-01-05 from `applied`.
  - A doc of that type with a `date` key but no `applied` → mtime.
  - An unparseable configured field → a problem that names the field.
- `internal/read/testdata/queries.yaml`:
  - `since`/`until` and `date_asc`/`date_desc` cases that hinge on the application's `applied` date.
  - Update `all` and the other orderings that move.
- `internal/write/filename_test.go`: `{date}` uses the given date field.
- `internal/server/readtools_test.go`:
  - Every `list_docs` entry has `date` and `date_source` (`field`|`mtime`).
  - The raw JSON never contains `"modified"`.
- `internal/read/testdata/context.golden` and `context-noweek.golden`: undated lines gain `date=2026-01-10 date_source=mtime`.

**Code**
- `internal/config/config.go`:
  - `TypeSpec.DateField` (`yaml:"date_field"`), defaulted in `Parse` (`cmp.Or`).
  - `func (c *Config) DateField(typ string) string`.
- `internal/read/scan.go`:
  - `parseDoc` takes the date field name (`r.cfg.DateField(typ)`).
  - The problem text names the field.
- `internal/read/context.go` `metaLine`: always prints `date=`, plus `date_source=mtime` when the date is the mtime.
- `internal/server/readtools.go`:
  - `docMeta` has `Date` and `DateSource` (no omitempty), and `Modified` is gone.
  - The `listDocsDescription` text says since/until use each type's date field, else mtime.
- `internal/write/filename.go` + `writer.go`: `RenderFilename` gets the type's date field for `{date}`, and the create_doc description says "the type's date field".

**Hand check (Claude Desktop):**
> Using prep: list applications applied since 2026-01-01, then list every improvement with its date and say where each date comes from.

Expect `acme-backend` with `date: 2026-01-05` and `date_source: field`. Expect the improvements with `date_source: mtime`.

## M2: change 2 (show / fields)

**Example data.**
- `application` gets `show: [stage, status, next_action, next_action_date]`.
- `acme-backend.md` gets `next_action_date: 2026-01-16`.

**Tests first**
- Config: `show` is parsed. Validation reuses the `summary_fields` rules, generalised to `fieldListErrors(label, list)`: empty name, an always-shown field, the type's own `date_field`, a repeat. `Config.ShowFields(typ)` returns `show` when it is non-nil, else `summary_fields`.
- Context:
  - The application line shows `stage=interview status=active next_action=Send thank-you note next_action_date=2026-01-16`, in `show` order.
  - Other types still use `summary_fields`.
  - The golden files are regenerated.
- `list_docs`:
  - `fields` comes from each doc's type.
  - `fields: ["next_action"]` overrides for every doc.
  - `fields: []` returns none.
  - The description names the per-type `show` lists and the `fields` argument.

**Code**
- `config.go`: add `TypeSpec.Show` and `ShowFields`, and generalise `summaryErrors`.
- `context.go`: `metaLine(d, r.cfg.ShowFields(d.Type))`.
- `readtools.go`:
  - `listDocsIn.Fields []string`: nil means config, non-nil overrides.
  - `metaOf` takes the resolved list.
  - Update `listDocsDescription`.

**Hand check:**
> What's on for the week of 2026-01-14? Then list active applications showing only next_action and next_action_date.

Expect the next_action line in Open items. Expect the second answer to show only those two fields.

## M3a: change 1, the engine (no MCP surface yet)

**Engine (`internal/write`)**
- `Op` holds the tool name plus that tool's arguments: `Draft`, `Path`, `Heading`, `Content`, `Patch`, `ExpectedHash`. It also has `Err error`, for an op the server could not decode, so decode errors are reported with all the others.
- Each tool's checks and byte transform are extracted, unchanged, into a plan step that takes the target's current bytes and returns the next bytes plus warnings:
  - `Create`'s body.
  - The two `modify` closures.
  - `Replace`'s hash and frontmatter checks.
- `Writer.Batch(ctx, ops, message) (*BatchResult, error)`, holding `w.mu` throughout:
  1. **Cap.** 0 ops → "pass at least one op". More than 200 → "N ops; at most 200 per call, split into batches (each is one commit)".
  2. **Plan everything.** An in-memory view (`rel → orig, existed, cur`) is read lazily, once per file. Ops run in order against it.
     - A failing op is recorded as `op 3 (update_frontmatter improvements/x.md): <reason>`, leaves the view untouched, and checking continues.
     - Path safety goes through `Resolve(AccessWrite)`, so `templates/`, `.git` and escapes are refused.
     - `create_doc` on a path that exists, on disk or earlier in the batch, fails.
     - `replace_doc` after another op on its path fails with "put replace_doc first for <path>, or fold the change into its content".
     - Any failure: return them all, joined under "nothing was written".
  3. **Apply.** Paths are written in first-touch order, skipping a path whose final bytes equal the original. A new file goes through `createFile`, an existing one through `replaceFile(rel, cur, hash(orig))`; both are the existing rename-time checks.
  4. **Roll back on failure,** in reverse order:
     - A replaced file goes back through `replaceFile(rel, orig, hash(cur))`. That restores the exact bytes and keeps the mode. It refuses if someone saved the file after we wrote it; that file is left alone and named in the error.
     - A created file is removed only if it still has our hash.
     - If the failure was a `*ConflictError` (a human saved between plan and rename), re-plan the whole batch once from disk, as `modify` does today. A second conflict → "changed again, twice in a row; nothing was written".
     - Any other failure → the error names the op, says nothing was written, and names any file it could not restore, with the reason.
  5. **One commit** for every written path.
  6. **Result:** `{"results":[{path,hash,warnings}…], "commit", "warnings"}`, with the commit warnings at the top level.
- `Create`, `AppendSection`, `UpdateFrontmatter` and `Replace` become one-op calls into the engine. The message stays `<tool>: <path>`, the error is returned unwrapped, and `modify` is deleted.
- `git.go`:
  - `Commit(ctx, dir string, paths []string, msg string)`: `add`, `diff --cached --quiet` and `commit --only` take all paths.
  - The fix-it hint lists up to 5 paths, then "and N more".

**Tests first**
- `internal/write/writer_test.go` (new `TestBatch*`):
  - Invalid ops among valid ones: 6 ops, 3 bad (`../x.md`, `templates/x.md`, a missing required field). The error names ops 2, 4 and 6 with their reasons, the vault snapshot is unchanged (`snapshot`/`sameSnapshot`), and there are no leftovers.
  - An op that arrives with `Err` set is reported in the same list as the plan failures.
  - A stale `replace_doc` hash rejects the whole batch: the error carries the current hash, and the earlier valid op's file is untouched.
  - Same path in order:
    - `update_frontmatter` + `append_section` on one file → both changes, one write, and both results report the file's hash.
    - `create_doc` + `append_section` on the new path works.
    - `replace_doc` second on a path → the "put first" error.
  - Limits: 0 and 201 ops are rejected with the limit named. 200 ops are accepted.
- `internal/write/seam_test.go` (white box):
  - Forced failure midway. Ops: update A, create B, append C, update D, with the 3rd rename failing. Afterwards A is byte-identical with the same mode, B is gone, C and D are untouched, there is no `.prep-tmp-*`, and the error says nothing was written.
  - A human saves A between our write and the rollback → A keeps the human's bytes, and the error names A.
  - A human saves during apply → rollback, one re-plan, the human edit is kept, and the batch succeeds.
- `internal/write/git_test.go`:
  - 3 `update_frontmatter` ops → exactly one new commit, author and committer `Claude <claude@local>`, message `batch: 3 update_frontmatter`, all 3 paths in it.
  - A custom `message` is used verbatim.
  - `wantCommitted` is generalised to take the commit hash.
- The existing single-tool tests stay unchanged and must pass. They are the regression net for the refactor.

**Stop for review** of the engine before any MCP code.

**Hand check (regression of the refactor; the single tools now run through the engine):**
> Mark improvements/buries-headline.md as improving, then add "- Fri: mock, headline first" to this week's Log.

Expect the same results as before, and two commits named `update_frontmatter: …` and `append_section: …`.

## M3b: change 1, the batch MCP tool

**Server (`internal/server/writetools.go`)**
- `batch` tool: `batchIn{Ops []batchOpIn; Message string}` with `batchOpIn{Tool string; Args map[string]any}`.
- The handler re-reads the raw arguments, as `rawArgs` does today, so frontmatter key order survives.
- Each op's `args` are decoded with `DisallowUnknownFields` into the existing `createDocIn`/`appendSectionIn`/`updateFrontmatterIn`/`replaceDocIn`, and converted by the same helper the single tools use.
- An unknown tool → an op error listing the four valid ones.
- Annotations: destructive, since it can replace.
- The description says when to use it: imports and multi-file changes, all or nothing, one commit, ops in order, at most 200. It also says to use it instead of editing vault files with other tools.
- The server `instructions` mention it.

**Tests first** (`internal/server`)
- `TestToolSet` gains `batch`.
- Strict decoding (an unknown tool, an unknown arg key).
- Frontmatter key order is kept through `batch`.
- A decode error and a plan error come back in one response.
- The result JSON shape.
- `TestDescriptionsAreDomainNeutral` and `TestTextSteersWritesToTheVault` cover the new text automatically.

**Hand check** (on a fresh `~/prep-demo` copy, since M3a's check already set improving):
> In one batch: set status: improving on improvements/buries-headline.md, set stage: offer on applications/acme-backend.md, and append "- 2026-01-16 offer call" to its Timeline section.

Then `git -C ~/prep-demo log -1 --stat`: one commit by Claude, 2 files.

Then:
> In one batch, set status: closed on improvements/buries-headline.md and on improvements/nope.md.

Expect an error naming op 2 and nothing changed.

## M4: change 5 (stale git lock)

The two locks fail at different steps, and the text says which:
- **`index.lock`:** `git add` fails, so nothing is staged.
- **`HEAD.lock`:** `git add` succeeds and `git commit` fails, so the change stays staged.

The wording is keyed on the step that failed (`git add` vs `git commit`), not on the lock's name. That way a branch-ref lock (`refs/heads/main.lock`), which also fails at commit, is described correctly too.

**Tests first**
- `git_test.go`: `TestGitIndexLock` becomes a table over `index.lock` and `HEAD.lock`. For each:
  - The write is kept, and there is no new commit.
  - The warning names the full lock path and says to delete it once no other git process is running.
  - The warning says "nothing was staged" (`index.lock`) or "the change is staged" (`HEAD.lock`). `git diff --cached --name-only` agrees.
  - The lock still exists afterwards.
- `git_test.go`, one extra case: a batch with `index.lock` present → all files written, one warning, the lock untouched.
- `internal/read/context_test.go`:
  - `.git/index.lock` and/or `.git/HEAD.lock` in the temp copy → a `Problems` entry per lock: the path, what it blocks, and "delete it once no git process is running; prep-mcp never deletes it". The lock still exists after the call.
  - `.git` as a file with `gitdir: <dir>` → the lock is found under that dir.
  - `commit_on_write: false`, or no `.git` → nothing flagged.
  - The golden files are unchanged, since the example has no `.git`.

**Code**
- `git.go` `problem()`: if git's output matches `Unable to create '([^']+\.lock)': File exists`, the warning reads:
  > not committed: git's lock file <path> exists. Another git process may be running, or one crashed and left it. The file is saved; {nothing was staged | the change is staged}. Once no git process is running in <dir>, delete <path>, then run `git add -- … && git commit --only -- …`. prep-mcp never deletes it.
- Otherwise the warning is unchanged.
- `internal/read/context.go`, when `commit_on_write` is true:
  - Find the git dir: `<root>/.git` if it is a directory, or the `gitdir:` target if `.git` is a file.
  - `os.Stat` `index.lock` and `HEAD.lock` there.
  - Add each lock found to `problems`.
  - Read only. No git binary, nothing deleted. Only where the vault is the repo root, the same rule as commits.

**Hand check:**
- In a terminal: `touch ~/prep-demo/.git/index.lock`.
- Prompt:
  > What's on for the week of 2026-01-14?

  Expect `Problems` to name the lock.
- Prompt:
  > Set status: open on improvements/buries-headline.md.

  Expect success with a warning naming the lock and "nothing was staged", and `ls ~/prep-demo/.git/index.lock` still there.
- Then `rm` the lock and repeat. Expect a commit.

## M5: docs

**PRD.md**
- §4.3: `date_field` and `show` in the example and text, with `summary_fields` as the fallback.
- §5.1 `list_docs`:
  - The `fields` argument.
  - The result carries `date` + `date_source`.
  - since/until/sort use the type's date field.
- §5.2: `batch`, its rules and result shape.
- §5.1 `get_context`: Problems also lists git lock files.
- §5.3: one commit per batch, and the lock warning (staged or not).
- §8: the new tests.

**README.md**
- Tool table: add `batch`, and `fields` on `list_docs`.
- The config example gets `show`/`date_field`.
- The `list_docs` JSON and the `get_context` excerpt are refreshed from the real output.
- A batch example with its single `git log` line.

**DESIGN.md**
- The header sentence "PRD.md itself is left as written" is updated: PRD was revised on 2026-10-02 for these changes.
- New entries (decision / instead of / why / cost):
  - **Batch writes, all or nothing.**
    - Instead of: one commit per call, or a write-ahead journal.
    - Cost:
      - Rollback is best effort, not a transaction. A crash or kill mid-apply leaves a partial batch with no record (`git status` shows it).
      - A file a human saved after our write is not rolled back, and the error names it.
      - A restore that fails is reported, not retried.
      - Restored files get a new inode and mtime, so Obsidian sees a change.
      - Empty folders made for new files stay.
      - The whole batch is in memory, hence the 200 cap.
      - One revert undoes the whole batch.
  - **Single writes are batches of one.**
  - **Per-type `show`** (amends "Summary fields come from config").
  - **Per-type `date_field`**, which `{date}` follows.
  - **`date` + `date_source` everywhere.** This supersedes "list_docs returns date or modified" and amends the `get_context` format entry. Cost: the `modified` key is gone, and undated context lines get longer.
  - **Lock: warn, never delete; flag it in `get_context` too.**
    - Instead of: deleting stale locks.
    - Why: a live lock can't be told from a stale one, and deleting a running git's lock can corrupt the index.
    - Cost: until the human removes the lock, every AI write is saved but not committed.
      - With `index.lock`, `git add` fails, so nothing is staged and the writes are plain working-tree changes.
      - With `HEAD.lock`, each write is staged. The next successful AI commit of the same path picks it up; other paths stay staged.
      - Either way, writes the human commits by hand lose the AI author.
      - Detection on write depends on git's English message, pinned by `LC_ALL=C` and the test.
      - The session-start check covers only these two lock names.
    - This also refines the existing "A failed commit leaves the path staged" note in "How the git step behaves": that holds only when `git add` got through.
- **"What changed after using it for N weeks",** with a first entry, 2026-10-02 (first day, 100-file vault):

  | Real use exposed | Answered by |
  |---|---|
  | The 60-file import bypassed the server | batch |
  | Invalid YAML was caught and reported under Problems (worked as designed) | — |
  | The `templates/` guard refused a write (worked as designed) | — |
  | One commit per write | batch |
  | Missing `next_action` | show |
  | The `applied`→`date` rename | date_field |
  | Mixed `date`/`modified` | date_source |
  | A leftover lock | lock warning + `get_context` Problems |

- **Scan time on examples/vault:** add a short `BenchmarkScanExample` (a `testvault.Copy` + `Scan`) next to `BenchmarkScan`, run it, and record the number in "Scan on every call". The SDK choice is already recorded, so nothing to fill there.

**Hand check:**
> What tools does prep have, and when should you use batch instead of single writes?

The answer should match the README table.

---

## Risks

- **Rollback correctness.**
  - Every restore uses the same atomic replace as a normal write: exact bytes, mode kept. It is guarded by our own hash, so it can never clobber a human save.
  - Rollback runs in reverse order.
  - Seam tests cover a failure midway, a human save during rollback, and a human save during apply.
  - The single tools run through the same engine, so the existing write tests exercise it too.
  - Residual risk: a process crash mid-apply, with no journal, by choice.
- **YAML key order.**
  - The config is only decoded, never written, so key order in `prep.config.yaml` doesn't matter. The order inside `show` is the display order in `get_context`.
  - `list_docs` `fields` is a JSON object, which `encoding/json` writes with sorted keys. Accepted.
  - Frontmatter order through `batch` comes from the raw JSON via `JSONToNode`, as today; a test pins it.
  - Chained ops on one file go through the line-splice `ApplyPatch`, so untouched keys stay byte-identical.
- **Backward compatibility.**
  - No `date_field` → `date`. No `show` → `summary_fields`.
  - Single-tool arguments, results and commit messages are unchanged.
  - Breaking: `list_docs` drops `modified`; only the model reads it.
  - `KnownFields(true)` means an *older* binary refuses a config that uses `show`/`date_field`, so install the new binary before editing the real config.
- **Refactor risk** from routing the single tools through the engine: the existing tests must pass unchanged. Any edit to an existing assertion is flagged to you, not done quietly.

## Verification

- `go test -race ./...`, `go vet ./...`, `gofmt -l .`, with `TestNoDomainWords` green.
- Golden diffs reviewed by you, not only regenerated.
- The hand-check prompts (M1, M2, M3a, M3b, M4, M5) in Claude Desktop against a fresh `~/prep-demo` copy (Desktop config `--root /Users/you/prep-demo`), after `go install ./cmd/prep-mcp` and ⌘Q/reopen.
