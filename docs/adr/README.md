# Architecture decisions

One file per decision, `NNNN-slug.md`; a new decision takes the next number. Each records the decision, what it replaced, why, and what it costs. PRD section 6 is the starting list. Titles marked *(deviation)* differ from PRD.md. PRD.md was left as written until 2026-10-02, when sections 4.3, 5 and 8 were updated for the changes listed under "What changed after using it for N weeks" below; the deviations recorded before that date still stand.

## What changed after using it for N weeks

### 2026-10-02: first day, a vault of about 100 files

| Real use exposed | Answered by |
|---|---|
| A 60-file import went around the server with shell and file tools, so it skipped the path, required-field and tag checks, and its commit had no AI author. | [`batch`](0039-batch-writes-all-or-nothing-one-commit.md): up to 200 checked writes in one call, all or nothing, one commit by the AI author. |
| Nine frontmatter edits made nine commits. | [`batch`](0039-batch-writes-all-or-nothing-one-commit.md). |
| A file with invalid YAML was caught and listed under Problems, and every other document was still served. | Nothing: it worked as designed ([ADR-0034](0034-broken-files-are-reported-not-hidden.md)). |
| A write into `templates/` was refused. | Nothing: the read-only guard worked as designed. |
| `get_context` listed applications without `next_action`, so a due follow-up did not show. | [Per-type `show`](0041-per-type-show-fields.md). |
| Applications could only be filtered by date after renaming `applied` to `date`. | [Per-type `date_field`](0042-per-type-date-field.md). |
| Some list results had `date` and others `modified`, depending on the file. | [`date` and `date_source` in every result](0043-date-and-date-source-in-every-list-result.md). |
| A manual commit left git lock files that would have blocked every later AI commit. | [A lock warning that names the lock and never deletes it, and a check in `get_context`](0044-a-git-lock-warn-never-delete-flag-it.md). |

Measured the same day: a scan of `examples/vault` takes 0.27 ms; 2,000 files take 59 ms, so the real vault is near 3 ms ([ADR-0001](0001-scan-on-every-call-no-persisted-index.md)).
