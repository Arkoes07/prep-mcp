# Atomic writes (temp file + rename)
- Instead of: writing the target file in place.
- Why: a crash never leaves a half-written note. A hard link to a file outside the vault is replaced rather than written through. The mode of an existing file is kept.
- Also decided: the temp file is created 0600 and chmod-ed to its final mode before the rename (an existing file keeps its mode; a new file gets 0644), so it is never briefly more open than the result. Its name is `.prep-tmp-<random>`.
- Cost: the file's inode changes on every write, and a dot-prefixed temp file briefly exists next to the target.
