# prep-mcp build plan

## Context
The repo holds only the spec: PRD.md, CLAUDE.md, a DESIGN.md skeleton, a README and `examples/vault` (8 typed docs, 7 templates, a config and tags.md). There is no Go code yet. The goal is a stdio MCP server in Go that gives an assistant read access and safe write access to a markdown+YAML vault. The build is test-first. Read tools come first, and all example-specific knowledge stays in data.

Facts I checked (2026-09-29):
- Installed: Go 1.26.4, git 2.50.1, `claude` CLI.
- Every example file is ASCII with LF line endings and a trailing newline.
- Only the example config's required lists differ from the PRD: story adds `ownership`, learning adds `track`.

**Revision 2 (after your review of open questions 3, 7, 9, 10 and 19):**
- #3: `get_context` takes an optional `date` param, which defaults to the server's today. `PREP_TODAY` is dropped.
- #7: No change; the PRD fallback stays. The table now explains mtime.
- #9: The current week doc is found by field, not by filename. This adds a new config key, `current_week_field`.
- #10: The week type uses a `{week}` field placeholder. The `{isoweek}` builtin is dropped. Every filename is lowercase kebab, and `examples/vault/weeks/2026-W03.md` is renamed to `2026-w03.md`.
- #19: You confirmed the real vault is its repo's root, so the rule stays. M3 adds an acceptance run against a clone of the real vault, then the real vault itself.

---

## 1. Open questions (each has a proposed answer; none are resolved silently)

