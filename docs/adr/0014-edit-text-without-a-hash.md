# edit_text without a hash

`edit_text` replaces one exact piece of text and takes no `expected_hash`. The text it must find is its guard: if a human changed that text, it no longer matches and nothing is written. A human's edit elsewhere in the file is kept, because only the matched bytes change. Asking for a hash would make every small fix a read-then-write pair and fail on unrelated edits, which is what pushed small fixes onto `replace_doc`.

Cost: an edit can land on a file that changed elsewhere since the model read it, so the model may fix a line without seeing a nearby human change. `old` must match exactly once, so it cannot hit the wrong copy.
