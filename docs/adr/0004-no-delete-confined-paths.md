# No delete tool, paths confined to the vault

A server that writes files is a prompt-injection target: a document can tell the model to delete or overwrite things. So there is no delete tool, and no path can leave the vault or be written in `.git` or `templates/`.

Cost: deletion is manual.