| # | Ambiguity / conflict | Proposed answer |
|---|---|---|
| 1 | PRD asks for **Go 1.22+**, but both SDKs now need **≥1.25** (go-sdk v1.8.0: `go 1.25.0`; mcp-go v1.1.1: `go 1.25.5`). | Set `go 1.25` in go.mod. This also gives us the full `os.Root` API (MkdirAll/Rename/WriteFile). Record in DESIGN.md. |
| 2 | Root/config precedence isn't stated. It's also unclear what happens if the config is missing, and whether the config is reloaded. | Precedence: `--root` > `PREP_ROOT` > cwd, and `--config` > `PREP_CONFIG` > `<root>/prep.config.yaml`. A missing *default* config means start with no types: reads work, and create_doc explains how to add a config. A missing *explicit* config, or an invalid config, exits with every problem listed. The config is loaded once at startup, so editing it needs a restart (DESIGN.md). |
| 3 | get_context depends on "today". The example vault is 2026-W03, so run today (W40) it shows no week doc and an empty 30-day tag line. | **Decided:** get_context takes an optional `date` (`YYYY-MM-DD`), defaulting to the server's local today. It sets the current week and the 30-day tag window. It stays optional because the server clock is the authority when nobody asks for another day. Writes (`created`, `updated`, the `{date}` default) always use the server clock. No env override. This deviates from PRD's `get_context()` and is recorded in DESIGN.md. |
| 4 | §4.4 says "every *line* matching `prefix/value`", but the example tags.md puts all 10 tags on one line. | Every backticked `` `x/y` `` token anywhere in the file is a tag. A token without `/` is not a vocabulary tag. |
| 5 | "Closest known tag" (Acceptance 5) has no algorithm. It's also unclear which tags should trigger warnings. | Use Levenshtein distance over normalised tags, with ties broken alphabetically. Always name the closest tag when the vocabulary is non-empty. Warn only about tags that *this write adds*, not tags a human already had. |
| 6 | Tag normalisation edge cases. | Lowercase and trim, plus library-mcp's bracketed-string form (`"[a, 'b']"`). Drop empty entries, dedupe keeping the first, stringify scalars, ignore nested items. Any tag list a write touches is written back as a normalised list in the node's existing flow or block style. A comma string becomes a flow list. |
| 7 | Many docs (story, improvement, application, week) have no `date`, so "date, falling back to mtime" sorts them by last edit. mtime is the filesystem's last-modified timestamp: any save changes it, and a clone or checkout resets it. | **Decided:** follow the PRD exactly, `date` then mtime, so undated docs read as "most recently touched first". `created`/`updated` are not used. Ties are broken by path. An unparseable `date` falls back to mtime and is reported as a problem. |
| 8 | Sessions have no `title`. | The title is `title` if present, otherwise the filename stem. |
| 9 | It's unclear how get_context finds "the current ISO week document". | **Decided:** compute the ISO week of `date`/today (e.g. `2026-W40`). Pick the doc of `current_week_type` whose `current_week_field` (a new config key, `week` in the example) equals it, compared case-insensitively and trimmed. The lookup ignores filenames, so hand-made files like `2026-W40.md` are found too. If several match, the first by path wins and the rest are reported as a problem. If none match, print one line: `no week doc for 2026-W40 (create_doc type=week with week: 2026-W40)`. Config validation: the field must be in that type's `required`. |
| 10 | "Values are sanitised to lowercase kebab-case" contradicts `{isoweek}` producing `2026-W41` (uppercase W). | **Decided:** the week comes from a parameter, the `week` frontmatter field, via a `{week}` field placeholder. The example config becomes `filename: "{week}.md"`. The `{isoweek}` builtin is dropped (deviation, DESIGN.md). **One case rule, no exceptions:** every placeholder value is lowercase kebab, so `week: 2026-W41` → `weeks/2026-w41.md`. The example `weeks/2026-W03.md` is renamed to `weeks/2026-w03.md`; its `week:` value may keep its case. Builtins: `{slug}` comes from the `slug` arg, else `title`, else an error. `{date}` comes from frontmatter `date`, else today. |
| 11 | A field placeholder like `{kind}` could be missing at create time. | Config validation requires every field placeholder to be in `required`. The example config already satisfies this. `filename` may not contain `/`; `folder` may be nested (`a/b`). |
| 12 | "Validates required fields" doesn't define "empty". | Absent, null, `""`, `[]` and `{}` all count as missing. The same check also guards replace_doc and update_frontmatter (e.g. unsetting a required field), not only create_doc. |
| 13 | Which files do the query tools cover? | list_docs, search_text and list_tags cover typed docs only, so search_text skips CLAUDE.md. Files with invalid YAML frontmatter are skipped but listed under `problems` in list_docs and get_context output, so the human can fix them. |
| 14 | Read scope isn't specified. | read_doc can read any `.md` in the root, **including `templates/`**, so the AI can follow a template. It refuses dot-folders and non-`.md` files. create_doc's tool description is generated from config (types, path pattern, required fields), so the AI never needs to read the config. |
| 15 | Path rule details. | Paths are relative with `/`. Absolute paths are rejected even inside the root (§8). The template ban applies only to root-level `templates/`. Any dot component (folder or file) is forbidden for writes and skipped by scans. Comparisons are case-insensitive (APFS). **Writes refuse any symlink in the path.** Reads follow symlinks that stay in the root. The scanner never follows symlinks. |
| 16 | append_section: which heading levels match, and where content goes. | Only level-2 `## ` headings match, exactly, after trimming trailing whitespace, and never inside code fences. Content goes at the **end of that section**, before the next `#`/`##` heading, not right under the heading. If the heading appears twice, use the last one. A list appended after a list gets no blank line; anything else gets one blank line. A missing trailing newline is added before a new heading. `updated` is not touched. |
| 17 | update_frontmatter semantics. | Top-level keys only, with no dotted paths. A file without frontmatter is refused (use replace_doc). `type` cannot be unset. `set.tags` cannot be combined with add_tags/remove_tags. A no-op means no write, no `updated` change and no commit. A caller-supplied `updated` wins. Dates are local `YYYY-MM-DD`. |
| 18 | replace_doc validation. | The file must exist (otherwise use create_doc). The new content gets the same checks as create_doc: valid YAML and required fields present, otherwise rejected. Unknown tags produce a warning. Dropping the frontmatter produces a warning that the file will leave queries. `updated` is not set. The hash is lowercase hex sha256. |
| 19 | "If the root is a git repo" is ambiguous when the vault sits *inside* a repo. That is the case for examples/vault inside this repo. | **Decided (you confirmed your real vault is its repo's root):** commit only when `git rev-parse --show-toplevel` equals the vault root, comparing both after `EvalSymlinks`. The vault path is **not** in prep.config.yaml, which lives inside the vault. It is set by the MCP registration: `claude mcp add --scope user prep -- prep-mcp --root ~/prep`. The real vault therefore commits. examples/vault inside this repo never does, so the public repo gets no AI commits. When the rule blocks a commit, warn "not committed: …". Commit only the written path (`git commit --only -- <path>`) so the human's staged work isn't swept in. Set the committer identity to the configured author too, so commits work without `user.name`. Keep hooks enabled, with a 10 s timeout. |
| 20 | Output formats. | read_doc and get_context return plain text with a small header (path, hash). List, search and tag tools return compact JSON. Every write returns `{path, hash, warnings[], commit}`. |
| 21 | Does CLAUDE.md's "no domain words in Go code" rule cover `_test.go` files and SDK identifiers such as `req.Session`? | Yes to both. Example-specific expectations live in `testdata/*.yaml` and golden files. A `TestNoDomainWords` test reads the word list from testdata and enforces the rule. Tool descriptions avoid "session" (e.g. "start-of-conversation bundle"). |
| 22 | The README quick start runs `git init` *inside* `examples/vault`, which nests a repo in this one. | Change the README to copy the vault out first (`cp -R examples/vault ~/prep-demo`). I'll make the change in M3. |
| 23 | Limits and matching aren't defined. | list_docs: default 50, capped at 500. search_text: default 20, at most 3 lines per doc plus a total count. Each context query defaults to 20. `where` compares scalar text exactly, and a list field matches if any element matches. `since`/`until` are inclusive `YYYY-MM-DD`. No truncation for the 4k target; the golden test asserts the example output is under 16 KB. |

---

## 2. SDK choice → recommend the official `github.com/modelcontextprotocol/go-sdk`

These facts come from proxy.golang.org and the tagged source:

| | official go-sdk | mark3labs/mcp-go |
|---|---|---|
| Latest | v1.8.0 (2026-09-04) | v1.1.1 (2026-09-23) |
| Stable v1 since | 2025-09-30, a year of minor releases | 2026-09-02, only weeks ago |
| MCP spec | 2026-07-28 plus all older versions | — |
| Min Go | 1.25.0 | 1.25.5 |
| Tool API | `mcp.AddTool[In,Out](s, &mcp.Tool{…}, func(ctx, *CallToolRequest, In) (*CallToolResult, Out, error))`. The schema is inferred from the struct and `jsonschema` tags, and args are validated before the handler runs. A returned `error` becomes an `IsError` result (`toolForErr`). `req.Params.Arguments` stays raw JSON. | Builder style (`mcp.NewTool(..., mcp.WithString(...))`), with map args or `BindArguments`. |
| Tests | `mcp.NewInMemoryTransports()` plus `mcp.NewClient` | in-process client |
| Panic recovery | none, so we wrap each handler | `server.WithRecovery()` |
| Transitive deps | jsonschema-go, jwt, oauth2, x/tools | testify, cast, jsonschema/v6, uuid |

**Why the official SDK:** it meets the PRD's condition (stable v1 with a year of compatible releases). It tracks the spec first, it's co-maintained with the Go team, and typed inputs remove hand-written argument parsing.

**Costs:**
- We write a ~15-line recover wrapper.
- go.sum gets larger (unused auth packages aren't linked).
- Ordered frontmatter from JSON has to be re-decoded from the raw `Arguments` into `yaml.Node`. That's the same work with either SDK.

This goes into DESIGN.md's "MCP SDK choice" entry.

---

## 3. Layout (PRD §7 plus one test-only helper)

```
cmd/prep-mcp/main.go        flags/env → config → vault → write → server; stdio; logs to stderr only
internal/config/            load + validate
internal/vault/             paths, frontmatter split, tags, scan, query, search, tag stats, read, context
internal/write/             filename, append, frontmatter patch, create/replace, atomic write, git
internal/server/            tool registration, input structs, formatting, recover wrapper
internal/testvault/         test-only helper: copy examples/vault to t.TempDir(), fixed mtimes, fixed clock, isolated git init
```

**config**: owns the shape and validity of `prep.config.yaml`.
```go
type Config struct { TagsFile string; CommitOnWrite bool; Author string; Types map[string]TypeSpec; Context ContextSpec }
type TypeSpec struct { Folder, Filename string; Required []string }
type ContextSpec struct { CurrentWeekType, CurrentWeekField string; OpenItems, Recent []Query }
type Query struct { Type string; Where map[string][]string; Tags, AnyTags []string; Limit int }
func Parse(data []byte) (*Config, error)   // strict (KnownFields), defaults, then Validate; errors.Join of all problems
func Load(path string) (*Config, error)    // wraps fs.ErrNotExist so main can choose "missing default" behaviour
func (c *Config) Validate() error
func (c *Config) Type(name string) (TypeSpec, bool)
func (c *Config) TypeNames() []string      // sorted
func (c *Config) AuthorIdent() (name, email string)
```

**vault**: all reads. Every file I/O goes through a `*os.Root`.
```go
type Vault struct { /* dir string; root *os.Root; cfg *config.Config; now func() time.Time */ }
func Open(dir string, cfg *config.Config, now func() time.Time) (*Vault, error) // EvalSymlinks(dir), os.OpenRoot
type Access int; const ( Read Access = iota; Write )
func (v *Vault) Resolve(p string, a Access) (string, error)   // clean slash-relative path or *PathError with a hint
func (v *Vault) Root() *os.Root
type Frontmatter struct { Map *yaml.Node; Start, End, BodyOffset, BodyLine int; CRLF bool }
func SplitFrontmatter(src []byte) (*Frontmatter, error)       // nil,nil when absent
type Doc struct { Path, Type, Title, Updated string; Tags []string; Date time.Time; DateFromMtime bool; Fields map[string]*yaml.Node; Body []byte; BodyLine int }
type Problem struct { Path, Msg string }
func (v *Vault) Scan() ([]*Doc, []Problem, error)
func NormalizeTags(n *yaml.Node) []string
func NormalizeTagList(in []string) []string
func MatchTag(pattern, tag string) bool                      // trailing "/" = prefix
type Vocabulary struct{ /* set + sorted list */ }
func ParseVocabulary(src []byte) *Vocabulary
func (v *Vault) Vocabulary() (*Vocabulary, error)            // nil when no tags_file / file missing
func (voc *Vocabulary) Known(tag string) bool
func (voc *Vocabulary) Closest(tag string) string
type Filter struct { Type string; Tags, AnyTags []string; Where map[string][]string; Since, Until *time.Time; Sort string; Limit int }
func Apply(docs []*Doc, f Filter) ([]*Doc, error)            // pure
func (v *Vault) List(f Filter) ([]*Doc, []Problem, error)
type Hit struct { Path, Title string; Lines []Line; Total int }; type Line struct { N int; Text string }
func (v *Vault) Search(q, typ string, limit int) ([]Hit, error)
type TagCount struct { Tag string; Count int; Last time.Time; Unknown bool }
func (v *Vault) TagStats(prefix string, since *time.Time) ([]TagCount, error)
func (v *Vault) Read(p string) (path string, content []byte, hash string, err error)
func Hash(b []byte) string
func ISOWeek(t time.Time) string; func ParseDate(s string) (time.Time, error)
func (v *Vault) Context(day time.Time) (string, error)       // get_context text; day = the `date` param or today; deterministic for a given day + mtimes
```

**write**: all mutations. Pure byte transforms plus a `Writer` that orchestrates them.
```go
func Slugify(s string) string
func RenderFilename(pattern string, fm *yaml.Node, slug string, today time.Time) (string, error)
func InsertSection(src []byte, heading, content string) ([]byte, error)
type Patch struct { Set *yaml.Node /* ordered mapping */; Unset, AddTags, RemoveTags []string }
func ApplyPatch(src []byte, p Patch, today string) (out []byte, changed bool, err error) // line-splice, see Risk 2
func JSONToNode(raw json.RawMessage) (*yaml.Node, error)   // keeps key order; date-like strings plain, ambiguous strings quoted
type Git struct { Bin, Author string; Timeout time.Duration }
func (g *Git) Commit(ctx context.Context, dir, rel, msg string) (hash, warning string)
type Writer struct{ /* v *vault.Vault; cfg; git *Git; now */ }
func New(v *vault.Vault, cfg *config.Config, g *Git, now func() time.Time) *Writer
type Result struct { Path, Hash, Commit string; Warnings []string }
func (w *Writer) Create(ctx context.Context, typ string, fm *yaml.Node, body, slug string) (*Result, error)
func (w *Writer) AppendSection(ctx context.Context, path, heading, content string) (*Result, error)
func (w *Writer) UpdateFrontmatter(ctx context.Context, path string, p Patch) (*Result, error)
func (w *Writer) Replace(ctx context.Context, path, content, expectedHash string) (*Result, error)
type ExistsError struct{ Path string }; type ConflictError struct{ Path, Current string }
```

**server**: MCP glue only. No logic worth unit-testing test-first.
```go
func New(v *vault.Vault, w *write.Writer, cfg *config.Config, version string) *mcp.Server
func guard[In any](name string, h mcp.ToolHandlerFor[In, any]) mcp.ToolHandlerFor[In, any] // recover → IsError
// one input struct per tool, e.g. type listDocsIn struct { Type string `json:"type,omitempty" jsonschema:"…"`; … }
```

---

## 4. Test plan (written before implementation)

Conventions:
- `testvault.Copy(t)` copies `examples/vault` into `t.TempDir()` and sets every mtime to **2026-01-10 12:00 local**, which is before the test day, so mtime-dated docs sort below dated ones.
- The test day is **2026-01-14** (Wednesday of 2026-W03). It is passed to `Context(day)` and injected as the writer's clock.
- Expectations that name example-specific paths or types live in `internal/vault/testdata/*.yaml` and are run by a generic table runner (see open question 21).
- Write tests use example types without domain words (`week`, `improvement`, `learning`) or neutral inline inputs.

### 4.1 internal/config: `TestParse` (table, inline YAML) + `TestLoadExample`
| name | input | expected |
|---|---|---|
| example | examples/vault/prep.config.yaml | ok; 7 types; commit true; author `Claude <claude@local>`; 2 open_items, 1 recent |
| minimal | one type `{folder: notes, filename: "{slug}.md"}` | ok; commit_on_write defaults true; default author; tags_file "" |
| empty_file | `""` | ok, no types |
| commit_false | `commit_on_write: false` | false |
| unknown_top_key | `comit_on_write: true` | error names key + line |
| unknown_type_key | `fodler:` | error |
| missing_folder / missing_filename | — | error "type note: folder is required" / filename |
| filename_not_md | `"{slug}.txt"` | error |
| filename_has_slash | `"{date}/{slug}.md"` | error |
| folder_absolute / folder_dotdot | `/etc`, `../x` | error |
| folder_forbidden | `.git`, `.obsidian/x`, `templates`, `Templates` | error |
| placeholder_unbalanced | `"{slug.md"` | error |
| field_placeholder_not_required | `"{kind}.md"`, required [] | error "add kind to required" |
| builtin_placeholders_ok | `{slug}` `{date}` | ok |
| field_placeholder_ok | `"{week}.md"`, required [week] | ok |
| week_type_undefined | `current_week_type: nope` | error |
| week_field_missing | `current_week_type` set, no `current_week_field` | error "set context.current_week_field" |
| week_field_not_required | field `week` not in the type's required | error "add week to required" |
| no_week_type | neither key set | ok; get_context skips the week section |
| query_type_undefined | open_items type nope | error |
| where_scalar_or_list | `{status: open}` / `{status: [a, b]}` | `[open]` / `[a b]` |
| limit_negative / limit_zero | -1 / 0 | error / default 20 |
| author_invalid / valid | `Claude` / `"A B <a@b.c>"` | error / ok |
| tags_file_escape | `../tags.md`, `/tags.md` | error |
| many_errors | two bad types | one error containing both |
| invalid_yaml | `types: [` | error with line |
| load_missing | nonexistent path | `errors.Is(err, fs.ErrNotExist)` |
| type_names_sorted | 3 types | alphabetical |

### 4.2 internal/vault

**`TestResolve`** (table on a temp copy). Setup adds: `out.md` outside the root; `link-out.md → out.md`; `linkdir → <outside dir>`; `link-in.md → weeks/2026-w03.md`.

| input | Read | Write |
|---|---|---|
| `weeks/2026-w03.md` | ok | ok |
| `./weeks/../weeks/2026-w03.md` | ok, cleaned | ok, cleaned |
| `../out.md`, `weeks/../../x.md` | err "outside the vault" | err |
| `/etc/passwd`, `<root>/weeks/2026-w03.md` | err "absolute paths not allowed; use a vault-relative path" | err |
| `""`, `weeks/2026-w03.md/`, `a\b.md`, `a\x00.md` | err | err |
| `link-out.md`, `linkdir/x.md` | err outside | err outside |
| `link-in.md` | ok | err "is a symlink; write to weeks/2026-w03.md" |
| `.git/config`, `.git/x.md`, `.obsidian/w.md`, `weeks/.hidden.md`, `.GIT/x.md` | err | err |
| `templates/week.md`, `Templates/x.md` | ok (read) | err "templates/ is read-only" |
| `weeks/2026-w03.txt`, `prep.config.yaml` | err ".md only" | err |
| `newdir/sub/x.md` (nonexistent) | err not found (at Read) | ok |

Plus these standalone tests:
- `TestRootEnforced`: Resolve ok, then swap `weeks/` for a symlink to outside. The write via `os.Root` fails and the outside dir is untouched.
- `TestOpenViaSymlinkedRoot`: open the vault through a symlink; `t.TempDir` under `/var` → `/private/var` already exercises this.

**`TestSplitFrontmatter`** (inline bytes)
| name | input | expected |
|---|---|---|
| standard | `---\ntype: x\n---\nbody\n` | type x; body `body\n`; BodyLine 4 |
| none | `# hi\n` | nil |
| crlf | `---\r\ntype: x\r\n---\r\nb` | ok, CRLF true |
| bom | `﻿---\n…` | ok |
| unterminated | `---\ntype: x\nbody` | error "frontmatter not closed" |
| empty | `---\n---\nb` | empty map, no type |
| invalid_yaml | `---\ntype: [x\n---\n` | error with line |
| not_mapping | `---\n- a\n---\n` | error "must be a mapping" |
| duplicate_key | `a: 1\na: 2` | error |
| leading_blank_line | `\n---\n…` | nil (not frontmatter) |
| four_dashes | `----\n` | nil |
| closing_trailing_space | `--- \n` | ok |
| hr_in_body | body contains `---` | first closing fence only |
| no_trailing_newline | `---\ntype: x\n---` | ok, empty body |

**`TestNormalizeTags`** (the value of `tags:`)
| input | expected |
|---|---|
| `[a, b]` | [a b] |
| `[A, " B "]` | [a b] |
| `"a, b"` / `"A,  B ,c"` | [a b] / [a b c] |
| `"[a, 'b']"` | [a b] |
| `a` | [a] |
| null / `[]` / key missing | [] |
| `"a,,b,"` | [a b] |
| `[a, A, "a "]` | [a] |
| `[2026, true]` | ["2026" "true"] |
| `[a, [b], {c: d}]` | [a] |
| `"Issue/Buries-Headline"` | [issue/buries-headline] |
| `["\ta\n"]` | [a] |

**`TestParseVocabulary`**:
- example tags.md gives 10 tags, including `q/failure` and `meta/plan`.
- One tag per line or many per line: both parse.
- `` `foo` `` (no slash) is ignored.
- `` `Q/Failure` `` becomes `q/failure`.
- An empty file gives an empty vocabulary.

**`TestClosest`**:
- `issue/buries-headlin` → `issue/buries-headline`
- `skill/tradeoff` → `skill/trade-off`
- A tie picks the alphabetically first.
- An empty vocabulary gives `""`.
- `Known("Q/FAILURE")` is true after normalising.

**`TestMatchTag`**:
- (`issue/`, `issue/x`) true
- (`issue/`, `issues/x`) false
- (`issue`, `issue/x`) false
- (`issue/x`, `issue/x`) true
- (`Issue/`, `issue/x`) true

**`TestISOWeek`**:
- 2026-01-14 → 2026-W03
- 2026-01-01 → 2026-W01
- 2027-01-01 → 2026-W53
- 2024-12-30 → 2025-W01
- 2026-09-29 → 2026-W40

**`TestParseDate`**:
- `2026-01-12`, quoted `"2026-01-12"` and `2026-01-12T09:30:00Z` all parse.
- `12/01/2026` and `garbage` return an error.

**`TestScan`** (temp copy; expected paths in testdata):
- Returns exactly the 8 typed docs. Excluded: CLAUDE.md (no frontmatter), templates/*, prep.config.yaml.
- Setup adds files; expected results:

  | Added file | Expected |
  |---|---|
  | `.obsidian/x.md` (typed) | excluded |
  | `.hidden.md` | excluded |
  | `improvements/sub/x.md` | included |
  | `notes/nofm.md` | excluded, readable |
  | `notes/notype.md` | excluded silently |
  | `notes/bad.md` (invalid YAML) | excluded + Problem with path and line |
  | `linkdir → outside` | not followed |
  | `link-in.md` | skipped (not double-counted) |

- Title fallback gives the stem.
- A doc with no date gives Date = mtime and `DateFromMtime` true.
- `date: garbage` gives mtime plus a Problem.

**`TestList`**: data-driven from `testdata/queries.yaml`. Real names are shown here for review.

| name | filter | want (in order) |
|---|---|---|
| all | {} | 8 docs date_desc, ties by path |
| by_type | type=session | 01-14, 01-12 |
| acceptance_4 | type=session, tags=[issue/buries-headline] | 01-12 |
| tags_and | tags=[q/hardest-project, issue/missing-tradeoff] | 01-14, 01-12 |
| tags_and_narrow | tags=[q/hardest-project, issue/buries-headline] | 01-12 |
| any_tags_or | any_tags=[issue/buries-headline, domain/payments] | 01-12, story (mtime 01-10) |
| prefix | tags=[issue/] | 01-14, 01-12 |
| prefix_any | any_tags=[company/, domain/] | application, story (path order) |
| tag_case | tags=[Issue/Missing-Tradeoff] | both sessions |
| where_scalar | where={status: open} | improvements/buries-headline |
| where_list | where={status: [open, improving]} | both improvements |
| where_type | type=application, where={status: [active]} | acme-backend |
| where_list_field | where={focus: "Headline first"} | weeks/2026-w03 |
| where_nomatch / where_missing_field | {status: open}+type=application / {nope: x} | [] |
| since | type=session, since=2026-01-13 | 01-14 |
| until_inclusive | type=session, until=2026-01-12 | 01-12 |
| since_mtime_fallback | type=improvement, since=2026-01-10 / 2026-01-11 | both / [] |
| since_bad | since=13-01-2026 | error "since: use YYYY-MM-DD" |
| date_asc / title | sort=date_asc / sort=title (type=improvement) | 01-12, 01-14 / buries-headline, missing-tradeoff |
| sort_bad | sort=newest | error listing valid values |
| limit / limit_zero / limit_huge | 1 / 0 / 9999 | 01-14 / default 50 / capped 500 |
| unknown_type | type=nope | [] + note listing known types |
| metadata | type=session | each item has path,type,title,tags,date,kind; no body |

**`TestSearch`** (testdata):

| query | args | expected |
|---|---|---|
| `trade-off` | — | both sessions, story, missing-tradeoff (title match) and tags.md (body token `skill/trade-off`), with correct 1-based file line numbers (e.g. session 01-12 line 10) |
| `HEADLINE` | — | matches (case-insensitive) |
| `headline` | type=improvement | buries-headline, via its title, reported at line 3 |
| `deep-dive` | — | [] (other frontmatter fields aren't searched) |
| `Sam` | — | [] (untyped CLAUDE.md is skipped) |
| (fixture with 5 matching lines) | — | 3 lines, Total 5 |
| `""` | — | error |
| (any) | limit | respected |

**`TestTagStats`**:
- With no prefix, sort by count desc then name.
- `q/hardest-project` has count 3 (2 sessions and the story) and last 2026-01-14.
- `issue/missing-tradeoff` has count 2.
- `prefix=issue/` gives 2 rows.
- A copy with a doc tagged `issue/typo` has that row marked Unknown.
- With tags.md deleted, nothing is marked.
- `since` = 30 days before 2026-03-01 gives [].

**`TestRead`**:
- weeks/2026-w03.md: bytes equal the file and the hash equals `sha256` computed in the test.
- CLAUDE.md and templates/week.md are readable.
- A missing file gives the error "not found: x (use list_docs or search_text)".

**`TestContextGolden`**: compare against `testdata/context.golden`; `-update` regenerates it.
- The golden file is hand-written for review in M1b and is the spec for the output format.
- Sections appear in PRD order 1–5.
- Two runs give identical output.
- Output is under 16 KB.

Variants:
- Day 2026-09-29 → `context-noweek.golden`, with the line `no week doc for 2026-W40 (create_doc type=week with week: 2026-W40)`.
- With CLAUDE.md deleted, section 1 is omitted and there's no error.

**`TestCurrentWeek`** (day 2026-01-14 unless stated):

| setup | expected |
|---|---|
| example copy | `weeks/2026-w03.md` |
| file renamed to `weeks/My Week.md`, same `week: 2026-W03` | found (lookup ignores the filename) |
| a doc with `week: 2026-w03` (lowercase value) | found (case-insensitive) |
| two docs with week 2026-W03 | first by path, plus a Problem naming the other |
| a doc of another type with `week: 2026-W03` | ignored |
| day 2026-01-19 | none |

**`TestNoDomainWords`** (in cmd/prep-mcp): walks all `*.go` files including tests. There must be no whole-word match of the words in `testdata/forbidden-words.txt`.

### 4.3 internal/write

**`TestSlugify`**:
| input | expected |
|---|---|
| `Checkout Service Rewrite` | `checkout-service-rewrite` |
| `  Hello,  World!! ` | `hello-world` |
| `already-kebab` | same |
| `A/B\C..D` | `a-b-c-d` |
| `../../etc` | `etc` |
| `Déjà vu` | `déjà-vu` |
| `Deep_Dive` | `deep-dive` |
| `v2.0 Release` | `v2-0-release` |
| `!!!` | `""` |

**`TestRenderFilename`** (pattern, fm, slug, today → want/err):
| pattern | fm | slug | today | want / err |
|---|---|---|---|---|
| `{slug}.md` | — | `My Note` | — | `my-note.md` |
| `{slug}.md` | title `My Note` | none | — | `my-note.md` |
| `{slug}.md` | no title | none | — | err "pass slug or set title" |
| `{date}-{kind}-{slug}.md` | kind `Deep Dive`, date 2026-01-12 | `x` | — | `2026-01-12-deep-dive-x.md` |
| `{date}-{slug}.md` | no date | `x` | 2026-09-29 | `2026-09-29-x.md` |
| `{week}.md` | week `2026-W41` | — | — | `2026-w41.md` (lowercase, one rule) |
| `{week}.md` | week `2026-w41` | — | — | `2026-w41.md` (same file either way) |
| `{week}.md` | week missing | — | — | err "week is required to build the filename" |
| `{kind}.md` | kind missing | — | — | err |
| `{kind}.md` | kind `../../x` | — | — | `x.md` |
| `{kind}.md` | kind is a list | — | — | err |
| `{kind}.md` | kind `!!!` | — | — | err |

**`TestInsertSection`** (pure). Every row also asserts the bytes before the insertion point are unchanged.
| name | src | heading, content | expected |
|---|---|---|---|
| new_heading | `a\n` | H, `c` | `a\n\n## H\n\nc\n` |
| new_no_trailing_nl | `a` | H, `c` | `a\n\n## H\n\nc\n` |
| existing_prose | `## H\nold\n` | H, `new` | `## H\nold\n\nnew\n` |
| existing_list | `## Log\n- a\n` | Log, `- b` | `## Log\n- a\n- b\n` |
| before_next_section | `## A\nx\n\n## B\ny\n` | A, `z` | `## A\nx\n\nz\n\n## B\ny\n` |
| empty_section | `## A\n\n## B\n` | A, `z` | `## A\n\nz\n\n## B\n` |
| subsections_included | `## A\nx\n### A1\ny\n## B\n` | A, `z` | z after `y`, before `## B` |
| h1_terminates | `## A\nx\n# T\n` | A, `z` | z before `# T` |
| no_prefix_match | `## Feedback notes\n` | Feedback | new `## Feedback` at end |
| other_level | `### H\n` | H | new `## H` at end |
| in_code_fence | ```` ```\n## H\n```\n ```` | H | new `## H` at end |
| duplicate_uses_last | two `## H` | H, `z` | under the second |
| trailing_ws_heading | `## H  \n` | H | matches |
| heading_normalised | — | `## H` | same as `H` |
| heading_newline | — | `a\nb` | err |
| content_trailing_nls | — | `c\n\n\n` | single `\n` |
| empty_content | — | `""` | err "content is empty" |
| crlf | CRLF file | — | inserted lines use CRLF |

**`TestApplyPatch`** (pure). Every row also asserts `out[bodyOffset:] == in[bodyOffset:]`, and that untouched lines are byte-identical.
| name | src frontmatter | patch | expected |
|---|---|---|---|
| set_existing | `a: 1\nb: x  # c\nc: 3` | set b=y | only b's line changes (comment kept); `updated` appended |
| order_kept | a,b,c | set b | keys a,b,c,updated |
| set_new | a | set z=1 then y=2 | a,z,y,updated (caller order) |
| unset | a,b | unset b | b's lines gone |
| unset_block | `metrics:\n  x: 1\n  y: 2` | unset metrics | all 3 lines gone |
| unset_missing | a | unset q | changed=false |
| aligned_comments | template-style `kind: x          # a \| b` + `date:` | set date | kind line byte-identical |
| blank_and_comment_lines | comment/blank between keys | set a | preserved |
| value_list | — | set f=[x,y] | `f: [x, y]` |
| value_map | — | set m={p:1} | block map |
| date_string | — | set d="2026-01-20" | `d: 2026-01-20` (unquoted) |
| ambiguous_string | — | set s="yes" / "123" | quoted |
| wikilink | — | set s="[[x]]" | `s: "[[x]]"` |
| add_tags_flow | `tags: [a, b]` | add [C] | `tags: [a, b, c]` |
| add_tags_block | block list | add c | block list + `- c` |
| add_tags_string | `tags: a, b` | add c | `tags: [a, b, c]` |
| add_tags_absent | no tags | add c | `tags: [c]` appended |
| add_existing_tag | `[a]` | add A | changed=false, bytes identical, no `updated` |
| remove_tags / remove_last | `[a, b]` / `[a]` | remove a | `[b]` / `[]` |
| remove_absent | `[a]` | remove z | changed=false, reported |
| tags_conflict | — | set tags + add_tags | err |
| set_unset_same | — | set a, unset a | err |
| unset_type | — | unset type | err |
| explicit_updated | — | set updated=2020-01-01 | caller value kept |
| body_edge_cases | body with `---`, trailing spaces, tabs, unicode, no final newline | set a | body bytes identical |
| no_frontmatter | `# x` | set a | err "no frontmatter; use replace_doc" |
| invalid_frontmatter | bad YAML | set a | err "fix the YAML at line N first" |
| crlf | CRLF | set a | new lines CRLF |
| empty_frontmatter | `---\n---\n` | set a=1 | `---\na: 1\nupdated: …\n---\n` |

**`TestJSONToNode`**:
- `{"b":1,"a":2}` keeps the order b, a.
- A nested map and a list work.
- `null` is a null node.
- Non-object input returns an error.

**`TestWriterCreate`** (temp copy):
| name | input | expected |
|---|---|---|
| happy | learning {title: "DDIA ch5", track: ddia}, body `x` | `learning/ddia-ch5.md` (folder created); exact bytes `---\ntype: learning\ntitle: DDIA ch5\ntrack: ddia\ncreated: 2026-01-14\n---\nx\n`; hash matches |
| slug_arg | same + slug `Custom!` | `learning/custom.md` |
| slug_traversal | slug `../../x` | `learning/x.md` |
| key_order | `{"track":"ddia","title":"T"}` | type, track, title, created |
| missing_required | no track | err "missing required fields for learning: track"; no file |
| empty_required | track `""` / `[]` | err |
| unknown_type | nope | err listing configured types |
| type_conflict / same | fm.type=week / =learning | err / ok |
| exists | improvement slug `buries-headline` | ExistsError "file exists: improvements/buries-headline.md (use append_section or update_frontmatter)"; file unchanged |
| created_kept | fm has created 2020-01-01 | kept |
| week_param | week {week: 2026-W04} | `weeks/2026-w04.md`; the frontmatter keeps `week: 2026-W04` |
| week_exists | week {week: 2026-W03} | ExistsError for `weeks/2026-w03.md` |
| tags_normalised | tags `"A, b"` | `tags: [a, b]` |
| unknown_tag | tags [issue/buries-headlin] | ok + warning naming `issue/buries-headline` |
| known_tag / no_tags_file | known / tags.md deleted | no warning |
| empty_body / body_no_nl | `""` / `x` | frontmatter only / `x\n` |

**`TestWriterAppend`**:
- weeks/2026-w03.md Log `- Tue: x`: exactly one `## Log`, the line lands after `- Mon…`, the frontmatter bytes are unchanged, and the new hash is returned.
- improvements/missing-tradeoff.md Countermeasure: a new section.
- A missing file: err "not found… use create_doc".
- templates/week.md: path error.

**`TestWriterUpdate`**:
- improvements/buries-headline.md set status=improving: the diff against the original is exactly the status line plus `updated: 2026-01-14`. The body is identical.
- Unset `exit_criteria`: err "exit_criteria is required for improvement".
- Add `issue/buries-headlin`: warning naming the closest tag (Acceptance 5).
- A no-op leaves the mtime unchanged.

**`TestWriterReplace`**:
| name | steps | expected |
|---|---|---|
| happy | read → H; replace(new, H) | ok; hash = sha256(new) |
| conflict (§8) | read → H; append a line on disk; replace(new, H) | ConflictError with current hash H2 and "re-read with read_doc, merge, retry"; disk still holds the human's version |
| bad_hash / empty_hash | `abc` / `""` | conflict / err "expected_hash is required; get it from read_doc" |
| missing_file | — | err "use create_doc" |
| invalid_yaml | — | err; file unchanged |
| drops_required | — | err |
| unknown_tag | — | ok + warning |
| drops_frontmatter | — | ok + warning "will no longer appear in queries" |
| templates_path | — | path err |

**`TestAtomicWrite`**:
- File mode 0600 is preserved.
- No `.prep-tmp-*` files are left after success or after an injected failure.
- Writing into a hard-linked file replaces the inode, so the outside link target is unchanged.

**`TestGit*`** (skipped if git is missing). Each test uses `GIT_CONFIG_GLOBAL=/dev/null` and `GIT_CONFIG_NOSYSTEM=1`, so your git config never leaks in.
| name | setup | expected |
|---|---|---|
| not_repo | plain copy | write ok; warning "not committed: … run git init in <root>"; no .git created |
| disabled | repo, commit_on_write=false | no commit, no warning |
| commit | repo, create | +1 commit; author `Claude <claude@local>`; message `create_doc: learning/x.md`; only that path in the commit |
| each_tool | append/update/replace | messages `append_section: …` etc. |
| human_staged | human stages other.md | AI commit has only its path; other.md still staged |
| subdir_of_repo | repo at parent, vault = child | not committed + warning (open question 19) |
| root_via_symlink | vault repo opened through a symlinked path (like `~/prep` → elsewhere) | commits (toplevel and root compared after EvalSymlinks) |
| hook_fails | pre-commit `exit 1` | write kept; warning includes hook output |
| hook_hangs | hook sleeps 30 s, timeout 1 s | warning "git timed out" |
| index_lock | `.git/index.lock` present | warning |
| no_git_binary | Bin=/nonexistent | warning |
| noop_update | — | no commit |

### 4.4 MCP wiring (not test-first)
After M1 and M2, add an in-memory smoke test (`mcp.NewInMemoryTransports` plus `mcp.NewClient` on a temp copy). It checks:
- The exact tool set (5, then 9).
- Descriptions contain no forbidden words.
- One valid call per tool gives `!IsError`.
- A bad path gives `IsError` with a hint, not a protocol error.
- An injected panic gives `IsError "internal error…"`, and a following call still works.

The manual Claude Code checks in §5 then cover the real client.

---

## 5. Milestones (each one: a. failing tests → b. **stop for your review** → c. implement until green → d. manual check)

### M1: Read-only server on examples/vault
- **a.** Example-data changes first:
  - rename `weeks/2026-W03.md` → `weeks/2026-w03.md`
  - week type `filename: "{week}.md"`
  - add `context.current_week_field: week` to `examples/vault/prep.config.yaml`
  - PRD.md is left as written; DESIGN.md already records the change

  Then write go.mod (go 1.25, go-sdk v1.8.0, yaml.v3), the testvault helper, all §4.1 tests and all §4.2 tests. The §4.2 tests include the hand-written `context.golden` and `queries.yaml`, and `TestCurrentWeek`. Finally write `TestNoDomainWords`.
- **b.** Show the tests, the golden file and the testdata to you.
- **c.** Implement config and vault. Implement server with the 5 read tools, `guard`, and get_context (with optional `date`)/list_docs/read_doc/search_text/list_tags. Implement `main.go` with flags and env (`--root`/`PREP_ROOT`, `--config`/`PREP_CONFIG`). Add the wiring smoke test. The DESIGN.md entries already exist; change one only if the implementation differs from it.
- **d.**
  ```bash
  go test ./... && go vet ./...
  go install ./cmd/prep-mcp
  claude mcp add prep-example -- "$(go env GOPATH)/bin/prep-mcp" --root "$PWD/examples/vault"
  claude mcp list        # prep-example: ✓ Connected
  ```
  Then, in `claude`:
  1. "Call get_context with date 2026-01-14 and show me the raw result." Expect CLAUDE.md, weeks/2026-w03.md in full, 2 improvements, 1 application, 2 sessions newest first, and a tag line. Calling it without a date should give the "no week doc for 2026-W40" line.
  2. "Use list_docs for type=session with tags=[issue/buries-headline]." Expect only 2026-01-12.
  3. "search_text 'trade-off', then list_tags with prefix issue/."
  4. "read_doc ../../PRD.md." Expect a refusal with a hint.
  5. `/mcp` should list exactly 5 tools.

  Without Claude, you can check the raw protocol:
  ```bash
  printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"sh","version":"0"}}}' '{"jsonrpc":"2.0","method":"notifications/initialized"}' '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_context","arguments":{"date":"2026-01-14"}}}' | prep-mcp --root examples/vault
  ```

### M2: Write tools, without git
- **a.** Write the §4.2 `TestResolve` Write column and `TestRootEnforced`, plus the §4.3 tests for slug/filename/InsertSection/ApplyPatch/JSONToNode/Writer*/atomic. Leave out git.
- **b.** Show them to you.
- **c.** Implement write (with `Git` stubbed as a no-op) and register create_doc/append_section/update_frontmatter/replace_doc. create_doc's description is generated from config. The DESIGN.md entries already exist; change one only if the implementation differs from it.
- **d.**
  ```bash
  go test -race ./...
  DEMO=$(mktemp -d)/vault && cp -R examples/vault "$DEMO" && echo "$DEMO"
  claude mcp add prep-demo -- "$(go env GOPATH)/bin/prep-mcp" --root "$DEMO"
  ```
  Then, in `claude`:
  1. "Log a deep-dive practice session on [[checkout-rewrite]] dated 2026-01-16: headline at 0:06, trade-off stated. Tag q/hardest-project and issue/missing-tradof." Expect `sessions/2026-01-16-deep-dive-checkout-rewrite.md` and a warning naming `issue/missing-tradeoff`.
  2. "Mark the buries-headline improvement as improving." Then run `diff -u examples/vault/improvements/buries-headline.md "$DEMO/improvements/buries-headline.md"`. Expect only the status line changed and an `updated:` line added.
  3. "Add '- Fri: mock, headline first' to the Log in weeks/2026-w03.md." Expect a single `## Log`.
  4. "Create next week's plan doc for 2026-W04." Expect `weeks/2026-w04.md` with `week: 2026-W04`. Asking again should fail with "file exists".
  5. Ask Claude to read weeks/2026-w03.md. In another shell, run `echo '- human edit' >> "$DEMO/weeks/2026-w03.md"`. Then ask Claude to rewrite the Plan table with replace_doc. Expect a refusal carrying the current hash, then Claude re-reads and merges, keeping "human edit".
  6. "Write to templates/week.md" and "delete tags.md" should be refused, and no delete tool exists.

### M3: Git, acceptance, measurements
- **a.** Write the §4.3 `TestGit*` tests and `BenchmarkScan` (2,000 generated docs in a temp dir).
- **b.** Show them to you.
- **c.** Implement `Git` (toplevel check, `git add -- p`, `git commit --only -m … --author … -- p`, env `GIT_TERMINAL_PROMPT=0` plus committer identity, 10 s timeout, output captured, never pushed). Fill in the DESIGN.md scan measurement (`Measured at ___`) and check the git entries against the implementation. Update the README quick start to copy the vault out (open question 22).
- **d.**
  ```bash
  go test -race ./... && go test -bench Scan -run ^$ ./internal/vault
  DEMO=$(mktemp -d)/vault && cp -R examples/vault "$DEMO" && git -C "$DEMO" init -q && git -C "$DEMO" add . && git -C "$DEMO" commit -qm init
  claude mcp add prep-git -- "$(go env GOPATH)/bin/prep-mcp" --root "$DEMO"
  ```
  Then, in `claude`: "Log this session: behavioral, q/failure, headline in 5 s."
  ```bash
  git -C "$DEMO" log --format='%h | %an <%ae> | %s' -3   # expect: Claude <claude@local> | create_doc: sessions/…
  git -C "$DEMO" show --stat HEAD                       # only that file
  ```
  **Real-vault acceptance (PRD §9.1–9.3).** Do this in two steps: first on a clone of your private vault, then on the vault itself. Before starting, update your real config: add `context.current_week_field: week`, and set the week type's `filename: "{week}.md"`. If you forget, startup fails with a message saying exactly that. Existing `2026-W41.md` files are still found by field.
  ```bash
  git -C ~/prep status --short                       # must be empty, so AI commits stay separate from your work
  TRIAL=$(mktemp -d)/prep && git clone -q ~/prep "$TRIAL"
  claude mcp add prep-trial -- "$(go env GOPATH)/bin/prep-mcp" --root "$TRIAL"
  #   in claude: get_context; log a session; mark an improvement improving; replace_doc after a hand edit
  git -C "$TRIAL" log --author=Claude --format='%h | %an <%ae> | %s'   # one commit per write
  claude mcp remove prep-trial
  claude mcp add --scope user prep -- "$(go env GOPATH)/bin/prep-mcp" --root ~/prep   # the permanent registration
  ```
  Then use it for a real day and check `git -C ~/prep log --author=Claude`. Walk through PRD §9 items 1–7, then run `claude mcp remove prep-example prep-demo prep-git`.

---

## 6. Risks

1. **Path confinement and symlinks.** Two layers:
   - (a) A lexical check for clear error messages: clean the path; reject absolute paths, `..` escapes, `\` and NUL; reject dot components and root `templates/` case-insensitively (APFS); require `.md`.
   - (b) **All I/O goes through `os.Root`**, which the kernel confines via openat and which follows only symlinks that stay inside. That closes the check-then-swap race (`TestRootEnforced`).

   Further measures:
   - The root is resolved with `EvalSymlinks` once. On macOS `/var` → `/private/var`, and `t.TempDir` exercises this.
   - Writes refuse any symlink component.
   - The scanner never follows symlinks.
   - Writes use temp-file-plus-rename, so a hard link to an outside file is replaced rather than written through.

   Residual risk: the path-check message and `os.Root` could disagree on an edge case. Tests cover both layers.
2. **Frontmatter key order and body bytes.** Re-encoding the whole frontmatter with yaml.v3 would collapse the aligned `# a | b` comments your templates produce, which means noisy diffs on every edit to a real file. Instead, `ApplyPatch` uses `yaml.Node` to locate each top-level key's line span and splices only the changed keys. Each changed key is encoded with indent 2, keeps the old line comment, and keeps flow/block style. New keys are appended before the closing fence. The body is copied from `BodyOffset` verbatim.

   Safety net before any write: re-parse, check that untouched keys are semantically equal and the body bytes are identical, otherwise abort with an error.

   Related: JSON args are decoded into `yaml.Node` from the raw `Arguments` to keep key order. Date-like strings are written plain so Obsidian still sees dates.

   Cost: more code than a round-trip, recorded in DESIGN.md with the alternative.
3. **Git when the vault isn't a repo, or is inside another repo.**
   - Not a repo: the write succeeds and the result carries a warning with the fix (`git init`).
   - Vault inside a larger repo, such as examples/vault in this checkout: no commit plus a warning, so dev runs never commit into prep-mcp itself.

   Other failure modes all become warnings, never failed writes: hooks, GPG pinentry (timeout plus `GIT_TERMINAL_PROMPT=0`), `index.lock`, and a missing binary.

   Known limitation: an uncommitted human edit to the same file gets included in the AI's commit, because a commit covers the whole file. This is documented.
4. **stdout is the protocol channel.** Nothing may print to stdout. Logs go to stderr, and git output is captured.
5. **Golden-test nondeterminism** from the day, mtimes (git doesn't keep them) and map order. The fixes are an explicit day passed to `Context`, an injected writer clock, fixed mtimes in `testvault.Copy`, path tie-breaks, and sorted output.
6. **Lost update in append/update.** A human edit landing in the milliseconds between read and rename would be lost. To shrink the window, re-hash immediately before the rename and retry once. The remaining risk is accepted and documented.
7. **Domain-word leakage** through tests, tool descriptions or SDK identifiers. `TestNoDomainWords` catches it.
8. **Go 1.25 floor** differs from the PRD. It's unavoidable with either SDK and recorded in DESIGN.md.
9. **Mixed-case week files in your real vault.** Old files may be `2026-W41.md`, while new ones will be `2026-w42.md`. Lookup is by field, so both are found. On macOS's default case-insensitive disk, creating week 41 again hits "file exists" rather than a duplicate. On a case-sensitive disk it would create a second file. That's accepted; rename old files if you want one style.
