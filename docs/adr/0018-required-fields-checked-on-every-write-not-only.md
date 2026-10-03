# Required fields checked on every write, not only create_doc
- Instead of: validating required fields only in create_doc (PRD 5.2).
- Why: otherwise replace_doc and update_frontmatter (for example, unsetting a field) could produce documents create_doc would reject. Absent, null, `""`, `[]` and `{}` all count as missing.
- Cost: a human-made doc that is already missing a required field must be fixed before the AI can rewrite it.
