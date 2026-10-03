# Few combinable tools

`list_docs` takes `type`, `tags`, `any_tags`, `where`, `since` and `until` together, instead of one tool per filter (`get_by_tag`, `get_by_date_range`, ...). Models pick tools better from a small set, and filters compose.

Cost: slightly more complex parameters.
