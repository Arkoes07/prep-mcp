# Tag warnings only for tags a write adds; closest tag by edit distance
- Instead of: warning about every unknown tag in the file, or failing the write.
- Why: warning about tags a human already had would nag on every write. The closest known tag is chosen by Levenshtein distance over normalised tags, with ties broken alphabetically. That gives Acceptance 5 a concrete suggestion.
- Also decided: every backticked `` `x/y` `` token anywhere in `tags.md` is a vocabulary tag, because the example puts all tags on one line.
- Cost: a vocabulary typo already in a file is never flagged.
