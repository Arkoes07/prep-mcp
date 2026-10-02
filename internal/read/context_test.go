package read_test

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Arkoes07/prep-mcp/internal/testvault"
)

var update = flag.Bool("update", false, "rewrite the golden files from the current output")

var (
	day       = testvault.Day
	dayNoWeek = time.Date(2026, 9, 29, 9, 0, 0, 0, time.Local) // 2026-W40: no week doc
)

// golden compares got with testdata/<name>. The golden files are written by
// hand and are the specification of the output format; -update regenerates
// them only when the format is changed on purpose.
func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s (run with -update only if the format change is intended)\n--- got ---\n%s\n--- want ---\n%s", p, got, want)
	}
}

func TestContextGolden(t *testing.T) {
	t.Run("with_week", func(t *testing.T) {
		_, r := copyOpen(t)
		got, err := r.Context(day)
		if err != nil {
			t.Fatalf("Context: %v", err)
		}
		golden(t, "context.golden", got)

		again, err := r.Context(day)
		if err != nil || again != got {
			t.Errorf("a second run differs from the first (err %v)", err)
		}
		if len(got) >= 16*1024 {
			t.Errorf("output is %d bytes, want under 16 KB", len(got))
		}

		// Sections appear in PRD order: rules, week, open items, recent, tags.
		header := regexp.MustCompile(`(?m)^==== (.+) ====$`)
		var names []string
		for _, m := range header.FindAllStringSubmatch(got, -1) {
			names = append(names, m[1])
		}
		want := []string{"CLAUDE.md", "Current week: 2026-W03", "Open items", "Recent", "Tags"}
		if !slices.Equal(names, want) {
			t.Errorf("section order = %q, want %q", names, want)
		}
	})

	t.Run("no_week_doc", func(t *testing.T) {
		_, r := copyOpen(t)
		got, err := r.Context(dayNoWeek)
		if err != nil {
			t.Fatalf("Context: %v", err)
		}
		golden(t, "context-noweek.golden", got)
	})

	t.Run("without_rules_file", func(t *testing.T) {
		root, r := copyOpen(t)
		if err := os.Remove(filepath.Join(root, "CLAUDE.md")); err != nil {
			t.Fatal(err)
		}
		got, err := r.Context(day)
		if err != nil {
			t.Fatalf("Context without CLAUDE.md: %v", err)
		}
		full, err := os.ReadFile("testdata/context.golden")
		if err != nil {
			t.Fatal(err)
		}
		// Same output as the golden file, minus its first section.
		_, rest, ok := strings.Cut(string(full), "==== Current week")
		if !ok {
			t.Fatal("context.golden has no week section")
		}
		if want := "==== Current week" + rest; got != want {
			t.Errorf("output without CLAUDE.md:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})
}

// The attributes after the title are the date, marked when it is the file's
// modification time, then the configured summary fields in config order.
func TestContextSummaryFields(t *testing.T) {
	const line = "- improvements/missing-tradeoff.md | Trade-off not stated | "
	tests := []struct {
		name, fields, want string
	}{
		{
			name:   "configured",
			fields: "summary_fields: [exit_criteria, status]",
			want: line + "date=2026-01-10 date_source=mtime " +
				"exit_criteria=Cost stated in 5 project answers in a row status=improving | -",
		},
		{
			name:   "none",
			fields: "",
			want:   line + "date=2026-01-10 date_source=mtime | -",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := copyOpen(t)
			replaceInFile(t, root, "prep.config.yaml", "summary_fields: [kind, stage, status]", tc.fields)
			got, err := openAt(t, root).Context(day)
			if err != nil {
				t.Fatalf("Context: %v", err)
			}
			if !strings.Contains(got, tc.want+"\n") {
				t.Errorf("output lacks the line\n%s\n--- got ---\n%s", tc.want, got)
			}
		})
	}
}

// TestCurrentWeek checks how the current week doc is found. The vault API has
// no separate accessor, so it looks at the "path:" line of the week section
// of Context, and at the Problems section.
func TestCurrentWeek(t *testing.T) {
	const week = "weeks/2026-w03.md"
	const noWeekLine = "no week doc for 2026-W03 (create_doc type=week with week: 2026-W03)"

	tests := []struct {
		name  string
		day   time.Time
		setup func(t *testing.T, root string)
		// wantPath is the expected "path:" line; empty means the "no week doc" line instead.
		wantPath string
		wantNone string // the exact "no week doc" line, when wantPath is empty
		problems []string
	}{
		{
			name:     "example",
			day:      day,
			setup:    func(t *testing.T, root string) {},
			wantPath: week,
		},
		{
			name: "filename_is_ignored",
			day:  day,
			setup: func(t *testing.T, root string) {
				rename(t, root, week, "weeks/My Week.md")
			},
			wantPath: "weeks/My Week.md",
		},
		{
			name: "value_case_insensitive",
			day:  day,
			setup: func(t *testing.T, root string) {
				replaceInFile(t, root, week, "week: 2026-W03", "week: 2026-w03")
			},
			wantPath: week,
		},
		{
			name: "value_trimmed",
			day:  day,
			setup: func(t *testing.T, root string) {
				replaceInFile(t, root, week, "week: 2026-W03", `week: " 2026-W03 "`)
			},
			wantPath: week,
		},
		{
			name: "two_docs_first_by_path_wins",
			day:  day,
			setup: func(t *testing.T, root string) {
				b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(week)))
				if err != nil {
					t.Fatal(err)
				}
				testvault.Write(t, root, "weeks/dup.md", string(b))
			},
			wantPath: week,
			problems: []string{"weeks/dup.md"},
		},
		{
			name: "other_type_ignored",
			day:  day,
			setup: func(t *testing.T, root string) {
				if err := os.Remove(filepath.Join(root, filepath.FromSlash(week))); err != nil {
					t.Fatal(err)
				}
				testvault.Write(t, root, "notes/w.md", "---\ntype: learning\ntitle: T\ntrack: t\nweek: 2026-W03\n---\nb\n")
			},
			wantNone: noWeekLine,
		},
		{
			name:     "next_week_has_none",
			day:      time.Date(2026, 1, 19, 9, 0, 0, 0, time.Local),
			setup:    func(t *testing.T, root string) {},
			wantNone: "no week doc for 2026-W04 (create_doc type=week with week: 2026-W04)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := testvault.Copy(t)
			tc.setup(t, root)
			r := openAt(t, root)
			got, err := r.Context(tc.day)
			if err != nil {
				t.Fatalf("Context: %v", err)
			}
			if tc.wantPath != "" {
				if !strings.Contains(got, "\npath: "+tc.wantPath+"\n") {
					t.Errorf("output has no line %q\n%s", "path: "+tc.wantPath, got)
				}
			} else if !strings.Contains(got, "\n"+tc.wantNone+"\n") {
				t.Errorf("output has no line %q\n%s", tc.wantNone, got)
			}
			if len(tc.problems) == 0 {
				if strings.Contains(got, "==== Problems ====") {
					t.Errorf("unexpected Problems section\n%s", got)
				}
				return
			}
			_, problems, found := strings.Cut(got, "==== Problems ====")
			if !found {
				t.Fatalf("output has no Problems section\n%s", got)
			}
			for _, p := range tc.problems {
				if !strings.Contains(problems, p) {
					t.Errorf("Problems section does not name %s:\n%s", p, problems)
				}
			}
		})
	}
}

func rename(t *testing.T, root, from, to string) {
	t.Helper()
	if err := os.Rename(filepath.Join(root, filepath.FromSlash(from)), filepath.Join(root, filepath.FromSlash(to))); err != nil {
		t.Fatal(err)
	}
}

func replaceInFile(t *testing.T, root, rel, old, new string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, old) {
		t.Fatalf("%s does not contain %q", rel, old)
	}
	if err := os.WriteFile(p, []byte(strings.Replace(s, old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}
