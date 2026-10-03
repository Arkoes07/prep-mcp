# Partial writes, and a hash-guarded full rewrite

A human edits the same files in Obsidian. So the write tools are append-to-section and frontmatter patch, which cannot clobber prose, plus `replace_doc`, which refuses unless the file still has the hash the caller read. Last write wins would silently lose a human's edit.

Cost: four write tools instead of one, and after a conflict the model re-reads and retries.
