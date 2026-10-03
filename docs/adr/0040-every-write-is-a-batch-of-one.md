# Every write is a batch of one (2026-10-02)
- Instead of: keeping a code path per write tool next to the batch engine.
- How: `Writer.Apply(ctx, op)` runs one op, `Writer.Batch(ctx, ops, message)` several; `CreateOp`, `AppendOp`, `PatchOp` and `ReplaceOp` describe every write, each carrying the function that applies it to the plan. In the server, a write tool's arguments and a batch op's go through the same decoder (`buildOp`).
- Why: one set of guarantees and one set of tests. The existing write, seam and git tests passed through the engine unchanged and now guard it too. Single tools keep their own error texts (unlabelled) and commit messages (`<tool>: <path>`).
- Cost: a single write builds a one-op plan it does not need. A `replace_doc` whose content equals the file is now a no-op rather than a rewrite of the same bytes; there was no commit either way.
