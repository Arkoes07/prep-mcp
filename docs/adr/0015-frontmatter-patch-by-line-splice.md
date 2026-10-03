# Frontmatter patch by line splice
- Instead of: decoding the frontmatter into `yaml.Node`, changing it, and re-encoding all of it.
- Why: re-encoding collapses the aligned `# a | b` comments the templates produce, so every edit to a real file would show a noisy diff. `yaml.Node` is used to find each top-level key's line span. Only changed keys are rewritten; each keeps its line comment and flow or block style. New keys go before the closing fence. The body is copied byte-for-byte.
- Safety net: before writing, re-parse and check that untouched keys are unchanged and the body bytes are identical. If not, abort with an error.
- Cost: more code than a round-trip, and multi-line flow values at column 0 are an edge case.
