# A nil `Git` means "do not commit, do not warn" (M2)
- Instead of: always passing a `Git` and letting it decide.
- Why: `commit_on_write` is true in the example config. Once M3 gives `Git` real behaviour, a non-repo vault gets a "not committed" warning on every write, which is right for the server but would make every "no warnings" assertion in the write tests depend on git state. Tests pass `nil`; `main` passes a real `Git`.
- Cost: two ways to be "off" (a nil `Git` and `commit_on_write: false`).
