package vault_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Arkoes07/prep-mcp/internal/testvault"
)

type wantLine struct {
	N    int    `yaml:"n"`
	Text string `yaml:"text"`
}

type wantHit struct {
	Path  string     `yaml:"path"`
	Title string     `yaml:"title"`
	Total int        `yaml:"total"`
	Lines []wantLine `yaml:"lines"`
}

type searchCase struct {
	Name  string    `yaml:"name"`
	Query string    `yaml:"query"`
	Type  string    `yaml:"type"`
	Limit int       `yaml:"limit"`
	Want  []wantHit `yaml:"want"`
	Err   []string  `yaml:"err"`
}

func TestSearch(t *testing.T) {
	b, err := os.ReadFile("testdata/search.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Cases []searchCase `yaml:"cases"`
	}
	if err := yaml.Unmarshal(b, &f); err != nil {
		t.Fatalf("testdata/search.yaml: %v", err)
	}
	if len(f.Cases) == 0 {
		t.Fatal("testdata/search.yaml has no cases")
	}

	_, v := copyOpen(t)
	for _, tc := range f.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			hits, err := v.Search(tc.Query, tc.Type, tc.Limit)
			if len(tc.Err) > 0 {
				if err == nil {
					t.Fatalf("Search(%q) returned %d hits, want an error containing %q", tc.Query, len(hits), tc.Err)
				}
				for _, s := range tc.Err {
					if !strings.Contains(err.Error(), s) {
						t.Errorf("error %q does not contain %q", err, s)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			var got []string
			for _, h := range hits {
				got = append(got, h.Path)
			}
			var want []string
			for _, w := range tc.Want {
				want = append(want, w.Path)
			}
			if !slices.Equal(got, want) {
				t.Fatalf("hit paths:\n got  %v\n want %v", got, want)
			}
			for i, w := range tc.Want {
				h := hits[i]
				if w.Title != "" && h.Title != w.Title {
					t.Errorf("%s: Title = %q, want %q", w.Path, h.Title, w.Title)
				}
				if w.Total != 0 && h.Total != w.Total {
					t.Errorf("%s: Total = %d, want %d", w.Path, h.Total, w.Total)
				}
				if w.Lines == nil {
					continue
				}
				if len(h.Lines) != len(w.Lines) {
					t.Errorf("%s: %d lines %v, want %d", w.Path, len(h.Lines), h.Lines, len(w.Lines))
					continue
				}
				for j, wl := range w.Lines {
					if h.Lines[j].N != wl.N || h.Lines[j].Text != wl.Text {
						t.Errorf("%s: line %d = {%d %q}, want {%d %q}", w.Path, j, h.Lines[j].N, h.Lines[j].Text, wl.N, wl.Text)
					}
				}
			}
		})
	}
}

// A document reports at most 3 matching lines but counts all of them.
func TestSearchLineCap(t *testing.T) {
	root := testvault.Copy(t)
	// The frontmatter is 5 lines, so the body starts on line 6.
	testvault.Write(t, root, "notes/many.md",
		"---\ntype: learning\ntitle: Many\ntrack: t\n---\n"+
			"needle one\nfiller\nneedle two\nneedle three\nneedle four\nneedle five\n")
	v := openAt(t, root)

	hits, err := v.Search("needle", "", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].Path != "notes/many.md" {
		t.Fatalf("hits = %+v, want only notes/many.md", hits)
	}
	h := hits[0]
	if h.Total != 5 {
		t.Errorf("Total = %d, want 5", h.Total)
	}
	var got []int
	for _, l := range h.Lines {
		got = append(got, l.N)
	}
	if want := []int{6, 8, 9}; !slices.Equal(got, want) {
		t.Errorf("reported lines = %v, want the first three matches %v", got, want)
	}
}
