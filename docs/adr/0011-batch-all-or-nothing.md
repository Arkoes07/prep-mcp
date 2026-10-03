# Batch writes: all or nothing, one commit

On the first day of real use, a 60-file import went around the server (no checks, no AI author) and nine frontmatter edits made nine commits. `batch` checks every op first, against the vault as the earlier ops leave it, and writes nothing if any fails. Then it writes each changed file once, puts back what it wrote if a write fails, and makes one commit. At most 200 ops. Every write tool runs through the same engine as a batch of one.

Rejected: a write-ahead journal that could finish or undo a batch after a crash, at the price of a recovery step at every start.

Cost: atomic per file, not per batch: a crash mid-write leaves a partial batch, which `git status` shows. A put-back is guarded by hash, so a file a human saved after the batch wrote it is kept and named in the error, not rolled back. One commit means one `git revert` undoes the whole batch.
