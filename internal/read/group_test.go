package read_test

import (
	"os"
	"slices"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Arkoes07/prep-mcp/internal/config"
	"github.com/Arkoes07/prep-mcp/internal/read"
	"github.com/Arkoes07/prep-mcp/internal/testvault"
)

func TestGroup(t *testing.T) {
	b, err := os.ReadFile("testdata/groups.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Cases []struct {
			Name    string            `yaml:"name"`
			Type    string            `yaml:"type"`
			Tags    []string          `yaml:"tags"`
			Where   config.Conditions `yaml:"where"`
			Until   string            `yaml:"until"`
			Since   string            `yaml:"since"`
			GroupBy []string          `yaml:"group_by"`
			Want    []read.Group      `yaml:"want"`
		} `yaml:"cases"`
	}
	if err := yaml.Unmarshal(b, &f); err != nil {
		t.Fatalf("testdata/groups.yaml: %v", err)
	}
	if len(f.Cases) == 0 {
		t.Fatal("testdata/groups.yaml has no cases")
	}

	_, r := copyOpen(t)
	for _, tc := range f.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			flt := read.Filter{Type: tc.Type, Tags: tc.Tags, Where: tc.Where}
			for _, bound := range []struct {
				text string
				dst  **time.Time
			}{{tc.Since, &flt.Since}, {tc.Until, &flt.Until}} {
				if bound.text == "" {
					continue
				}
				d, err := read.ParseDate(bound.text)
				if err != nil {
					t.Fatal(err)
				}
				*bound.dst = &d
			}
			got, problems, err := r.Group(flt, tc.GroupBy)
			if err != nil {
				t.Fatalf("Group: %v", err)
			}
			if len(problems) != 0 {
				t.Errorf("problems = %v, want none", problems)
			}
			if !slices.EqualFunc(got, tc.Want, func(a, b read.Group) bool {
				return a.Count == b.Count && slices.Equal(a.Values, b.Values)
			}) {
				t.Errorf("groups:\n got  %v\n want %v", got, tc.Want)
			}
		})
	}
}

func TestGroupReportsProblems(t *testing.T) {
	root, r := copyOpen(t)
	testvault.Write(t, root, "notes/broken.md", "---\ntype: [x\n---\n")

	_, problems, err := r.Group(read.Filter{}, []string{"type"})
	if err != nil {
		t.Fatalf("Group: %v", err)
	}
	if len(problems) != 1 || problems[0].Path != "notes/broken.md" {
		t.Errorf("problems = %v, want one for notes/broken.md", problems)
	}
}
