# Search matches bodies and the `title` field only
- Instead of: searching whole files including frontmatter, or matching file names.
- Why: PRD 5.1 says bodies and titles. Matching frontmatter would return every document that merely carries a tag or a field value, which `list_docs` already answers.
- Rules: case-insensitive substring; a document without a `title` field is not matched by its filename stem, even though the stem is shown as its title; a title match is reported at the file line of the `title:` key, and `Text` is always the verbatim file line; hits are ordered by path; each hit reports up to 3 lines and the total count.
- Cost: searching for a frontmatter value needs `list_docs` with `where`.
