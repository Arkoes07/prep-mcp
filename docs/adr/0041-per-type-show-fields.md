# Per-type `show` fields (2026-10-02)
- Instead of: only the top-level `summary_fields`, which showed an application's `stage` and `status` but never `next_action`, the field that says a follow-up is due.
- How: a type may set `show: [fields]`, listed in that order in `get_context` and under `fields` in `list_docs`. It replaces `summary_fields` for that type; `show: []` lists nothing; other types keep `summary_fields`. `list_docs` takes `fields: [...]` to choose the fields for one call, for every document; `[]` lists none. Validation is that of `summary_fields`, plus the type's own date field, which is already shown as `date`.
- Why replace rather than merge: a type's list then says exactly what its lines show, and `show: []` can hide fields.
- Cost: a type with `show` must repeat any summary field it still wants. `list_docs`' `fields` is a JSON object, which `encoding/json` writes with sorted keys, so only `get_context` keeps the configured order.
