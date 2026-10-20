package document_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/arkoes07/prep-mcp/internal/config"
	"github.com/arkoes07/prep-mcp/internal/document"
	"github.com/arkoes07/prep-mcp/internal/vault"
)

const testConfig = `
types:
  week:
    folder: weeks
    filename: "{week}.md"
    required: [week, start]
  plan:
    folder: plans
    filename: "{slug}.md"
    required: [start]
  book:
    folder: books
    filename: "{slug}.md"
    date_field: finished
`

var voc = vault.ParseVocabulary([]byte("`meta/plan` `issue/buries-headline`"))

func parse(t *testing.T, src string, voc *vault.Vocabulary) *document.Doc {
	t.Helper()
	cfg, err := config.Parse([]byte(testConfig))
	if err != nil {
		t.Fatal(err)
	}
	d, err := document.Parse([]byte(src), cfg, voc)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	return d
}

func TestParse(t *testing.T) {
	tests := []struct {
		name, src string
		noVoc     bool
		typ       string
		title     string
		tags      []string
		dateField string
		date      string // YYYY-MM-DD, "" for none
		findings  []document.Finding
	}{
		{
			name:      "valid",
			src:       "---\ntype: week\ntitle: Plan\nweek: w\nstart: s\ndate: 2026-01-12\ntags: [meta/plan]\n---\nbody\n",
			typ:       "week",
			title:     "Plan",
			tags:      []string{"meta/plan"},
			dateField: "date",
			date:      "2026-01-12",
		},
		// A file without a type is a plain file: no rule applies.
		{
			name:      "no_type",
			src:       "---\ntitle: T\ndate: soon\ntags: [x/unknown]\n---\n",
			title:     "T",
			tags:      []string{"x/unknown"},
			dateField: "date",
		},
		{
			name:      "type_unknown",
			src:       "---\ntype: old\n---\n",
			typ:       "old",
			tags:      []string{},
			dateField: "date",
			findings:  []document.Finding{{Rule: document.RuleType, Key: document.Key{Type: "old"}, Reason: `unknown type "old"`}},
		},
		{
			name:      "required",
			src:       "---\ntype: week\nweek: \"\"\n---\n",
			typ:       "week",
			tags:      []string{},
			dateField: "date",
			findings: []document.Finding{
				{
					Rule:   document.RuleRequired,
					Key:    document.Key{Type: "week", Field: "week"},
					Reason: "missing required field for week: week",
				},
				{
					Rule:   document.RuleRequired,
					Key:    document.Key{Type: "week", Field: "start"},
					Reason: "missing required field for week: start",
				},
			},
		},
		{
			name:      "date",
			src:       "---\ntype: week\nweek: w\nstart: s\ndate: soon\n---\n",
			typ:       "week",
			tags:      []string{},
			dateField: "date",
			findings: []document.Finding{{
				Rule:   document.RuleDate,
				Key:    document.Key{Type: "week", Field: "date", Value: "soon"},
				Reason: `date "soon" is not a date; use YYYY-MM-DD`,
			}},
		},
		// date is just another field once the type names a different one.
		{
			name:      "date_field_of_type",
			src:       "---\ntype: book\nfinished: 2026-01-08\ndate: soon\n---\n",
			typ:       "book",
			tags:      []string{},
			dateField: "finished",
			date:      "2026-01-08",
		},
		{
			name:      "date_of_unconfigured_type",
			src:       "---\ntype: old\ndate: soon\n---\n",
			typ:       "old",
			tags:      []string{},
			dateField: "date",
			findings: []document.Finding{
				{Rule: document.RuleType, Key: document.Key{Type: "old"}, Reason: `unknown type "old"`},
				{
					Rule:   document.RuleDate,
					Key:    document.Key{Type: "old", Field: "date", Value: "soon"},
					Reason: `date "soon" is not a date; use YYYY-MM-DD`,
				},
			},
		},
		{
			name:      "tag",
			src:       "---\ntype: plan\nstart: s\ntags: [meta/plan, issue/buries-headlin]\n---\n",
			typ:       "plan",
			tags:      []string{"meta/plan", "issue/buries-headlin"},
			dateField: "date",
			findings: []document.Finding{{
				Rule:   document.RuleTag,
				Key:    document.Key{Value: "issue/buries-headlin"},
				Reason: `unknown tag "issue/buries-headlin"; closest known tag: "issue/buries-headline"`,
			}},
		},
		{
			name:      "tag_without_vocabulary",
			src:       "---\ntype: plan\nstart: s\ntags: [issue/buries-headlin]\n---\n",
			noVoc:     true,
			typ:       "plan",
			tags:      []string{"issue/buries-headlin"},
			dateField: "date",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := voc
			if tc.noVoc {
				v = nil
			}
			d := parse(t, tc.src, v)
			if d == nil || d.Frontmatter == nil {
				t.Fatalf("Parse = %+v, want a Doc with its frontmatter", d)
			}
			date := ""
			if !d.Date.IsZero() {
				date = d.Date.Format(time.DateOnly)
			}
			if d.Type != tc.typ || d.Title != tc.title || !slices.Equal(d.Tags, tc.tags) ||
				d.DateField != tc.dateField || date != tc.date {
				t.Errorf("Parse = type %q, title %q, tags %q, date %s %q; want %q, %q, %q, %s %q",
					d.Type, d.Title, d.Tags, d.DateField, date, tc.typ, tc.title, tc.tags, tc.dateField, tc.date)
			}
			if !slices.Equal(d.Findings, tc.findings) {
				t.Errorf("Findings = %+v, want %+v", d.Findings, tc.findings)
			}
		})
	}
}

