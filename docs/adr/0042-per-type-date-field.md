# Per-type `date_field` (2026-10-02)
- Instead of: the fixed field `date`, which made the real vault rename `applied` to `date` so applications could be filtered by date.
- How: a type may set `date_field`, default `date`. The scan reads it, so `since`/`until`, date sorting, `get_context`'s recent queries and the tag dates follow it, and so does the `{date}` filename placeholder. A document without the field is dated by mtime; a `date` key on a type whose date field is another is an ordinary field. Validation rejects a blank `date_field` and `type`, `title` or `tags`, which never hold a date.
- Why `{date}` follows it: otherwise a type with `date_field: applied` and `{date}-{slug}.md` would name its files with today's date.
- Cost: one more config key. Without a `type`, `list_docs` dates each document by its own type's field, so one `since` can compare different fields for different types.
