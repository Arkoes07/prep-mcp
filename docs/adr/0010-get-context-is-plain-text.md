# `get_context` returns plain text, not JSON

Sections start with `==== name ====` lines, which cannot be confused with the `##` headings of the embedded week document, and text costs fewer tokens than JSON and reads well in a chat. Lists are one line per document (`- path | title | attributes | tags`). The week section carries `path` and `hash`, so `replace_doc` needs no extra `read_doc`. `internal/read/testdata/context.golden` is the specification.

Cost: the format is ours, so the model reads a convention instead of parsing JSON, and a format change means regenerating the golden files.
