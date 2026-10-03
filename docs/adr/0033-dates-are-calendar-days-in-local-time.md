# Dates are calendar days in local time
- Instead of: comparing instants.
- Why: `date` fields are calendar dates, and mtime is only a fallback. `ParseDate` returns the date as written at local midnight and drops any time of day, so `2026-01-12T09:30:00Z` is 2026-01-12 in every timezone. `since` and `until` compare the local calendar day of a document's date or mtime, inclusive. Without this, the same vault would answer differently in UTC+13.
- Cost: a timestamp near midnight in a `date` field is filed under the day written, not the day in the reader's timezone. Whatever follows the date after a `T` or a space is not checked, so `2026-01-12T99:99` reads as 2026-01-12 instead of being reported.
- Consequence to know: undated documents take their mtime as date, so on a fresh clone every undated document looks as if it were touched today. `get_context` then counts their tags in the 30-day summary. The fallback is PRD 5.1, and the documents that need real dates should get a `date` field.
