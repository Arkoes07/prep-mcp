package testvault

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// ForbiddenWords returns a whole-word, case-insensitive matcher for the words
// in testdata/forbidden-words.txt, which must not appear in Go code or in what
// the server shows the model. The words live in data so no .go file names them.
// Types come from the config, never the code, so the server works for any vault.
func ForbiddenWords(t testing.TB) *regexp.Regexp {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "testdata", "forbidden-words.txt")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("testvault.ForbiddenWords: %v", err)
	}
	var words []string
	for line := range strings.SplitSeq(string(b), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			words = append(words, regexp.QuoteMeta(line))
		}
	}
	if len(words) == 0 {
		t.Fatalf("testvault.ForbiddenWords: %s has no words; checks would pass vacuously. Add one word per line", path)
	}
	return regexp.MustCompile(`(?i)\b(` + strings.Join(words, "|") + `)\b`)
}
