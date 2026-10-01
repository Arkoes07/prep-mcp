package vault_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Arkoes07/prep-mcp/internal/testvault"
	"github.com/Arkoes07/prep-mcp/internal/vault"
)

type scanDoc struct {
	Path     string            `yaml:"path"`
	Type     string            `yaml:"type"`
	Title    string            `yaml:"title"`
	Date     string            `yaml:"date"`
	Tags     []string          `yaml:"tags"`
	BodyLine int               `yaml:"body_line"`
	Fields   map[string]string `yaml:"fields"`
}

func loadScanDocs(t *testing.T) []scanDoc {
	t.Helper()
	b, err := os.ReadFile("testdata/scan.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Docs []scanDoc `yaml:"docs"`
	}
	if err := yaml.Unmarshal(b, &f); err != nil {
		t.Fatalf("testdata/scan.yaml: %v", err)
	}
	if len(f.Docs) == 0 {
		t.Fatal("testdata/scan.yaml has no docs")
	}
	return f.Docs
}

func TestScan(t *testing.T) {
	want := loadScanDocs(t)
	wantPaths := make([]string, len(want))
	for i, w := range want {
		wantPaths[i] = w.Path
	}
	slices.Sort(wantPaths)

	t.Run("example", func(t *testing.T) {
		root, v := copyOpen(t)
		docs, problems, err := v.Scan()
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if len(problems) != 0 {
			t.Errorf("problems on the pristine example: %v", problems)
		}
		got := paths(docs)
		slices.Sort(got)
		if !slices.Equal(got, wantPaths) {
			t.Fatalf("scanned paths:\n got  %v\n want %v", got, wantPaths)
		}
		byPath := map[string]*vault.Doc{}
		for _, d := range docs {
			byPath[d.Path] = d
		}
		for _, w := range want {
			d := byPath[w.Path]
			if d.Type != w.Type {
				t.Errorf("%s: Type = %q, want %q", w.Path, d.Type, w.Type)
			}
			if d.Title != w.Title {
				t.Errorf("%s: Title = %q, want %q", w.Path, d.Title, w.Title)
			}
			if !slices.Equal(d.Tags, w.Tags) {
				t.Errorf("%s: Tags = %q, want %q", w.Path, d.Tags, w.Tags)
			}
			if w.Date == "" {
				if !d.DateFromMtime || !d.Date.Equal(testvault.Mtime) {
					t.Errorf("%s: Date = %v (from mtime %v), want mtime %v", w.Path, d.Date, d.DateFromMtime, testvault.Mtime)
				}
			} else if d.DateFromMtime || d.Date.Format(time.DateOnly) != w.Date {
				t.Errorf("%s: Date = %s (from mtime %v), want %s from the date field", w.Path, d.Date.Format(time.DateOnly), d.DateFromMtime, w.Date)
			}
			for k, wantVal := range w.Fields {
				n := d.Fields[k]
				if n == nil || n.Value != wantVal {
					t.Errorf("%s: Fields[%q] = %v, want %q", w.Path, k, n, wantVal)
				}
			}
			if w.BodyLine != 0 {
				if d.BodyLine != w.BodyLine {
					t.Errorf("%s: BodyLine = %d, want %d", w.Path, d.BodyLine, w.BodyLine)
				}
				// BodyLine must point at the first line of Body in the file.
				file, err := os.ReadFile(filepath.Join(root, w.Path))
				if err != nil {
					t.Fatal(err)
				}
				lines := strings.Split(string(file), "\n")
				first, _, _ := strings.Cut(string(d.Body), "\n")
				if w.BodyLine > len(lines) || lines[w.BodyLine-1] != first {
					t.Errorf("%s: line %d of the file is not the first body line %q", w.Path, w.BodyLine, first)
				}
			}
		}
	})

	const learning = "---\ntype: learning\ntitle: T\ntrack: t\n---\nbody\n"
	extra := []struct {
		name  string
		setup func(t *testing.T, root string)
		check func(t *testing.T, root string, v *vault.Vault, docs []*vault.Doc, problems []vault.Problem)
	}{
		{
			name: "dot_folder_excluded",
			setup: func(t *testing.T, root string) {
				testvault.Write(t, root, ".obsidian/x.md", learning)
			},
			check: excludes(".obsidian/x.md"),
		},
		{
			name: "dot_file_excluded",
			setup: func(t *testing.T, root string) {
				testvault.Write(t, root, ".hidden.md", learning)
			},
			check: excludes(".hidden.md"),
		},
		{
			name: "nested_folder_included",
			setup: func(t *testing.T, root string) {
				testvault.Write(t, root, "improvements/sub/x.md", "---\ntype: improvement\nstatus: open\nexit_criteria: e\n---\nb\n")
			},
			check: func(t *testing.T, _ string, _ *vault.Vault, docs []*vault.Doc, _ []vault.Problem) {
				if !slices.Contains(paths(docs), "improvements/sub/x.md") {
					t.Errorf("improvements/sub/x.md missing from %v", paths(docs))
				}
			},
		},
		{
			name: "no_frontmatter_excluded_but_readable",
			setup: func(t *testing.T, root string) {
				testvault.Write(t, root, "notes/nofm.md", "# just text\n")
			},
			check: func(t *testing.T, _ string, v *vault.Vault, docs []*vault.Doc, problems []vault.Problem) {
				if slices.Contains(paths(docs), "notes/nofm.md") {
					t.Error("a file without frontmatter was scanned as a doc")
				}
				if len(problems) != 0 {
					t.Errorf("problems = %v, want none", problems)
				}
				if _, content, _, err := v.Read("notes/nofm.md"); err != nil || string(content) != "# just text\n" {
					t.Errorf("Read(notes/nofm.md) = %q, %v; want the file, still readable by path", content, err)
				}
			},
		},
		{
			name: "no_type_excluded_silently",
			setup: func(t *testing.T, root string) {
				testvault.Write(t, root, "notes/notype.md", "---\ntitle: T\n---\nb\n")
			},
			check: func(t *testing.T, _ string, _ *vault.Vault, docs []*vault.Doc, problems []vault.Problem) {
				if slices.Contains(paths(docs), "notes/notype.md") {
					t.Error("a file without a type was scanned as a doc")
				}
				if len(problems) != 0 {
					t.Errorf("problems = %v, want none", problems)
				}
			},
		},
		{
			name: "invalid_yaml_reported",
			setup: func(t *testing.T, root string) {
				// The tab on line 3 of the file is the error.
				testvault.Write(t, root, "notes/bad.md", "---\ntype: learning\n\tbad: 1\n---\nb\n")
			},
			check: func(t *testing.T, _ string, _ *vault.Vault, docs []*vault.Doc, problems []vault.Problem) {
				if slices.Contains(paths(docs), "notes/bad.md") {
					t.Error("a file with invalid YAML was scanned as a doc")
				}
				if len(problems) != 1 || problems[0].Path != "notes/bad.md" || !strings.Contains(problems[0].Msg, "line 3") {
					t.Errorf("problems = %+v, want one for notes/bad.md naming file line 3", problems)
				}
			},
		},
		{
			name: "symlinked_dir_not_followed",
			setup: func(t *testing.T, root string) {
				outside := filepath.Join(filepath.Dir(root), "outside")
				testvault.Write(t, outside, "x.md", learning)
				mustSymlink(t, outside, filepath.Join(root, "linkdir"))
			},
			check: excludes("linkdir/x.md"),
		},
		{
			name: "symlinked_file_skipped",
			setup: func(t *testing.T, root string) {
				mustSymlink(t, "weeks/2026-w03.md", filepath.Join(root, "link-in.md"))
			},
			check: func(t *testing.T, _ string, _ *vault.Vault, docs []*vault.Doc, _ []vault.Problem) {
				got := paths(docs)
				if slices.Contains(got, "link-in.md") {
					t.Error("a symlink was scanned as a doc")
				}
				n := 0
				for _, p := range got {
					if p == "weeks/2026-w03.md" {
						n++
					}
				}
				if n != 1 {
					t.Errorf("weeks/2026-w03.md appears %d times, want 1", n)
				}
			},
		},
		{
			name: "quoted_date",
			setup: func(t *testing.T, root string) {
				testvault.Write(t, root, "notes/q.md", "---\ntype: learning\ntitle: T\ntrack: t\ndate: \"2026-01-12\"\n---\nb\n")
			},
			check: func(t *testing.T, _ string, _ *vault.Vault, docs []*vault.Doc, problems []vault.Problem) {
				d := find(t, docs, "notes/q.md")
				if d.DateFromMtime || d.Date.Format(time.DateOnly) != "2026-01-12" {
					t.Errorf("Date = %s (from mtime %v), want 2026-01-12 from the field", d.Date.Format(time.DateOnly), d.DateFromMtime)
				}
				if len(problems) != 0 {
					t.Errorf("problems = %v, want none", problems)
				}
			},
		},
		{
			name: "unparseable_date_falls_back_and_reports",
			setup: func(t *testing.T, root string) {
				testvault.Write(t, root, "notes/d.md", "---\ntype: learning\ntitle: T\ntrack: t\ndate: garbage\n---\nb\n")
			},
			check: func(t *testing.T, _ string, _ *vault.Vault, docs []*vault.Doc, problems []vault.Problem) {
				d := find(t, docs, "notes/d.md")
				if !d.DateFromMtime || !d.Date.Equal(testvault.Mtime) {
					t.Errorf("Date = %v (from mtime %v), want mtime %v", d.Date, d.DateFromMtime, testvault.Mtime)
				}
				if len(problems) != 1 || problems[0].Path != "notes/d.md" || !strings.Contains(problems[0].Msg, "date") {
					t.Errorf("problems = %+v, want one for notes/d.md mentioning date", problems)
				}
			},
		},
	}
	for _, tc := range extra {
		t.Run(tc.name, func(t *testing.T) {
			root := testvault.Copy(t)
			tc.setup(t, root)
			v := openAt(t, root)
			docs, problems, err := v.Scan()
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}
			tc.check(t, root, v, docs, problems)
		})
	}
}

func excludes(p string) func(*testing.T, string, *vault.Vault, []*vault.Doc, []vault.Problem) {
	return func(t *testing.T, _ string, _ *vault.Vault, docs []*vault.Doc, _ []vault.Problem) {
		t.Helper()
		if slices.Contains(paths(docs), p) {
			t.Errorf("%s was scanned, want it excluded", p)
		}
	}
}

func find(t *testing.T, docs []*vault.Doc, p string) *vault.Doc {
	t.Helper()
	for _, d := range docs {
		if d.Path == p {
			return d
		}
	}
	t.Fatalf("%s missing from %v", p, paths(docs))
	return nil
}
