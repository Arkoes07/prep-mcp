# Working on prep-mcp

- No new dependencies without asking.
- Write tests alongside the code, not after. Tests run against a temp copy of `examples/vault`, never against a real vault.
- Nothing in the Go code may mention interviews, stories, sessions or applications. Those words live only in config and example data.
- Code says what. A why is a comment where the code makes the decision. `DESIGN.md` holds what no single place in the code can: how the packages fit together, and the whys behind boundaries or things left out on purpose. Keep its overview at package level.
- Error messages must say what to do next.
- Do not add tools without asking.

## Agent skills

- Issues: GitHub Issues on `Arkoes07/prep-mcp`, via `gh`. Close done work with `gh issue close <n> --comment "Done in <sha>"`.
- Domain docs: single-context. `GLOSSARY.md` at the root; decisions are sections in `DESIGN.md`, and this repo keeps no `docs/adr/`.
