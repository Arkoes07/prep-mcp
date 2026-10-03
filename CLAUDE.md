# Working on prep-mcp

- No new dependencies without asking.
- Write tests alongside the code, not after. Tests run against a temp copy of `examples/vault`, never against a real vault.
- Nothing in the Go code may mention interviews, stories, sessions or applications. Those words live only in config and example data.
- Code says what. A short why is a comment where the code does it. A why that is long, or a decision that is hard to reverse, surprising and a real trade-off, is an ADR in `docs/adr/`.
- Error messages must say what to do next.
- Do not add tools without asking.

## Agent skills

### Issue tracker

Issues live as local markdown under `.scratch/<feature>/`. See `docs/agents/issue-tracker.md`.

### Triage labels

The five default roles: needs-triage, needs-info, ready-for-agent, ready-for-human, wontfix. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` at the root, decisions in `docs/adr/`. See `docs/agents/domain.md`.
