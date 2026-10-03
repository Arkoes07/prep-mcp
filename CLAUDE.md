# Building prep-mcp

Read PRD.md first. It is the spec. This file is how to work on it.

- Go, standard layout from PRD section 7. Keep dependencies to the ones listed.
- Plan the build before coding. Ship read tools first so they can be used read-only before writes exist.
- Write the tests in PRD section 8 alongside the code, not after. Tests run against a temp copy of `examples/vault`, never against a real vault.
- Nothing in the Go code may mention interviews, stories, sessions or applications. Those words live only in config and example data.
- Every design decision that differs from the PRD goes into a new ADR in `docs/adr/` with the alternative and the cost.
- Error messages must say what to do next.
- Do not add tools beyond PRD section 5 without asking.

## Agent skills

### Issue tracker

Issues live as local markdown under `.scratch/<feature>/`. See `docs/agents/issue-tracker.md`.

### Triage labels

The five default roles: needs-triage, needs-info, ready-for-agent, ready-for-human, wontfix. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` at the root, decisions in `docs/adr/`. See `docs/agents/domain.md`.
