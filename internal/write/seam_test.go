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

	"github.com/arkoes07/prep-mcp/internal/testvault"
	"github.com/arkoes07/prep-mcp/internal/vault"
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

	_, err = w.Apply(context.Background(), AppendOp(p, "Log", "- Tue: x"))
	if err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("append error = %v, want the injected failure", err)
	}
	d := Draft{Type: "reference", Frontmatter: nodeOf(t, `{"title":"T"}`), Body: "x"}
	if _, err := w.Apply(context.Background(), CreateOp(d)); err == nil {
		t.Fatal("create succeeded despite the injected failure")
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
	res, err := w.Apply(context.Background(), AppendOp(p, "Log", "- Tue: x"))
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
	_, err := w.Apply(context.Background(), AppendOp(p, "Log", "- Tue: x"))
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
	_, err := w.Apply(context.Background(), ReplaceOp(p, string(orig)+"- ai\n", vault.Hash(orig)))
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

const (
	seamA = "improvements/buries-headline.md"
	seamB = "learning/ddia-ch5.md" // created by the batch
	seamC = "weeks/2026-w03.md"
	seamD = "improvements/missing-tradeoff.md"
)

func readAll(t *testing.T, root string, paths ...string) map[string]string {
	t.Helper()
	out := make(map[string]string, len(paths))
	for _, p := range paths {
		b, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			t.Fatal(err)
		}
		out[p] = string(b)
	}
	return out
}

// A write that fails partway through a batch puts back every file the batch
// already wrote, byte for byte and with its mode, and removes the files it
// created. Files it had not reached yet are untouched.
func TestBatchFailureMidwayRollsBack(t *testing.T) {
	root, w := seamEnv(t)
	if err := os.Chmod(filepath.Join(root, seamA), 0o600); err != nil {
		t.Fatal(err)
	}
	before := readAll(t, root, seamA, seamC, seamD)
	calls := 0
	setSeam(t, func(string) error {
		calls++
		if calls == 3 { // the third rename: seamC
			return errors.New("injected failure")
		}
		return nil
	})
	ops := []Op{
		PatchOp(seamA, Patch{Set: nodeOf(t, `{"status":"improving"}`)}),
		CreateOp(Draft{Type: "learning", Frontmatter: nodeOf(t, `{"title":"DDIA ch5","track":"ddia"}`), Body: "x"}),
		AppendOp(seamC, "Log", "- Tue: x"),
		PatchOp(seamD, Patch{Set: nodeOf(t, `{"status":"open"}`)}),
	}

	_, err := w.Batch(context.Background(), ops, "")

	if err == nil || !strings.Contains(err.Error(), "injected failure") || !strings.Contains(err.Error(), "put back") {
		t.Fatalf("error = %v, want the injected failure and that the written files were put back", err)
	}
	after := readAll(t, root, seamA, seamC, seamD)
	for p, want := range before {
		if after[p] != want {
			t.Errorf("%s was not restored byte for byte:\n%s", p, after[p])
		}
	}
	if fi, err := os.Stat(filepath.Join(root, seamA)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("%s mode = %v (%v), want 0600 as before", seamA, fi.Mode().Perm(), err)
	}
	if _, err := os.Stat(filepath.Join(root, seamB)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the created %s was not removed (%v)", seamB, err)
	}
	if left := tempFiles(t, root); len(left) > 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

// Rollback never overwrites a human's save: a file changed since the batch
// wrote it is left as the human saved it, and the error names it.
func TestBatchRollbackKeepsHumanSave(t *testing.T) {
	root, w := seamEnv(t)
	targetA := filepath.Join(root, seamA)
	before := readAll(t, root, seamC)
	var human []byte
	calls := 0
	setSeam(t, func(string) error {
		calls++
		if calls != 2 { // the second rename: seamC
			return nil
		}
		ours, err := os.ReadFile(targetA)
		if err != nil {
			return err
		}
		human = append(ours, "human edit\n"...)
		if err := os.WriteFile(targetA, human, 0o644); err != nil {
			return err
		}
		return errors.New("injected failure")
	})
	ops := []Op{
		PatchOp(seamA, Patch{Set: nodeOf(t, `{"status":"improving"}`)}),
		AppendOp(seamC, "Log", "- Tue: x"),
	}

	_, err := w.Batch(context.Background(), ops, "")

	if err == nil || !strings.Contains(err.Error(), "injected failure") || !strings.Contains(err.Error(), seamA) {
		t.Fatalf("error = %v, want the injected failure and %s named as not put back", err, seamA)
	}
	if got, _ := os.ReadFile(targetA); string(got) != string(human) {
		t.Errorf("%s:\n%s\nwant the human's save kept:\n%s", seamA, got, human)
	}
	if got := readAll(t, root, seamC); got[seamC] != before[seamC] {
		t.Errorf("%s changed:\n%s", seamC, got[seamC])
	}
	if left := tempFiles(t, root); len(left) > 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

// A human saving a file between the batch's checks and its rename is a
// conflict: the batch puts back what it wrote, starts over once from the new
// content, and the human's save is kept.
func TestBatchHumanEditDuringApplyIsKept(t *testing.T) {
	root, w := seamEnv(t)
	targetC := filepath.Join(root, seamC)
	origC, _ := os.ReadFile(targetC)
	calls := 0
	setSeam(t, func(string) error {
		calls++
		if calls == 2 { // the second rename, seamC, on the first attempt
			return os.WriteFile(targetC, append(append([]byte{}, origC...), "- human edit\n"...), 0o644)
		}
		return nil
	})
	ops := []Op{
		PatchOp(seamA, Patch{Set: nodeOf(t, `{"status":"improving"}`)}),
		AppendOp(seamC, "Log", "- Tue: x"),
	}

	if _, err := w.Batch(context.Background(), ops, ""); err != nil {
		t.Fatalf("Batch: %v", err)
	}

	// Renames: A, C (conflict), A put back, then A and C again.
	if calls != 5 {
		t.Errorf("renames = %d, want 5", calls)
	}
	if got, _ := os.ReadFile(targetC); string(got) != string(origC)+"- human edit\n- Tue: x\n" {
		t.Errorf("%s:\n%s\nwant the human edit followed by the batch's line", seamC, got)
	}
	gotA := readAll(t, root, seamA)[seamA]
	if strings.Count(gotA, "status: improving\n") != 1 || strings.Count(gotA, "updated:") != 1 {
		t.Errorf("%s:\n%s\nwant the patch applied once", seamA, gotA)
	}
	if left := tempFiles(t, root); len(left) > 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

// Several ops on one path are one write of the final content.
func TestBatchWritesEachFileOnce(t *testing.T) {
	_, w := seamEnv(t)
	calls := 0
	setSeam(t, func(string) error {
		calls++
		return nil
	})
	ops := []Op{
		PatchOp(seamA, Patch{Set: nodeOf(t, `{"status":"improving"}`)}),
		AppendOp(seamA, "Notes", "- a"),
		PatchOp(seamA, Patch{AddTags: []string{"meta/plan"}}),
	}

	if _, err := w.Batch(context.Background(), ops, ""); err != nil {
		t.Fatalf("Batch: %v", err)
	}

	if calls != 1 {
		t.Errorf("renames = %d, want 1", calls)
	}
}
