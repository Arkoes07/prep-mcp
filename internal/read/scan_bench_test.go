package read_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arkoes07/prep-mcp/internal/testvault"
)

// BenchmarkScan measures one full scan of a generated vault. It is the number
// behind Scan having no index: add a cache only if this is measured slow.
func BenchmarkScan(b *testing.B) {
	const docs = 2000
	dir := b.TempDir()
	testvault.Write(b, dir, "prep.config.yaml", "types:\n  note:\n    folder: notes\n    filename: \"{slug}.md\"\n")

	body := strings.Repeat("A line of body text that is about as long as a typical sentence in a note.\n", 15)
	for i := range docs {
		// 20 folders of 100 files, a mix of dated and undated documents.
		rel := fmt.Sprintf("notes/group-%02d/doc-%04d.md", i%20, i)
		fm := fmt.Sprintf("---\ntype: note\ntitle: Note number %d\ntags: [topic/a%d, topic/b%d, area/c]\nstatus: open\n", i, i%7, i%13)
		if i%2 == 0 {
			fm += fmt.Sprintf("date: 2026-01-%02d\n", i%28+1)
		}
		testvault.Write(b, dir, rel, fm+"---\n# Note "+fmt.Sprint(i)+"\n"+body)
	}
	// Dot-folders and templates are skipped by the scan but still walked past.
	testvault.Write(b, dir, ".obsidian/workspace.md", "---\ntype: note\n---\n")
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o755); err != nil {
		b.Fatal(err)
	}
	r := openAt(b, dir)

	got, problems, err := r.Scan()
	if err != nil {
		b.Fatal(err)
	}
	if len(got) != docs || len(problems) != 0 {
		b.Fatalf("scan found %d docs and %d problems, want %d and 0", len(got), len(problems), docs)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, _, err := r.Scan(); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(docs, "docs")
}

// BenchmarkScanExample measures one full scan of examples/vault, a small vault.
func BenchmarkScanExample(b *testing.B) {
	_, r := copyOpen(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, _, err := r.Scan(); err != nil {
			b.Fatal(err)
		}
	}
}
