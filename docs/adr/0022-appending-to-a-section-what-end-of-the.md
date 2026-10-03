# Appending to a section: what "end of the section" and "blank line" mean (M2)
- Rules: the insertion point is after the last non-blank line of the section, so blank lines before the next heading stay where they were. A blank line separates the new text from the old unless both the last old line and the first new line are list items. A section with nothing in it gets a blank line after its heading. The file's line ending (LF or CRLF) is used for everything inserted. Frontmatter is never searched, so a `## x` YAML comment cannot match.
- Consequence: a file that is only frontmatter gets a blank line between the closing `---` and the new `## heading`.
- Cost: list detection is a simple prefix test (`-`, `*`, `+`, `1.` or `1)`), so an indented continuation line counts as prose.
