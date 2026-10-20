package write_test

import (
	"cmp"
	"testing"
	"time"

	"github.com/arkoes07/prep-mcp/internal/config"
	"github.com/arkoes07/prep-mcp/internal/document"
	"github.com/arkoes07/prep-mcp/internal/write"
)

func TestSlugify(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Checkout Service Rewrite", "checkout-service-rewrite"},
		{"  Hello,  World!! ", "hello-world"},
		{"already-kebab", "already-kebab"},
		{`A/B\C..D`, "a-b-c-d"},
		{"../../etc", "etc"},
		{"Déjà vu", "déjà-vu"},
		{"Deep_Dive", "deep-dive"},
		{"v2.0 Release", "v2-0-release"},
		{"!!!", ""},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := write.Slugify(tc.in); got != tc.want {
				t.Errorf("Slugify(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRenderFilename(t *testing.T) {
	sep29 := time.Date(2026, 9, 29, 10, 0, 0, 0, time.Local)
	tests := []struct {
		name, pattern, fm, slug string
		dateField               string // empty means date
		today                   time.Time
		want, wantErr           string
	}{
		{name: "slug_arg", pattern: "{slug}.md", slug: "My Note", want: "my-note.md"},
		{name: "slug_from_title", pattern: "{slug}.md", fm: "title: My Note", want: "my-note.md"},
		{name: "slug_arg_beats_title", pattern: "{slug}.md", fm: "title: My Note", slug: "Other", want: "other.md"},
		{name: "no_slug_no_title", pattern: "{slug}.md", fm: "a: 1", wantErr: "pass slug or set title"},
		{name: "field_and_date", pattern: "{date}-{kind}-{slug}.md", fm: "kind: Deep Dive\ndate: 2026-01-12", slug: "x", want: "2026-01-12-deep-dive-x.md"},
		{name: "date_default_today", pattern: "{date}-{slug}.md", fm: "a: 1", slug: "x", today: sep29, want: "2026-09-29-x.md"},
		{
			name:      "date_from_date_field",
			pattern:   "{date}-{slug}.md",
			fm:        "finished: 2026-01-08\ndate: 2026-01-12",
			slug:      "x",
			dateField: "finished",
			want:      "2026-01-08-x.md",
		},
		{
			name:      "date_field_missing_today",
			pattern:   "{date}-{slug}.md",
			fm:        "date: 2026-01-12",
			slug:      "x",
			dateField: "finished",
			today:     sep29,
			want:      "2026-09-29-x.md",
		},
		{
			name:    "date_timestamp_cut",
			pattern: "{date}-{slug}.md",
			fm:      "date: 2026-01-12T09:00",
			slug:    "x",
			want:    "2026-01-12-x.md",
		},
		{name: "week_upper", pattern: "{week}.md", fm: "week: 2026-W41", want: "2026-w41.md"},
		{name: "week_lower", pattern: "{week}.md", fm: "week: 2026-w41", want: "2026-w41.md"},
		{name: "week_missing", pattern: "{week}.md", fm: "a: 1", wantErr: "week is required to build the filename"},
		{name: "kind_missing", pattern: "{kind}.md", fm: "a: 1", wantErr: "kind"},
		{name: "kind_traversal", pattern: "{kind}.md", fm: `kind: "../../x"`, want: "x.md"},
		{name: "kind_list", pattern: "{kind}.md", fm: "kind: [a, b]", wantErr: "kind"},
		{name: "kind_alias", pattern: "{kind}.md", fm: "base: &k Deep Dive\nkind: *k", want: "deep-dive.md"},
		{name: "kind_only_punctuation", pattern: "{kind}.md", fm: `kind: "!!!"`, wantErr: "kind"},
		{name: "slug_folder", pattern: "{slug}.md", slug: "ddia/ch05-replication", want: "ddia/ch05-replication.md"},
		{
			name:    "slug_folders_kebab",
			pattern: "{slug}.md",
			slug:    "DDIA/Ch05 Replication",
			want:    "ddia/ch05-replication.md",
		},
		{name: "slug_deep", pattern: "{slug}.md", slug: "a/b c/d", want: "a/b-c/d.md"},
		{name: "title_slash_flattens", pattern: "{slug}.md", fm: "title: A/B", want: "a-b.md"},
		{name: "slug_dotdot", pattern: "{slug}.md", slug: "../../x", wantErr: "pass folder/name"},
		{name: "slug_dotdot_inside", pattern: "{slug}.md", slug: "a/../b", wantErr: "pass folder/name"},
		{name: "slug_dot", pattern: "{slug}.md", slug: "./a", wantErr: "pass folder/name"},
		{name: "slug_empty_segment", pattern: "{slug}.md", slug: "a//b", wantErr: "pass folder/name"},
		{name: "slug_leading_slash", pattern: "{slug}.md", slug: "/a", wantErr: "pass folder/name"},
		{name: "slug_trailing_slash", pattern: "{slug}.md", slug: "a/", wantErr: "pass folder/name"},
		{name: "slug_backslash", pattern: "{slug}.md", slug: `a\b`, wantErr: "pass folder/name"},
		{name: "slug_segment_punctuation", pattern: "{slug}.md", slug: "ddia/!!!", wantErr: "pass folder/name"},
		{name: "slug_blank_segment", pattern: "{slug}.md", slug: "ddia/ /x", wantErr: "pass folder/name"},
		{
			name:    "slug_folder_other_pattern",
			pattern: "{date}-{kind}-{slug}.md",
			fm:      "kind: k",
			slug:    "a/b",
			wantErr: `builds names from "{date}-{kind}-{slug}.md", so slug cannot contain "/" or "\"; pass a plain name`,
		},
		{name: "slug_folder_prefix_pattern", pattern: "x-{slug}.md", slug: "a/b", wantErr: "pass a plain name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Types: map[string]config.TypeSpec{"t": {DateField: cmp.Or(tc.dateField, "date")}}}
			doc, err := document.Parse([]byte("---\ntype: t\n"+tc.fm+"\n---\n"), cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			day := tc.today
			if day.IsZero() {
				day = time.Date(2026, 1, 14, 9, 0, 0, 0, time.Local)
			}
			got, err := write.RenderFilename(tc.pattern, doc, tc.slug, day)
			if tc.wantErr != "" {
				mustContain(t, err, tc.wantErr)
				return
			}
			if err != nil {
				t.Fatalf("RenderFilename: %v", err)
			}
			if got != tc.want {
				t.Errorf("RenderFilename(%q) = %q, want %q", tc.pattern, got, tc.want)
			}
		})
	}
}
