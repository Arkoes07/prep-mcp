# `{week}` field placeholder instead of the `{isoweek}` builtin; every filename lowercase *(deviation)*
- Instead of: a builtin `{isoweek}` computed from today, written in canonical `2026-W41` form (PRD section 4.3).
- Why:
  - The week is a parameter: the `week` frontmatter field fills the filename. This means next week's plan can be created in advance.
  - One case rule with no exceptions: every placeholder value is sanitised to lowercase kebab-case, as PRD 4.3 says, so `week: 2026-W41` → `weeks/2026-w41.md`.
  - The example file was renamed from `weeks/2026-W03.md` to `weeks/2026-w03.md` to match.
- Builtins left: `{slug}` (the `slug` argument, else `title`) and `{date}` (frontmatter `date`, else today). Every field placeholder must be in the type's `required`, which config validation enforces.
- Cost: existing uppercase week files stay as they are; they are still found by field. On a case-sensitive filesystem, create_doc would not detect `2026-W41.md` as an existing `2026-w41.md`.
