# prep-mcp

An MCP server that lets an AI assistant read and safely write a vault of markdown documents with YAML frontmatter, while a human edits the same files.

## Language

**Document**:
A markdown file in the vault, outside the templates folder, whose frontmatter has a `type`. Files without one, such as the vault's working rules or the tags file, are plain files.
_Avoid_: note, record

**Template**:
A markdown file in the root `templates/` folder, which the assistant reads to follow a type's structure. It can be written, but it is not a document: queries skip it, and of the document rules only "its frontmatter parses" applies.
_Avoid_: stub, skeleton

**Type**:
The kind of a document, named by its `type` field and defined in the config with its folder, file name pattern, required fields and date field.
_Avoid_: kind, category

**Vocabulary**:
The tags the vault allows, read from the tags file.
_Avoid_: tag list, taxonomy

**Document rules**:
What a document is expected to satisfy: its frontmatter parses, its type is configured, its required fields have values, its date field (the type's, or `date` for a type not configured) holds a date and its tags are in the vocabulary. A write may not break a document rule; one the document already broke is a warning, not a failure.
_Avoid_: invariants, validation
