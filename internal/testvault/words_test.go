package testvault_test

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/Arkoes07/prep-mcp/internal/testvault"
)

// TestNoDomainWords walks every .go file in the repository, tests included,
// and fails on any whole-word, case-insensitive match of a word listed in
// the forbidden word list (see ForbiddenWords).
func TestNoDomainWords(t *testing.T) {
	re := testvault.ForbiddenWords(t)

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
