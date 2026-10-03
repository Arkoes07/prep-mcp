# `list_docs` returns `date` or `modified`, never a body
*Superseded on 2026-10-02 by [ADR-0043](0043-date-and-date-source-in-every-list-result.md): the `modified` key is gone. Bodies are still never returned, and the `note` for an unknown type stays.*

- Instead of: one `date` field that is sometimes a file mtime.
- Why: a model reading `date` should be able to trust it. A document with a `date` field returns `date`; one without returns `modified` (the file's last-modified day) instead. `updated` appears when present, and so do the configured summary fields, under `fields`. Bodies are never returned. A `type` that is not configured and matches nothing returns a `note` listing the configured types.
- Cost: a client that wants one sort key must read either field.