func TestParseWithoutFrontmatter(t *testing.T) {
	cfg, err := config.Parse([]byte(testConfig))
	if err != nil {
		t.Fatal(err)
	}
	if d, err := document.Parse([]byte("# text\n"), cfg, voc); d != nil || err != nil {
		t.Errorf("no frontmatter: Parse = %+v, %v; want nil, nil", d, err)
	}
	_, err = document.Parse([]byte("---\ntype: [x\n---\n"), cfg, voc)
	if fe := (*vault.FrontmatterError)(nil); !errors.As(err, &fe) {
		t.Errorf("broken frontmatter: err = %v, want a *vault.FrontmatterError", err)
	}
}

// A finding in a changed file is the same one when its rule and key are, so
// what makes a new key decides whether a write broke a rule.
func TestFindingIsSame(t *testing.T) {
	const week = "---\ntype: week\nweek: w\ndate: soon\ntags: [issue/buries-headlin]\n---\n"
	tests := []struct {
		name, before, after string
		rule                document.Rule
		same                bool
	}{
		{name: "required/unchanged", before: week, after: week + "more\n", rule: document.RuleRequired, same: true},
		{
			name:   "required/type_changed",
			before: week,
			after:  "---\ntype: plan\nweek: w\ndate: soon\n---\n",
			rule:   document.RuleRequired,
		},
		{name: "date/unchanged", before: week, after: week + "more\n", rule: document.RuleDate, same: true},
		{
			name:   "date/another_non_date",
			before: week,
			after:  "---\ntype: week\nweek: w\ndate: later\n---\n",
			rule:   document.RuleDate,
		},
		{
			name:   "date/type_changed",
			before: week,
			after:  "---\ntype: plan\ndate: soon\n---\n",
			rule:   document.RuleDate,
		},
		{
			name:   "tag/type_changed",
			before: week,
			after:  "---\ntype: plan\nstart: s\ntags: [issue/buries-headlin]\n---\n",
			rule:   document.RuleTag,
			same:   true,
		},
		{
			name:   "type/changed",
			before: "---\ntype: old\n---\n",
			after:  "---\ntype: older\n---\n",
			rule:   document.RuleType,
		},
		{
			name:   "type/unchanged",
			before: "---\ntype: old\n---\n",
			after:  "---\ntype: old\ntitle: T\n---\n",
			rule:   document.RuleType,
			same:   true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			was, now := parse(t, tc.before, voc), parse(t, tc.after, voc)
			i := slices.IndexFunc(now.Findings, func(f document.Finding) bool { return f.Rule == tc.rule })
			if i < 0 {
				t.Fatalf("no %s finding in %+v", tc.rule, now.Findings)
			}
			if got := slices.ContainsFunc(was.Findings, now.Findings[i].Same); got != tc.same {
				t.Errorf("%+v in %+v = %v, want %v", now.Findings[i], was.Findings, got, tc.same)
			}
		})
	}
}
