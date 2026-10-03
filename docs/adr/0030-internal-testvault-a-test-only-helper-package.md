# `internal/testvault`, a test-only helper package *(deviation from PRD section 7 layout)*
- Instead of: repeating copy-and-setup code in each package's tests.
- Why: every test uses a temp copy of `examples/vault` with fixed mtimes (git doesn't preserve them) and a frozen clock, so golden output is deterministic. The git tests also cut out your git config and environment, but that is `isolateGit` in `internal/write/git_test.go`, not this package: only they run git.
- It also owns the forbidden word list and its matcher (`ForbiddenWords`), because two packages check against it. Instead of: each test reading `cmd/prep-mcp/testdata` with its own parser, which let the two checks drift apart.
- Cost: one extra package outside the PRD layout.
