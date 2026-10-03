# Commit only when the vault is its git repo's root

Instead of committing whenever the vault is anywhere inside a work tree. The real vault is the root of its own private repo, while `examples/vault` sits inside this public repo and must never receive AI commits. A vault that is not a repo root gets the write plus a warning saying how to fix it. A commit covers only the written paths (`git commit --only`), so a human's staged work is not swept in. Git failures are warnings, never failed writes, and nothing is pushed.

Cost: a vault in a subfolder of a bigger repo gets no commits. An uncommitted human edit to a written file goes into the AI's commit, since a commit covers the whole file.
