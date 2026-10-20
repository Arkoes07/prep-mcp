# Design

How the code fits together, then the reasons behind decisions no single file holds. Each package's doc comment has its detail. Terms are defined in [GLOSSARY.md](GLOSSARY.md).

## Overview

```text
cmd/prep-mcp     reads flags, loads the config, opens the vault, serves MCP over stdio
     │
server           the MCP tools: decodes arguments, renders results; glue only
     ├── read    queries: scans the vault, filters, searches, builds get_context
     └── write   changes: plans ops, checks the document rules, writes files, commits
           │
document         a file read as a document: type, title, tags, date, and the rules it breaks
vault            confined file access (os.Root), frontmatter, where conditions and tags
config           prep.config.yaml: types and their filename patterns, fields shown, get_context queries
```

**A read** scans every typed document (`Reader.Scan`), filters, searches or groups them, and `server` renders the answer as text or JSON.

**A write** takes one path for every write tool. `server` turns each call into `write.Op`s: one from a write tool, up to 200 from `batch`. `Writer.Apply` and `Writer.Batch` run the same engine: it works out each op's new content against the file as the earlier ops leave it, and checks it against the document rules. If any op fails, nothing is written. Otherwise each changed file is written once, through a temp file and a rename, and `Git.Commit` makes one commit.

## Decisions

### A human edits the same files

The vault is also open in Obsidian, and last write wins would silently lose a human's edit. So each write tool changes only what it names:

- `create_doc` only makes new files. `append_section` and `update_frontmatter` add or patch, leaving existing prose alone.
- `edit_text` changes one exact piece of text, which must match once. The text is its guard: if a human changed it, nothing matches and nothing is written. It takes no hash, so a small fix needs no read first, and an edit elsewhere in the file does not block it.
- `replace_doc`, the only full rewrite, refuses unless the file still has the hash the caller read.

Cost: five write tools instead of one, a re-read and retry after a conflict, and an `edit_text` can land next to a human change the model has not seen.

### No delete, paths confined

A server that writes files is a prompt-injection target: a document can tell the model to delete or overwrite things. So there is no delete tool, and no path leaves the vault or is written in `.git`.

Templates and `CLAUDE.md` can be written, though both steer what the model writes next. Keeping them current is worth more than the guard, and each write is a commit to review or revert.

Cost: deleting and moving files is manual, and an injected change to a template or `CLAUDE.md` shapes later writes until a human reverts it.

### Few tools that combine

Models pick tools better from a small set, and filters compose. So `list_docs` takes type, tags, `where` and dates together, instead of one tool per filter.

Cost: more complex parameters.

### Locks are reported by the write that hits them

A git lock left behind stops every commit. Only the commit reports it: git names the lock in its error, so the warning is right for any lock (the index, HEAD, a branch ref), and no copy of git's lock rules lives here. `get_context` does not look for locks: a copy drifts from git's rules, and looking would reach into `.git` around the vault's confined root.

Cost: a stale lock shows up at the first write of a conversation, not at its start. The file is still saved, and the warning names the lock and the command to commit by hand.
