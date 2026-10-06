# A write may not make a document worse

Every write checks the document rules (see `CONTEXT.md`) once, in the batch plan, for each op against the file as the earlier ops leave it. An op fails only on a rule it breaks: a required field it empties, a tag it adds that the vocabulary lacks, a type it removes or sets to one the config does not define, or frontmatter it leaves unparseable. A rule the document already broke is a warning. Holding every result to every rule would block all writes, appends included, to a document a human left without a required field or with an old tag, which is why the tag check already looked only at added tags.

Frontmatter that did not parse before the op leaves nothing to compare against, so what the rules find in the result is a warning; that lets a write repair it. A changed type is checked like a new document: all of the new type's required fields apply. `edit_text` may not change the type ([ADR-0014](0014-edit-text-without-a-hash.md)).

Rejected: checking each file once at the end of a batch, from its first to its final content. It would accept a type change whose required field a later op adds, but blame a file instead of an op, and `edit_text`'s type rule would still need a check per op.

Cost: a document can keep a broken rule indefinitely, warned about on every write. A repair of broken frontmatter can add an unknown tag with only a warning. A type change and the new type's required fields must come in one op.
