package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/arkoes07/prep-mcp/internal/config"
	"github.com/arkoes07/prep-mcp/internal/testvault"
	"github.com/arkoes07/prep-mcp/internal/vault"
)

func TestFilenameRender(t *testing.T) {
	c, err := config.Parse([]byte(`
types:
  plain: {folder: f, filename: "{slug}.md"}
  dated: {folder: f, filename: "{date}-{slug}.md"}
  finished: {folder: f, filename: "{date}-{slug}.md", date_field: finished}
  kind: {folder: f, filename: "{kind}.md", required: [kind]}
  week: {folder: f, filename: "{week}.md", required: [week]}
  full: {folder: f, filename: "{date}-{kind}-{slug}.md", required: [kind]}
  prefixed: {folder: f, filename: "x-{slug}.md"}
`))
	if err != nil {
		t.Fatal(err)
	}
	sep29 := time.Date(2026, 9, 29, 10, 0, 0, 0, time.Local)
	tests := []struct {
		name, typ, fm, slug string
		today               time.Time
		want, wantErr       string
	}{
		{name: "slug_arg", typ: "plain", slug: "My Note", want: "my-note.md"},
		{name: "slug_from_title", typ: "plain", fm: "title: My Note", want: "my-note.md"},
		{name: "slug_arg_beats_title", typ: "plain", fm: "title: My Note", slug: "Other", want: "other.md"},
		{name: "no_slug_no_title", typ: "plain", fm: "a: 1", wantErr: "pass slug or set title"},
		{name: "title_words", typ: "plain", fm: "title: Checkout Service Rewrite", want: "checkout-service-rewrite.md"},
		{name: "title_punctuation", typ: "plain", fm: "title: '  Hello,  World!! '", want: "hello-world.md"},
		{name: "title_kebab", typ: "plain", fm: "title: already-kebab", want: "already-kebab.md"},
		{name: "title_separators_flatten", typ: "plain", fm: `title: 'A/B\C..D'`, want: "a-b-c-d.md"},
		{name: "title_traversal", typ: "plain", fm: "title: ../../etc", want: "etc.md"},
		{name: "title_accents", typ: "plain", fm: "title: Déjà vu", want: "déjà-vu.md"},
		{name: "title_underscore", typ: "plain", fm: "title: Deep_Dive", want: "deep-dive.md"},
		{name: "title_version", typ: "plain", fm: "title: v2.0 Release", want: "v2-0-release.md"},
		{name: "title_only_punctuation", typ: "plain", fm: "title: '!!!'", wantErr: "pass slug or set title"},
		{
			name: "field_and_date",
			typ:  "full",
			fm:   "kind: Deep Dive\ndate: 2026-01-12",
			slug: "x",
			want: "2026-01-12-deep-dive-x.md",
		},
		{name: "date_default_today", typ: "dated", fm: "a: 1", slug: "x", today: sep29, want: "2026-09-29-x.md"},
		{
			name: "date_from_date_field",
			typ:  "finished",
			fm:   "finished: 2026-01-08\ndate: 2026-01-12",
			slug: "x",
			want: "2026-01-08-x.md",
		},
		{
			name:  "date_field_missing_today",
			typ:   "finished",
			fm:    "date: 2026-01-12",
			slug:  "x",
			today: sep29,
			want:  "2026-09-29-x.md",
		},
		{name: "date_timestamp_cut", typ: "dated", fm: "date: 2026-01-12T09:00", slug: "x", want: "2026-01-12-x.md"},
		{name: "week_upper", typ: "week", fm: "week: 2026-W41", want: "2026-w41.md"},
		{name: "week_lower", typ: "week", fm: "week: 2026-w41", want: "2026-w41.md"},
		{name: "kind_traversal", typ: "kind", fm: `kind: "../../x"`, want: "x.md"},
		{name: "kind_list", typ: "kind", fm: "kind: [a, b]", wantErr: "kind must be a single value"},
		{name: "kind_alias", typ: "kind", fm: "base: &k Deep Dive\nkind: *k", want: "deep-dive.md"},
		{name: "kind_only_punctuation", typ: "kind", fm: `kind: "!!!"`, wantErr: "use a different value"},
		{name: "slug_folder", typ: "plain", slug: "ddia/ch05-replication", want: "ddia/ch05-replication.md"},
		{name: "slug_folders_kebab", typ: "plain", slug: "DDIA/Ch05 Replication", want: "ddia/ch05-replication.md"},
		{name: "slug_deep", typ: "plain", slug: "a/b c/d", want: "a/b-c/d.md"},
		{name: "title_slash_flattens", typ: "plain", fm: "title: A/B", want: "a-b.md"},
		{name: "slug_dotdot", typ: "plain", slug: "../../x", wantErr: "pass folder/name"},
		{name: "slug_dotdot_inside", typ: "plain", slug: "a/../b", wantErr: "pass folder/name"},
		{name: "slug_dot", typ: "plain", slug: "./a", wantErr: "pass folder/name"},
		{name: "slug_empty_segment", typ: "plain", slug: "a//b", wantErr: "pass folder/name"},
		{name: "slug_leading_slash", typ: "plain", slug: "/a", wantErr: "pass folder/name"},
		{name: "slug_trailing_slash", typ: "plain", slug: "a/", wantErr: "pass folder/name"},
		{name: "slug_backslash", typ: "plain", slug: `a\b`, wantErr: "pass folder/name"},
		{name: "slug_segment_punctuation", typ: "plain", slug: "ddia/!!!", wantErr: "pass folder/name"},
		{name: "slug_blank_segment", typ: "plain", slug: "ddia/ /x", wantErr: "pass folder/name"},
		{name: "slug_no_letters", typ: "plain", slug: "!!!", wantErr: "pass a different slug"},
		{
			name:    "slug_folder_other_pattern",
			typ:     "full",
			fm:      "kind: k",
			slug:    "a/b",
			wantErr: `builds names from "{date}-{kind}-{slug}.md", so slug cannot contain "/" or "\"; pass a plain name`,
		},
		{name: "slug_folder_prefix_pattern", typ: "prefixed", slug: "a/b", wantErr: "pass a plain name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fm, err := vault.SplitFrontmatter([]byte("---\n" + tc.fm + "\n---\n"))
			if err != nil {
				t.Fatal(err)
			}
			if tc.today.IsZero() {
				tc.today = testvault.Day
			}
			got, err := c.Types[tc.typ].Filename.Render(fm.Map, tc.slug, tc.today)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Render error = %v, want one containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if got != tc.want {
				t.Errorf("Render = %q, want %q", got, tc.want)
			}
		})
	}
}
