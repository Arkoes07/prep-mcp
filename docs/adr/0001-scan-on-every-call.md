# Scan files on every call, no index

The server writes, so an index built at startup is stale the moment the AI logs something, and a stored index (of documents or tags) is a second copy of the frontmatter that can drift. Every call scans the vault instead.

Cost: a full scan per call. Measured at 59 ms for 2,000 files and 0.27 ms for `examples/vault` (Apple M4, `go test -bench Scan -run '^$' ./internal/read`); a vault of about 100 files is near 3 ms. Add an mtime-keyed cache only if a real vault measures slow.
