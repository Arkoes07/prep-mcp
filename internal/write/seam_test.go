package write

// White-box tests: they use the beforeRename seam to fail or interfere at the
// one moment black-box tests cannot reach, between writing the temp file and
// renaming it into place.

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Arkoes07/prep-mcp/internal/testvault"
	"github.com/Arkoes07/prep-mcp/internal/vault"
)

func seamEnv(t *testing.T) (string, *Writer) {
	t.Helper()
	root := testvault.Copy(t)
	cfg, v := testvault.Open(t, root)
	return root, New(v, cfg, nil, testvault.Clock())
}

func setSeam(t *testing.T, f func(tmp string) error) {
	t.Helper()
	beforeRename = f
	t.Cleanup(func() { beforeRename = nil })
}

func tempFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && strings.HasPrefix(d.Name(), ".prep-tmp-") {
			out = append(out, p)
		}
		return nil
	})
	return out
}

func TestFailureBeforeRenameLeavesNothingBehind(t *testing.T) {
	const p = "weeks/2026-w03.md"
	root, w := seamEnv(t)
	orig, err := os.ReadFile(filepath.Join(root, p))
	if err != nil {
		t.Fatal(err)
	}
	setSeam(t, func(tmp string) error {
		if _, err := os.Stat(filepath.Join(root, tmp)); err != nil {
			t.Errorf("the temp file should exist at the seam: %v", err)
		}
		return errors.New("injected failure")
	})

	if _, err := w.AppendSection(context.Background(), p, "Log", "- Tue: x"); err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("AppendSection error = %v, want the injected failure", err)
	}
	if _, err := w.Create(context.Background(), Draft{Type: "reference", Frontmatter: nodeOf(t, `{"title":"T"}`), Body: "x"}); err == nil {
		t.Fatal("Create succeeded despite the injected failure")
	}
	if got, _ := os.ReadFile(filepath.Join(root, p)); string(got) != string(orig) {
		t.Errorf("the target changed:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(root, "reference/t.md")); err == nil {
		t.Error("the new file appeared")
	}
	if left := tempFiles(t, root); len(left) > 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

// A human saving the file between our read and our rename must not be lost:
// the write starts over from the new content.
func TestHumanEditDuringWriteIsKept(t *testing.T) {
	const p = "weeks/2026-w03.md"
	root, w := seamEnv(t)
	target := filepath.Join(root, p)
	orig, _ := os.ReadFile(target)

	calls := 0
	setSeam(t, func(string) error {
		calls++
		if calls == 1 {
			return os.WriteFile(target, append(append([]byte{}, orig...), "- human edit\n"...), 0o644)
		}
		return nil
	})
	res, err := w.AppendSection(context.Background(), p, "Log", "- Tue: x")
	if err != nil {
		t.Fatalf("AppendSection: %v", err)
	}
	got, _ := os.ReadFile(target)
	if want := string(orig) + "- human edit\n- Tue: x\n"; string(got) != want {
		t.Errorf("file:\n%q\nwant:\n%q", got, want)
	}
	if res.Hash != vault.Hash(got) {
		t.Errorf("hash = %s, want the hash of the final file", res.Hash)
	}
	if left := tempFiles(t, root); len(left) > 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

// If the file keeps changing, give up without writing rather than loop.
func TestGivesUpWhenFileKeepsChanging(t *testing.T) {
	const p = "weeks/2026-w03.md"
	root, w := seamEnv(t)
	target := filepath.Join(root, p)
	n := 0
	setSeam(t, func(string) error {
		n++
		b, _ := os.ReadFile(target)
		return os.WriteFile(target, append(b, "- edit\n"...), 0o644)
	})
	_, err := w.AppendSection(context.Background(), p, "Log", "- Tue: x")
	if err == nil || !strings.Contains(err.Error(), "re-read it with read_doc") {
		t.Fatalf("error = %v, want a retry hint", err)
	}
	if n != 2 {
		t.Errorf("attempts = %d, want 2", n)
	}
	if b, _ := os.ReadFile(target); strings.Contains(string(b), "Tue: x") {
		t.Error("the AI's text was written over a changing file")
	}
	if left := tempFiles(t, root); len(left) > 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

// replace_doc has no retry: a change after the hash check is a conflict.
func TestReplaceConflictAtRename(t *testing.T) {
	const p = "weeks/2026-w03.md"
	root, w := seamEnv(t)
	target := filepath.Join(root, p)
	orig, _ := os.ReadFile(target)
	setSeam(t, func(string) error {
		return os.WriteFile(target, append(append([]byte{}, orig...), "- human edit\n"...), 0o644)
	})
	_, err := w.Replace(context.Background(), p, string(orig)+"- ai\n", vault.Hash(orig))
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("error %T (%v), want *ConflictError", err, err)
	}
	if b, _ := os.ReadFile(target); strings.Contains(string(b), "- ai") {
		t.Error("the AI's content was written over the human's edit")
	}
	if left := tempFiles(t, root); len(left) > 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

func nodeOf(t *testing.T, js string) *yaml.Node {
	t.Helper()
	n, err := JSONToNode([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	return n
}
