package vault_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Arkoes07/prep-mcp/internal/testvault"
	"github.com/Arkoes07/prep-mcp/internal/vault"
)

type tagRow struct {
	tag   string
	count int
	last  string // YYYY-MM-DD
}

// Tag counts on the example: each document counts once per tag. Documents
// without a date field are dated by mtime (2026-01-10).
var exampleTagRows = []tagRow{
	{"q/hardest-project", 3, "2026-01-14"},
	{"issue/missing-tradeoff", 2, "2026-01-14"},
	{"company/acme", 1, "2026-01-10"},
	{"domain/payments", 1, "2026-01-10"},
	{"issue/buries-headline", 1, "2026-01-12"},
	{"meta/plan", 1, "2026-01-10"},
	{"skill/scale", 1, "2026-01-10"},
	{"skill/trade-off", 1, "2026-01-10"},
}

func checkRows(t *testing.T, got []vault.TagCount, want []tagRow) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d rows %v, want %d %v", len(got), got, len(want), want)
	}
	for i, w := range want {
		g := got[i]
		if g.Tag != w.tag || g.Count != w.count || g.Last.Format(time.DateOnly) != w.last {
			t.Errorf("row %d = {%s %d %s}, want {%s %d %s}", i, g.Tag, g.Count, g.Last.Format(time.DateOnly), w.tag, w.count, w.last)
		}
	}
}

func TestTagStats(t *testing.T) {
	t.Run("no_prefix_sorted_by_count_then_name", func(t *testing.T) {
		_, v := copyOpen(t)
		got, err := v.TagStats("", nil)
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
		_, v := copyOpen(t)
		got, err := v.TagStats("issue/", nil)
		if err != nil {
			t.Fatal(err)
		}
		checkRows(t, got, []tagRow{
			{"issue/missing-tradeoff", 2, "2026-01-14"},
			{"issue/buries-headline", 1, "2026-01-12"},
		})
	})

	t.Run("since_window", func(t *testing.T) {
		_, v := copyOpen(t)
		since := time.Date(2026, 1, 12, 0, 0, 0, 0, time.Local)
		got, err := v.TagStats("", &since)
		if err != nil {
			t.Fatal(err)
		}
		// Only the two dated documents are in the window; the rest are dated 01-10.
		checkRows(t, got, []tagRow{
			{"issue/missing-tradeoff", 2, "2026-01-14"},
			{"q/hardest-project", 2, "2026-01-14"},
			{"issue/buries-headline", 1, "2026-01-12"},
		})
	})

	t.Run("since_after_everything", func(t *testing.T) {
		_, v := copyOpen(t)
		since := time.Date(2026, 3, 1, 0, 0, 0, 0, time.Local).AddDate(0, 0, -30)
		got, err := v.TagStats("", &since)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Errorf("got %v, want none", got)
		}
	})

	const typo = "---\ntype: learning\ntitle: T\ntrack: t\ntags: [issue/typo]\n---\nb\n"

	t.Run("unknown_tag_marked", func(t *testing.T) {
		root := testvault.Copy(t)
		testvault.Write(t, root, "notes/typo.md", typo)
		v := openAt(t, root)
		got, err := v.TagStats("issue/", nil)
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
		v := openAt(t, root)
		got, err := v.TagStats("", nil)
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
		}
	})
}
