package main

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoDomainWords walks every .go file in the repository, tests included,
// and fails on any whole-word, case-insensitive match of a word listed in
// testdata/forbidden-words.txt. The words live in data so this file stays clean.
func TestNoDomainWords(t *testing.T) {
	words := readWords(t, "testdata/forbidden-words.txt")
	if len(words) == 0 {
		t.Fatal("testdata/forbidden-words.txt has no words; the check would pass vacuously. Add one word per line")
	}
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = regexp.QuoteMeta(w)
	}
	re := regexp.MustCompile(`(?i)\b(` + strings.Join(quoted, "|") + `)\b`)

	root := filepath.Join("..", "..")
	var checked int
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(p) != ".go" {
			return nil
		}
		checked++
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for n := 1; sc.Scan(); n++ {
			if m := re.FindString(sc.Text()); m != "" {
				t.Errorf("%s:%d: forbidden word %q. Go code must stay domain-neutral; move example-specific names into testdata or config", filepath.ToSlash(p), n, m)
			}
		}
		return sc.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("no .go files found; the walk root is wrong")
	}
}

func readWords(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read word list: %v", err)
	}
	var words []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		words = append(words, line)
	}
	return words
}
