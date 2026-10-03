# Path confinement in two layers

A check of the path text (`vault.Resolve`) gives clear errors: absolute paths, `..`, `\`, NUL, dot components, non-`.md` files and, for writes, `templates/`. All I/O then goes through `os.Root`, which the kernel confines, closing the race between check and use that a string check alone leaves open. Writes refuse any symlink in the path, reads follow symlinks that stay inside the vault, and the scanner never follows them.

Cost: two layers to keep consistent; tests cover both.
