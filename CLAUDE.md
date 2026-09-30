# Building prep-mcp

Read PRD.md first. It is the spec. This file is how to work on it.

- Go, standard layout from PRD section 7. Keep dependencies to the ones listed.
- Plan the build before coding. Ship read tools first so they can be used read-only before writes exist.
- Write the tests in PRD section 8 alongside the code, not after. Tests run against a temp copy of `examples/vault`, never against a real vault.
- Nothing in the Go code may mention interviews, stories, sessions or applications. Those words live only in config and example data.
- Every design decision that differs from the PRD goes into DESIGN.md with the alternative and the cost.
- Error messages must say what to do next.
- Do not add tools beyond PRD section 5 without asking.
