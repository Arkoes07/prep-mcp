# Working on prep-mcp

- No new dependencies without asking.
- Write tests alongside the code, not after. Tests run against a temp copy of `examples/vault`, never against a real vault.
- Nothing in the Go code may mention interviews, stories, sessions or applications. Those words live only in config and example data.
- Code says what. A why is a comment where the code makes the decision. `DESIGN.md` holds what no single place in the code can: how the packages fit together, and the whys behind boundaries or things left out on purpose. Keep its overview at package level.
- Error messages must say what to do next.
- Do not add tools without asking.

## Go development

Before any Go coding, review, debugging, troubleshooting, or setup task, load the `samber/cc-skills-golang@golang-how-to` skill first — it routes to whichever other Go skills the task needs.

### Code style

These override `golang-code-style` and `golang-naming` where they differ.

- A call or signature stays on one line up to 120 columns, counting a tab as 4. Past that, one argument per line.
- Past 120, a `slog` call keeps `ctx` and the message on its first line, then one key/value pair per line. A query call keeps the arguments before the SQL on its first line, then gives the SQL and each later argument a line of its own.
- A function that works with several kinds of things is split into methods. Any other long function gets a blank line between its step groups.
- Name a function or method with a verb for what it does: `renderAck`, not `ackText`.

## Agent skills

- Issues: GitHub Issues on `Arkoes07/prep-mcp`, via `gh`. Close done work with `gh issue close <n> --comment "Done in <sha>"`.
- Domain docs: single-context. `GLOSSARY.md` at the root; decisions are sections in `DESIGN.md`, and this repo keeps no `docs/adr/`.
