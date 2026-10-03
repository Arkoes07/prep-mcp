# No domain words in any Go file, tests included
- Instead of: applying CLAUDE.md's rule to non-test code only.
- Why: Acceptance 7 says "nothing in the Go code". Expectations that name example-specific types or paths live in `testdata/*.yaml` and golden files, which are data. `TestNoDomainWords` enforces the rule using a word list kept in `internal/testvault/testdata`. Tool descriptions avoid the words too, which `TestDescriptionsAreDomainNeutral` checks with the same list through `testvault.ForbiddenWords`.
- Cost: example-specific assertions are one step removed from the test code.
