package read_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/arkoes07/prep-mcp/internal/read"
	"github.com/arkoes07/prep-mcp/internal/testvault"
)

type tagRow struct {
	tag   string
	count int
	last  string // YYYY-MM-DD
}

// Tag counts on the example: each document counts once per tag. Documents
// without their type's date field are dated by mtime (2026-01-10); the one
// company/acme document is dated by its type's date field (2026-01-05).
var exampleTagRows = []tagRow{
	{"q/hardest-project", 3, "2026-01-14"},
	{"issue/missing-tradeoff", 2, "2026-01-14"},
	{"company/acme", 1, "2026-01-05"},
	{"domain/payments", 1, "2026-01-10"},
	{"issue/buries-headline", 1, "2026-01-12"},
	{"meta/plan", 1, "2026-01-10"},
	{"skill/scale", 1, "2026-01-10"},
	{"skill/trade-off", 1, "2026-01-10"},
	{"q/changed-mind", 0, ""},
	{"q/failure", 0, ""},
}

func checkRows(t *testing.T, got []read.TagCount, want []tagRow) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d rows %v, want %d %v", len(got), got, len(want), want)
	}
	for i, w := range want {
		g := got[i]
		last := ""
		if !g.Last.IsZero() {
			last = g.Last.Format(time.DateOnly)
		}
		if g.Tag != w.tag || g.Count != w.count || last != w.last {
			t.Errorf("row %d = {%s %d %s}, want {%s %d %s}", i, g.Tag, g.Count, last, w.tag, w.count, w.last)
		}
	}
}

func TestTagStats(t *testing.T) {
	t.Run("no_prefix_sorted_by_count_then_name", func(t *testing.T) {
		_, r := copyOpen(t)
		got, err := r.TagStats("")
		if err != nil {
			t.Fatal(err)
		}
		checkRows(t, got, exampleTagRows)
		for _, g := range got {
			if g.Unknown {
				t.Errorf("%s is marked unknown, but every example tag is in the vocabulary", g.Tag)
			}
		}
	})

	t.Run("prefix", func(t *testing.T) {
		_, r := copyOpen(t)
		got, err := r.TagStats("issue/")
		if err != nil {
			t.Fatal(err)
		}
		checkRows(t, got, []tagRow{
			{"issue/missing-tradeoff", 2, "2026-01-14"},
			{"issue/buries-headline", 1, "2026-01-12"},
		})
	})

	t.Run("prefix_covers_unused_vocabulary", func(t *testing.T) {
		_, r := copyOpen(t)
		got, err := r.TagStats("q/")
		if err != nil {
			t.Fatal(err)
		}
		checkRows(t, got, []tagRow{
			{"q/hardest-project", 3, "2026-01-14"},
			{"q/changed-mind", 0, ""},
			{"q/failure", 0, ""},
		})
	})

	const typo = "---\ntype: learning\ntitle: T\ntrack: t\ntags: [issue/typo]\n---\nb\n"

	t.Run("unknown_tag_marked", func(t *testing.T) {
		root := testvault.Copy(t)
		testvault.Write(t, root, "notes/typo.md", typo)
		r := openAt(t, root)
		got, err := r.TagStats("issue/")
		if err != nil {
			t.Fatal(err)
		}
		unknown := map[string]bool{}
		for _, g := range got {
			unknown[g.Tag] = g.Unknown
		}
		if len(got) != 3 || !unknown["issue/typo"] || unknown["issue/missing-tradeoff"] || unknown["issue/buries-headline"] {
			t.Errorf("rows = %+v, want issue/typo (and only it) marked unknown", got)
		}
	})

	t.Run("no_vocabulary_marks_nothing", func(t *testing.T) {
		root := testvault.Copy(t)
		testvault.Write(t, root, "notes/typo.md", typo)
		if err := os.Remove(filepath.Join(root, "tags.md")); err != nil {
			t.Fatal(err)
		}
		r := openAt(t, root)
		got, err := r.TagStats("")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) == 0 {
			t.Fatal("no rows")
		}
		for _, g := range got {
			if g.Unknown {
				t.Errorf("%s is marked unknown, but there is no tags.md", g.Tag)
			}
			if g.Count == 0 {
				t.Errorf("%s has count 0, but without tags.md only tags in use are listed", g.Tag)
			}
		}
	})
}
