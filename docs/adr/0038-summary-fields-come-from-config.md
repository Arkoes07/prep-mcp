# Summary fields come from config *(deviation from PRD 5.1)*
*Amended on 2026-10-02: a type's `show` list replaces `summary_fields` for that type (see [ADR-0041](0041-per-type-show-fields.md)).*

- Instead of: showing `status`, `stage` and `kind` whenever a document has them, as PRD 5.1 lists for `list_docs`.
- Why: those three names come from one vault's types, and nothing in the code acts on them; they are only displayed. Fixed in Go, they broke goal 5 in a way the forbidden-word test cannot see, because the words are generic. A vault with `priority` or `owner` fields would have had to rename them to get them shown.
- How: a top-level `summary_fields` list in `prep.config.yaml`. `get_context` shows them as attributes in config order after `date=`. `list_docs` returns the ones a document sets in a `fields` object, and its tool description is generated from the config so the model knows the names. The example config sets `[kind, stage, status]`, so the example output is unchanged.
- Fields that carry meaning stay fixed, because the data model defines them: `type`, `title`, `tags`, `date`, `created`, `updated`. Validation rejects listing an always-shown one (`type`, `title`, `tags`, `date`, `updated`), an empty name or a repeat.
- Cost: one more config key, and the default is no summary fields at all. In `list_docs` the values moved from top-level keys (`"status": "open"`) into `"fields": {"status": "open"}`. Only scalar values are shown.
