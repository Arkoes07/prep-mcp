# Limits and defaults
- `list_docs`: 50 results by default, at most 500. `search_text`: 20 by default, at most 500. Each context query: 20 unless its config says otherwise. Context tag summary: the 10 most used tags of the last 30 days, and no upper date bound.
- Every sort breaks ties by path, ascending, so output is deterministic.
- `list_tags` treats `prefix` as a plain string prefix, so `issue` and `issue/` both work.
- The default commit author is `Claude <claude@local>`. PRD 4.3 shows it only as an example value.
- Why: the PRD sets only `list_docs`' default of 50. The caps, the other defaults, inclusive date bounds and exact `where` matching (a list field matches if any element does) had to be chosen, and the tests exposed the small gaps.
- Cost: no truncation. A vault with hundreds of open items produces a long `get_context`.
