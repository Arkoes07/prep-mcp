package read_test

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/arkoes07/prep-mcp/internal/config"
	"github.com/arkoes07/prep-mcp/internal/read"
	"github.com/arkoes07/prep-mcp/internal/testvault"
	"github.com/arkoes07/prep-mcp/internal/vault"
)

type listCase struct {
	Name     string            `yaml:"name"`
	Type     string            `yaml:"type"`
	Tags     []string          `yaml:"tags"`
	AnyTags  []string          `yaml:"any_tags"`
	Where    config.Conditions `yaml:"where"`
	Since    string            `yaml:"since"`
	Until    string            `yaml:"until"`
	Sort     string            `yaml:"sort"`
	Limit    int               `yaml:"limit"`
	Want     []string          `yaml:"want"`
	Err      []string          `yaml:"err"`
	ParseErr bool              `yaml:"parse_err"`
}

func TestList(t *testing.T) {
	b, err := os.ReadFile("testdata/queries.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Cases []listCase `yaml:"cases"`
	}
	if err := yaml.Unmarshal(b, &f); err != nil {
		t.Fatalf("testdata/queries.yaml: %v", err)
	}
	if len(f.Cases) == 0 {
		t.Fatal("testdata/queries.yaml has no cases")
	}

	_, r := copyOpen(t)
	for _, tc := range f.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			flt := read.Filter{
				Type:    tc.Type,
				Tags:    tc.Tags,
				AnyTags: tc.AnyTags,
				Sort:    tc.Sort,
				Limit:   tc.Limit,
				Where:   tc.Where,
			}
			if tc.Since != "" {
				d, err := vault.ParseDate(tc.Since)
				if tc.ParseErr {
					if err == nil {
						t.Fatalf("ParseDate(%q) = %v, want an error", tc.Since, d)
					}
					return
				}
				if err != nil {
					t.Fatalf("ParseDate(since %q): %v", tc.Since, err)
				}
				flt.Since = &d
			}
			if tc.Until != "" {
				d, err := vault.ParseDate(tc.Until)
				if err != nil {
					t.Fatalf("ParseDate(until %q): %v", tc.Until, err)
				}
				flt.Until = &d
			}

			docs, problems, err := r.List(flt)
			if len(tc.Err) > 0 {
				if err == nil {
					t.Fatalf("List = %v, want an error containing %q", paths(docs), tc.Err)
				}
				for _, s := range tc.Err {
					if !strings.Contains(err.Error(), s) {
						t.Errorf("error %q does not contain %q", err, s)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(problems) != 0 {
				t.Errorf("problems = %v, want none", problems)
			}
			if got := paths(docs); !slices.Equal(got, tc.Want) {
				t.Errorf("paths:\n got  %v\n want %v", got, tc.Want)
			}
		})
	}
}

// The 500-document cap cannot be seen on the 8-document example, so it is
// checked here on synthetic documents. Apply is pure and needs no vault.
func TestApplyLimitCap(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
	docs := make([]*read.Doc, 600)
	for i := range docs {
		docs[i] = &read.Doc{
			Path: fmt.Sprintf("n/%04d.md", i),
			Type: "note",
			Date: base.Add(time.Duration(i) * time.Hour),
		}
	}
	tests := []struct {
		limit, want int
	}{
		{0, 50},
		{1, 1},
		{500, 500},
		{9999, 500},
	}
	for _, tc := range tests {
		got, err := read.Filter{Limit: tc.limit}.Apply(docs)
		if err != nil {
			t.Fatalf("Apply(limit %d): %v", tc.limit, err)
		}
		if len(got) != tc.want {
			t.Errorf("Apply(limit %d) returned %d docs, want %d", tc.limit, len(got), tc.want)
		}
	}
	// Newest first by default: the last synthetic doc leads.
	got, err := read.Filter{Limit: 1}.Apply(docs)
	if err != nil || len(got) != 1 || got[0].Path != "n/0599.md" {
		t.Errorf("Apply(limit 1) = %v, %v; want n/0599.md", paths(got), err)
	}
}

// where lists exactly the documents group_by counts under a value: both read
// a field through vault.Values, so tags compare normalised and an alias reads
// as what it points to.
func TestWhereMatchesWhatGroupByCounts(t *testing.T) {
	root, r := copyOpen(t)
	for name, tags := range map[string]string{
		"list":   "tags: [A]",
		"string": `tags: "a, b"`,
		"alias":  "base: &t B\ntags: *t",
	} {
		testvault.Write(t, root, "probe/"+name+".md", "---\ntype: probe\n"+tags+"\n---\n")
	}

	groups, _, err := r.Group(read.Filter{Type: "probe"}, []string{"tags"})
	if err != nil {
		t.Fatalf("Group: %v", err)
	}
	wantGroups := []read.Group{{Values: []string{"a"}, Count: 2}, {Values: []string{"b"}, Count: 2}}
	if !slices.EqualFunc(groups, wantGroups, func(a, b read.Group) bool {
		return a.Count == b.Count && slices.Equal(a.Values, b.Values)
	}) {
		t.Fatalf("group_by tags = %v, want %v", groups, wantGroups)
	}
	for tag, want := range map[string][]string{
		"a": {"probe/list.md", "probe/string.md"},
		"b": {"probe/alias.md", "probe/string.md"},
	} {
		docs, _, err := r.List(read.Filter{Type: "probe", Where: map[string][]string{"tags": {tag}}})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if got := slices.Sorted(slices.Values(paths(docs))); !slices.Equal(got, want) {
			t.Errorf("where tags: %q matched %v, want %v", tag, got, want)
		}
	}
}

func TestMatchTag(t *testing.T) {
	tests := []struct {
		pattern, tag string
		want         bool
	}{
		{"issue/", "issue/x", true},
		{"issue/", "issues/x", false},
		{"issue", "issue/x", false},
		{"issue/x", "issue/x", true},
		{"Issue/", "issue/x", true},
	}
	for _, tc := range tests {
		if got := read.MatchTag(tc.pattern, tc.tag); got != tc.want {
			t.Errorf("MatchTag(%q, %q) = %v, want %v", tc.pattern, tc.tag, got, tc.want)
		}
	}
}
