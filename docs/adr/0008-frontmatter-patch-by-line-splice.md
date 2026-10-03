# Frontmatter patch by line splice

Re-encoding the whole frontmatter through `yaml.Node` collapses the aligned `# a | b` comments the templates produce, so every edit would show a noisy diff. Instead `yaml.Node` only finds each top-level key's lines: a changed key is rewritten in place with its line comment and style, a new key goes before the closing fence, and the body is copied byte for byte. Before writing, the result is re-parsed; if an untouched key or the body changed, the write aborts.

Cost: more code than a round-trip, and multi-line flow values at column 0 are an edge case.
