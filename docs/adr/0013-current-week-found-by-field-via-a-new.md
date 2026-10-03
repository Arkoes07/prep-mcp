# Current week found by field, via a new `current_week_field` key *(deviation)*
- Instead of: building the week file's path from its filename pattern and reading that path.
- Why: the lookup is "the doc of `current_week_type` whose `current_week_field` equals this ISO week". It compares case-insensitively and ignores filenames, so week files a human creates by hand in Obsidian are found whatever they are named.
- Cost: one more config key (`context.current_week_field: week`), which must be in that type's `required`. If two docs match the same week, the first by path wins and the other is reported as a problem.
