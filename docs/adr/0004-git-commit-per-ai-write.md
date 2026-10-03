# Git commit per AI write
- Why: every AI change is reviewable (`git log --author=Claude`) and revertible.
- Cost: noisy history. Since 2026-10-02 a change to several documents can be one `batch` and one commit (see [ADR-0039](0039-batch-writes-all-or-nothing-one-commit.md)).
