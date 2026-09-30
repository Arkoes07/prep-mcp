package write_test

import (
	"os"
	"testing"
)

// The atomic write is internal, so these tests reach it through the Writer.

func TestAtomicWrite(t *testing.T) {
	const p = "weeks/2026-w03.md"

	t.Run("mode_preserved", func(t *testing.T) {
		e := newEnv(t)
		if err := os.Chmod(e.abs(p), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := e.w.AppendSection(ctx, p, "Log", "- Tue: x"); err != nil {
			t.Fatalf("AppendSection: %v", err)
		}
		fi, err := os.Stat(e.abs(p))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
		}
		if left := leftovers(t, e.root); len(left) > 0 {
			t.Errorf("temp files left behind: %v", left)
		}
	})

	t.Run("hard_link_not_written_through", func(t *testing.T) {
		e := newEnv(t)
		orig := e.read(t, p)
		outside := e.abs("../linked.md")
		if err := os.Link(e.abs(p), outside); err != nil {
			t.Skipf("hard links not available: %v", err)
		}
		if _, err := e.w.AppendSection(ctx, p, "Log", "- Tue: x"); err != nil {
			t.Fatalf("AppendSection: %v", err)
		}
		b, err := os.ReadFile(outside)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != orig {
			t.Errorf("the file outside the vault was written through the link:\n%q", b)
		}
		if e.read(t, p) == orig {
			t.Error("the vault file was not updated")
		}
		in, _ := os.Stat(e.abs(p))
		out, _ := os.Stat(outside)
		if os.SameFile(in, out) {
			t.Error("vault file still shares an inode with the outside link; the write must replace the file")
		}
	})

	t.Run("no_temp_after_exists_error", func(t *testing.T) {
		e := newEnv(t)
		before := snapshot(t, e.root)
		if _, err := e.w.Create(ctx, "week", node(t, `{"week":"2026-W03"}`), "", ""); err == nil {
			t.Fatal("Create over an existing file succeeded")
		}
		sameSnapshot(t, before, snapshot(t, e.root))
	})

	t.Run("no_temp_after_write_failure", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root ignores directory permissions")
		}
		e := newEnv(t)
		if err := os.Chmod(e.abs("weeks"), 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(e.abs("weeks"), 0o755) })
		before := snapshot(t, e.root)
		_, err := e.w.AppendSection(ctx, p, "Log", "- Tue: x")
		mustContain(t, err, p)
		sameSnapshot(t, before, snapshot(t, e.root))
	})
}
