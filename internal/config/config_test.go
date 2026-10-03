package config_test

import (
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Arkoes07/prep-mcp/internal/config"
	"github.com/Arkoes07/prep-mcp/internal/testvault"
)

// These tests assume the default author equals the example config's.
const defaultAuthor = "Claude <claude@local>"

func TestParse(t *testing.T) {
	// weekType is a valid week type that the context tests build on.
	const weekType = `
types:
  week:
    folder: weeks
    filename: "{week}.md"
    required: [week]
`
	tests := []struct {
		name string
		yaml string
		// wantErr lists substrings that must all appear in the error.
		// Empty means Parse must succeed.
		wantErr []string
		check   func(t *testing.T, c *config.Config)
	}{
		{
			name: "minimal",
			yaml: `
types:
  note: {folder: notes, filename: "{slug}.md"}
`,
			check: func(t *testing.T, c *config.Config) {
				if !c.CommitOnWrite {
					t.Error("commit_on_write should default to true")
				}
				if c.Author != defaultAuthor {
					t.Errorf("Author = %q, want default %q", c.Author, defaultAuthor)
				}
				if c.TagsFile != "" {
					t.Errorf("TagsFile = %q, want empty", c.TagsFile)
				}
				ts, ok := c.Types["note"]
				if !ok || ts.Folder != "notes" || ts.Filename != "{slug}.md" || len(ts.Required) != 0 {
					t.Errorf("Type(note) = %+v, %v", ts, ok)
				}
				if ts.DateField != "date" || c.DateField("note") != "date" {
					t.Errorf("date field = %q / %q, want the default date", ts.DateField, c.DateField("note"))
				}
				if got := c.DateField("nope"); got != "date" {
					t.Errorf("DateField(nope) = %q, want date for a type that is not configured", got)
				}
				if _, ok := c.Types["nope"]; ok {
					t.Error("Type(nope) should not exist")
				}
			},
		},
		{
			name: "empty_file",
			yaml: ``,
			check: func(t *testing.T, c *config.Config) {
				if len(c.Types) != 0 {
					t.Errorf("Types = %v, want none", c.Types)
				}
				if !c.CommitOnWrite {
					t.Error("commit_on_write should default to true")
				}
			},
		},
		{
			name: "commit_false",
			yaml: `commit_on_write: false`,
			check: func(t *testing.T, c *config.Config) {
				if c.CommitOnWrite {
					t.Error("commit_on_write: false was ignored")
				}
			},
		},
		{
			name:    "unknown_top_key",
			yaml:    `comit_on_write: true`,
			wantErr: []string{"comit_on_write", "line 1"},
		},
		{
			name: "unknown_type_key",
			yaml: `
types:
  note:
    fodler: notes
    filename: "{slug}.md"
`,
			wantErr: []string{"fodler"},
		},
		{
			name: "missing_folder",
			yaml: `
types:
  note: {filename: "{slug}.md"}
`,
			wantErr: []string{"type note: folder is required"},
		},
		{
			name: "missing_filename",
			yaml: `
types:
  note: {folder: notes}
`,
			wantErr: []string{"type note: filename is required"},
		},
		{
			name: "filename_not_md",
			yaml: `
types:
  note: {folder: notes, filename: "{slug}.txt"}
`,
			wantErr: []string{"type note", "filename", ".md"},
		},
		{
			name: "filename_has_slash",
			yaml: `
types:
  note: {folder: notes, filename: "{date}/{slug}.md"}
`,
			wantErr: []string{"type note", "filename", "/"},
		},
		{
			name: "folder_absolute",
			yaml: `
types:
  note: {folder: /etc, filename: "{slug}.md"}
`,
			wantErr: []string{"type note", "folder"},
		},
		{
			name: "folder_dotdot",
			yaml: `
types:
  note: {folder: ../x, filename: "{slug}.md"}
`,
			wantErr: []string{"type note", "folder"},
		},
		{
			name: "folder_dot_git",
			yaml: `
types:
  note: {folder: .git, filename: "{slug}.md"}
`,
			wantErr: []string{"type note", "folder"},
		},
		{
			name: "folder_dot_obsidian_nested",
			yaml: `
types:
  note: {folder: .obsidian/x, filename: "{slug}.md"}
`,
			wantErr: []string{"type note", "folder"},
		},
		{
			name: "folder_templates",
			yaml: `
types:
  note: {folder: templates, filename: "{slug}.md"}
`,
			wantErr: []string{"type note", "folder"},
		},
		{
			name: "folder_templates_any_case",
			yaml: `
types:
  note: {folder: Templates, filename: "{slug}.md"}
`,
			wantErr: []string{"type note", "folder"},
		},
		{
			name: "folder_nested_ok",
			yaml: `
types:
  note: {folder: a/b, filename: "{slug}.md"}
`,
		},
		{
			name: "placeholder_unbalanced",
			yaml: `
types:
  note: {folder: notes, filename: "{slug.md"}
`,
			wantErr: []string{"type note", "filename"},
		},
		{
			name: "date_field",
			yaml: `
types:
  note: {folder: notes, filename: "{slug}.md", date_field: finished}
`,
			check: func(t *testing.T, c *config.Config) {
				if got := c.DateField("note"); got != "finished" {
					t.Errorf("DateField(note) = %q, want finished", got)
				}
			},
		},
		{
			name: "date_field_blank",
			yaml: `
types:
  note: {folder: notes, filename: "{slug}.md", date_field: " "}
`,
			wantErr: []string{`type note: date_field " " cannot hold a date`, "remove date_field"},
		},
		{
			name: "date_field_not_a_date",
			yaml: `
types:
  note: {folder: notes, filename: "{slug}.md", date_field: tags}
`,
			wantErr: []string{`type note: date_field "tags" cannot hold a date`},
		},
		{
			name: "summary_fields",
			yaml: `
summary_fields: [priority, owner]
`,
			check: func(t *testing.T, c *config.Config) {
				if want := []string{"priority", "owner"}; !slices.Equal(c.SummaryFields, want) {
					t.Errorf("SummaryFields = %q, want %q", c.SummaryFields, want)
				}
			},
		},
		{
			name:    "summary_field_empty",
			yaml:    `summary_fields: [priority, ""]`,
			wantErr: []string{"summary_fields[1] is empty", "remove the entry"},
		},
		{
			name:    "summary_field_always_shown",
			yaml:    `summary_fields: [date]`,
			wantErr: []string{"date is always shown", "remove it"},
		},
		{
			name:    "summary_field_twice",
			yaml:    `summary_fields: [owner, owner]`,
			wantErr: []string{"owner is listed twice", "remove one"},
		},
		{
			// A type's show list replaces summary_fields for that type only.
			name: "show",
			yaml: `
summary_fields: [status]
types:
  note: {folder: notes, filename: "{slug}.md", show: [priority, owner]}
  log: {folder: logs, filename: "{slug}.md"}
`,
			check: func(t *testing.T, c *config.Config) {
				tests := []struct {
					typ  string
					want []string
				}{
					{typ: "note", want: []string{"priority", "owner"}},
					{typ: "log", want: []string{"status"}},
					{typ: "nope", want: []string{"status"}},
				}
				for _, tc := range tests {
					if got := c.ShowFields(tc.typ); !slices.Equal(got, tc.want) {
						t.Errorf("ShowFields(%s) = %q, want %q", tc.typ, got, tc.want)
					}
				}
			},
		},
		{
			name: "show_empty_hides",
			yaml: `
summary_fields: [status]
types:
  note: {folder: notes, filename: "{slug}.md", show: []}
`,
			check: func(t *testing.T, c *config.Config) {
				if got := c.ShowFields("note"); len(got) != 0 {
					t.Errorf("ShowFields(note) = %q, want none: an empty show list hides summary_fields", got)
				}
			},
		},
		{
			name: "show_field_empty",
			yaml: `
types:
  note: {folder: notes, filename: "{slug}.md", show: [priority, ""]}
`,
			wantErr: []string{"type note: show[1] is empty", "remove the entry"},
		},
		{
			name: "show_always_shown",
			yaml: `
types:
  note: {folder: notes, filename: "{slug}.md", show: [title]}
`,
			wantErr: []string{"type note: show: title is always shown", "remove it"},
		},
		{
			// The date field is shown as date already.
			name: "show_date_field",
			yaml: `
types:
  note: {folder: notes, filename: "{slug}.md", date_field: finished, show: [finished]}
`,
			wantErr: []string{"type note: show: finished is always shown", "remove it"},
		},
		{
			name: "show_twice",
			yaml: `
types:
  note: {folder: notes, filename: "{slug}.md", show: [owner, owner]}
`,
			wantErr: []string{"type note: show: owner is listed twice", "remove one"},
		},
		{
			name: "field_placeholder_not_required",
			yaml: `
types:
  note: {folder: notes, filename: "{kind}.md", required: []}
`,
			wantErr: []string{"type note", "add kind to required"},
		},
		{
			name: "builtin_placeholders_ok",
			yaml: `
types:
  note: {folder: notes, filename: "{date}-{slug}.md"}
`,
		},
		{
			name: "field_placeholder_ok",
			yaml: `
types:
  note: {folder: notes, filename: "{week}.md", required: [week]}
`,
		},
		{
			name: "week_type_undefined",
			yaml: `
context:
  current_week_type: nope
  current_week_field: week
`,
			wantErr: []string{"current_week_type", "nope"},
		},
		{
			name: "week_field_missing",
			yaml: weekType + `
context:
  current_week_type: week
`,
			wantErr: []string{"set context.current_week_field"},
		},
		{
			name: "week_field_not_required",
			yaml: `
types:
  week:
    folder: weeks
    filename: "{slug}.md"
    required: [title]
context:
  current_week_type: week
  current_week_field: week
`,
			wantErr: []string{"add week to required"},
		},
		{
			name: "no_week_type",
			yaml: `
types:
  note: {folder: notes, filename: "{slug}.md"}
context:
  recent:
    - {type: note}
`,
			check: func(t *testing.T, c *config.Config) {
				if c.Context.CurrentWeekType != "" || c.Context.CurrentWeekField != "" {
					t.Errorf("week settings should be empty, got %+v", c.Context)
				}
			},
		},
		{
			name: "week_ok",
			yaml: weekType + `
context:
  current_week_type: week
  current_week_field: week
`,
			check: func(t *testing.T, c *config.Config) {
				if c.Context.CurrentWeekType != "week" || c.Context.CurrentWeekField != "week" {
					t.Errorf("Context = %+v", c.Context)
				}
			},
		},
		{
			name: "query_type_undefined",
			yaml: `
context:
  open_items:
    - {type: nope}
`,
			wantErr: []string{"nope"},
		},
		{
			name: "where_scalar_or_list",
			yaml: `
types:
  note: {folder: notes, filename: "{slug}.md"}
context:
  open_items:
    - {type: note, where: {status: open}}
    - {type: note, where: {status: [a, b]}}
`,
			check: func(t *testing.T, c *config.Config) {
				items := c.Context.OpenItems
				if len(items) != 2 {
					t.Fatalf("OpenItems = %d, want 2", len(items))
				}
				if got := items[0].Where["status"]; !slices.Equal(got, []string{"open"}) {
					t.Errorf("scalar where = %v, want [open]", got)
				}
				if got := items[1].Where["status"]; !slices.Equal(got, []string{"a", "b"}) {
					t.Errorf("list where = %v, want [a b]", got)
				}
			},
		},
		{
			name: "query_tags",
			yaml: `
types:
  note: {folder: notes, filename: "{slug}.md"}
context:
  recent:
    - {type: note, tags: [x/y], any_tags: [p/, q/], limit: 3}
`,
			check: func(t *testing.T, c *config.Config) {
				q := c.Context.Recent[0]
				if !slices.Equal(q.Tags, []string{"x/y"}) || !slices.Equal(q.AnyTags, []string{"p/", "q/"}) || q.Limit != 3 {
					t.Errorf("Query = %+v", q)
				}
			},
		},
		{
			name: "limit_negative",
			yaml: `
types:
  note: {folder: notes, filename: "{slug}.md"}
context:
  recent:
    - {type: note, limit: -1}
`,
			wantErr: []string{"limit"},
		},
		{
			name: "limit_zero_uses_default",
			yaml: `
types:
  note: {folder: notes, filename: "{slug}.md"}
context:
  recent:
    - {type: note, limit: 0}
    - {type: note}
`,
			check: func(t *testing.T, c *config.Config) {
				for i, q := range c.Context.Recent {
					if q.Limit != 20 {
						t.Errorf("Recent[%d].Limit = %d, want default 20", i, q.Limit)
					}
				}
			},
		},
		{
			name:    "author_invalid",
			yaml:    `author: Claude`,
			wantErr: []string{"author"},
		},
		{
			name: "author_valid",
			yaml: `author: "A B <a@b.c>"`,
			check: func(t *testing.T, c *config.Config) {
				name, email, _ := config.ParseAuthor(c.Author)
				if name != "A B" || email != "a@b.c" {
					t.Errorf("ParseAuthor(%q) = %q, %q", c.Author, name, email)
				}
			},
		},
		{
			name: "author_default_ident",
			yaml: ``,
			check: func(t *testing.T, c *config.Config) {
				name, email, _ := config.ParseAuthor(c.Author)
				if name != "Claude" || email != "claude@local" {
					t.Errorf("ParseAuthor(%q) = %q, %q", c.Author, name, email)
				}
			},
		},
		{
			name:    "tags_file_dotdot",
			yaml:    `tags_file: ../tags.md`,
			wantErr: []string{"tags_file"},
		},
		{
			name:    "tags_file_absolute",
			yaml:    `tags_file: /tags.md`,
			wantErr: []string{"tags_file"},
		},
		{
			name: "many_errors",
			yaml: `
types:
  alpha: {filename: "{slug}.md"}
  beta: {folder: b}
`,
			wantErr: []string{"type alpha: folder is required", "type beta: filename is required"},
		},
		{
			name:    "invalid_yaml",
			yaml:    `types: [`,
			wantErr: []string{"line"},
		},
		{
			name: "type_names_sorted",
			yaml: `
types:
  zeta: {folder: z, filename: "{slug}.md"}
  alpha: {folder: a, filename: "{slug}.md"}
  mid: {folder: m, filename: "{slug}.md"}
`,
			check: func(t *testing.T, c *config.Config) {
				want := []string{"alpha", "mid", "zeta"}
				if got := c.TypeNames(); !slices.Equal(got, want) {
					t.Errorf("TypeNames = %v, want %v", got, want)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := config.Parse([]byte(tc.yaml))
			if len(tc.wantErr) > 0 {
				if err == nil {
					t.Fatalf("Parse succeeded, want error containing %q", tc.wantErr)
				}
				for _, s := range tc.wantErr {
					if !strings.Contains(err.Error(), s) {
						t.Errorf("error %q does not contain %q", err, s)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if c == nil {
				t.Fatal("Parse returned nil config and nil error")
			}
			if tc.check != nil {
				tc.check(t, c)
			}
		})
	}
}

func TestLoadExample(t *testing.T) {
	root := testvault.Copy(t)
	c, err := config.Load(filepath.Join(root, "prep.config.yaml"))
	if err != nil {
		t.Fatalf("Load example config: %v", err)
	}
	if got := len(c.Types); got != 7 {
		t.Errorf("types = %d, want 7", got)
	}
	if names := c.TypeNames(); len(names) != 7 || !slices.IsSorted(names) {
		t.Errorf("TypeNames = %v, want 7 sorted names", names)
	}
	if !c.CommitOnWrite {
		t.Error("commit_on_write = false, want true")
	}
	if c.Author != defaultAuthor {
		t.Errorf("Author = %q, want %q", c.Author, defaultAuthor)
	}
	if c.TagsFile != "tags.md" {
		t.Errorf("TagsFile = %q, want tags.md", c.TagsFile)
	}
	if want := []string{"kind", "stage", "status"}; !slices.Equal(c.SummaryFields, want) {
		t.Errorf("SummaryFields = %q, want %q", c.SummaryFields, want)
	}
	if got := len(c.Context.OpenItems); got != 2 {
		t.Errorf("open_items = %d, want 2", got)
	}
	if got := len(c.Context.Recent); got != 1 {
		t.Fatalf("recent = %d, want 1", got)
	}
	if got := c.Context.Recent[0].Limit; got != 5 {
		t.Errorf("recent limit = %d, want 5", got)
	}
	if got := c.Context.OpenItems[0].Where["status"]; !slices.Equal(got, []string{"open", "improving"}) {
		t.Errorf("open_items[0].where.status = %v, want [open improving]", got)
	}

	if c.Context.CurrentWeekType != "week" || c.Context.CurrentWeekField != "week" {
		t.Errorf("current week = %q / %q, want week / week", c.Context.CurrentWeekType, c.Context.CurrentWeekField)
	}
	wk, ok := c.Types["week"]
	if !ok {
		t.Fatal("example config has no week type")
	}
	if wk.Filename != "{week}.md" || wk.Folder != "weeks" || !slices.Equal(wk.Required, []string{"week"}) {
		t.Errorf("week type = %+v", wk)
	}
	if lt, ok := c.Types["learning"]; !ok || !slices.Equal(lt.Required, []string{"title", "track"}) {
		t.Errorf("learning type = %+v, %v", lt, ok)
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Load(missing) error = %v, want one wrapping fs.ErrNotExist", err)
	}
}
