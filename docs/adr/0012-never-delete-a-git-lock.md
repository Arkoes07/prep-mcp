# Never delete a git lock

When git reports `index.lock` or `HEAD.lock`, the write's warning names the lock, says whether the change was staged, and gives the command that commits it afterwards with the AI author; `get_context` lists the locks under Problems. Neither deletes them: a live lock cannot be told from a stale one, and deleting the lock of a running git (a GUI, the Obsidian Git plugin, an open commit editor) can corrupt the index.

Cost: until the human removes the lock, every AI write is saved but not committed, and writes the human commits without that command lose the AI author.
