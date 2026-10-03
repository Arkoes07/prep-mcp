# `date` and `date_source` in every list result (2026-10-02)
- Instead of: `date` for a document with a date field and `modified` for one without, so results carried one key or the other depending on the file.
- How: every `list_docs` entry has `date` (YYYY-MM-DD) and `date_source`, `field` or `mtime`. Every `get_context` line shows `date=`, followed by `date_source=mtime` when the date is the file's mtime; a field date prints `date=` alone, to save tokens.
- Why: one key to sort and read by, while the source still says when not to trust it (a clone or checkout resets mtime).
- Cost: the `modified` key is gone, so anything that read it breaks; only the model did. Lines for undated documents in `get_context` are about 30 characters longer.
