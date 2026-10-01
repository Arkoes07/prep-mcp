package write_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Arkoes07/prep-mcp/internal/testvault"
	"github.com/Arkoes07/prep-mcp/internal/vault"
	"github.com/Arkoes07/prep-mcp/internal/write"
)

// These tests shell out to git. Each one isolates itself from the developer's
// git setup (PLAN.md 4.3): no global or system config, no identity from the
// environment, no GIT_DIR left over from a hook that launched `go test`.

// isolateGit skips the test when git is missing and cuts every outside
// influence on the git processes this test starts, ours and the helper's.
func isolateGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{
		"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL",
		"GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "EMAIL",
	} {
		t.Setenv(k, "") // registers the restore; the next line removes the variable
		os.Unsetenv(k)
	}
}

// git runs git in dir as the human (not as the configured author) and returns
// trimmed stdout. It fails the test on a non-zero exit.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Human", "GIT_AUTHOR_EMAIL=human@local",
		"GIT_COMMITTER_NAME=Human", "GIT_COMMITTER_EMAIL=human@local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// initRepo makes dir a git repo with everything in it committed by the human.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	git(t, dir, "init", "-q")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "init")
}

func commitCount(t *testing.T, dir string) string { return git(t, dir, "rev-list", "--count", "HEAD") }

// newGitEnv is newEnv with a real Git on the writer. The vault is not a repo
// yet: call initRepo to make it one.
func newGitEnv(t *testing.T, timeout time.Duration) *env {
	t.Helper()
	isolateGit(t)
	e := newEnv(t)
	e.w = write.New(e.v, e.cfg, gitFor(e, "git", timeout), testvault.Clock())
	return e
}

func gitFor(e *env, bin string, timeout time.Duration) *write.Git {
	return &write.Git{Bin: bin, Author: e.cfg.Author, Timeout: timeout}
}

const gitTimeout = 10 * time.Second

// createNote writes learning/ddia-ch5.md through the writer.
func createNote(t *testing.T, e *env) *write.Result {
	t.Helper()
	res, err := e.w.Create(ctx, write.Draft{Type: "learning", Frontmatter: node(t, `{"title":"DDIA ch5","track":"ddia"}`), Body: "x"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if res.Path != "learning/ddia-ch5.md" {
		t.Fatalf("created %s, want learning/ddia-ch5.md", res.Path)
	}
	return res
}

// wantNoWarnings fails if the result carries any warning.
func wantNoWarnings(t *testing.T, res *write.Result) {
	t.Helper()
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %q, want none", res.Warnings)
	}
}

// wantWarning fails unless exactly one warning is present and it mentions every fragment.
func wantWarning(t *testing.T, res *write.Result, fragments ...string) {
	t.Helper()
	if len(res.Warnings) != 1 {
		t.Fatalf("warnings = %q, want exactly one", res.Warnings)
	}
	for _, f := range fragments {
		if !strings.Contains(res.Warnings[0], f) {
			t.Errorf("warning %q does not contain %q", res.Warnings[0], f)
		}
	}
}

// wantCommitted checks that HEAD is a new commit by the configured author with
// the given message, that it holds only the given paths, and that the result
// reports it.
func wantCommitted(t *testing.T, e *env, res *write.Result, before, msg string, paths ...string) {
	t.Helper()
	if got := commitCount(t, e.root); got == before {
		t.Fatalf("commit count is still %s, want one more", before)
	}
	if got := git(t, e.root, "log", "-1", "--format=%an <%ae>|%cn <%ce>"); got != "Claude <claude@local>|Claude <claude@local>" {
		t.Errorf("author|committer = %q, want the configured author for both", got)
	}
	if got := git(t, e.root, "log", "-1", "--format=%B"); got != msg {
		t.Errorf("message = %q, want %q", got, msg)
	}
	files := git(t, e.root, "show", "--name-only", "--format=", "HEAD")
	if want := strings.Join(paths, "\n"); files != want {
		t.Errorf("files in the commit = %q, want %q", files, want)
	}
	head := git(t, e.root, "rev-parse", "HEAD")
	if res.Commit == "" || !strings.HasPrefix(head, res.Commit) {
		t.Errorf("Result.Commit = %q, want the hash of HEAD (%s) or an abbreviation of it", res.Commit, head)
	}
}

func TestGitNotRepo(t *testing.T) {
	e := newGitEnv(t, gitTimeout)

	res := createNote(t, e)

	if !e.exists("learning/ddia-ch5.md") {
		t.Error("the write must succeed even though nothing can be committed")
	}
	wantWarning(t, res, "not committed", "git init", e.v.Dir())
	if res.Commit != "" {
		t.Errorf("Result.Commit = %q, want empty", res.Commit)
	}
	if e.exists(".git") {
		t.Error("the server must never create a repository")
	}
}

func TestGitDisabled(t *testing.T) {
	e := newGitEnv(t, gitTimeout)
	initRepo(t, e.root)
	e.cfg.CommitOnWrite = false
	before := commitCount(t, e.root)

	res := createNote(t, e)

	if got := commitCount(t, e.root); got != before {
		t.Errorf("commit count = %s, want %s: commit_on_write is false", got, before)
	}
	wantNoWarnings(t, res)
	if res.Commit != "" {
		t.Errorf("Result.Commit = %q, want empty", res.Commit)
	}
}

func TestGitCommit(t *testing.T) {
	e := newGitEnv(t, gitTimeout)
	initRepo(t, e.root)
	before := commitCount(t, e.root)

	res := createNote(t, e)

	wantNoWarnings(t, res)
	wantCommitted(t, e, res, before, "create_doc: learning/ddia-ch5.md", "learning/ddia-ch5.md")
	if st := git(t, e.root, "status", "--porcelain"); st != "" {
		t.Errorf("working tree is not clean after the commit:\n%s", st)
	}
}

func TestGitEachTool(t *testing.T) {
	e := newGitEnv(t, gitTimeout)
	initRepo(t, e.root)

	steps := []struct {
		msg, path string
		do        func(t *testing.T) (*write.Result, error)
	}{
		{"create_doc: learning/ddia-ch5.md", "learning/ddia-ch5.md", func(t *testing.T) (*write.Result, error) {
			return e.w.Create(ctx, write.Draft{Type: "learning", Frontmatter: node(t, `{"title":"DDIA ch5","track":"ddia"}`), Body: "x"})
		}},
		{"append_section: weeks/2026-w03.md", "weeks/2026-w03.md", func(t *testing.T) (*write.Result, error) {
			return e.w.AppendSection(ctx, "weeks/2026-w03.md", "Log", "- Tue: x")
		}},
		{"update_frontmatter: improvements/buries-headline.md", "improvements/buries-headline.md", func(t *testing.T) (*write.Result, error) {
			return e.w.UpdateFrontmatter(ctx, "improvements/buries-headline.md", write.Patch{Set: node(t, `{"status":"improving"}`)})
		}},
		{"replace_doc: learning/ddia-ch5.md", "learning/ddia-ch5.md", func(t *testing.T) (*write.Result, error) {
			old := e.read(t, "learning/ddia-ch5.md")
			return e.w.Replace(ctx, "learning/ddia-ch5.md", old+"more\n", vault.Hash([]byte(old)))
		}},
	}
	for _, s := range steps {
		t.Run(strings.SplitN(s.msg, ":", 2)[0], func(t *testing.T) {
			before := commitCount(t, e.root)
			res, err := s.do(t)
			if err != nil {
				t.Fatal(err)
			}
			wantNoWarnings(t, res)
			wantCommitted(t, e, res, before, s.msg, s.path)
		})
	}
}

// The human's staged work must neither be swept into the AI's commit nor lose
// its place in the index.
func TestGitHumanStaged(t *testing.T) {
	e := newGitEnv(t, gitTimeout)
	initRepo(t, e.root)
	testvault.Write(t, e.root, "other.md", "human draft\n")
	testvault.Write(t, e.root, "tags.md", e.read(t, "tags.md")+"`extra/tag`\n")
	git(t, e.root, "add", "other.md", "tags.md")
	before := commitCount(t, e.root)

	res := createNote(t, e)

	wantNoWarnings(t, res)
	wantCommitted(t, e, res, before, "create_doc: learning/ddia-ch5.md", "learning/ddia-ch5.md")
	if got := git(t, e.root, "diff", "--cached", "--name-only"); got != "other.md\ntags.md" {
		t.Errorf("still staged = %q, want other.md and tags.md", got)
	}
}

// The vault is a folder inside a bigger repo: never commit there (PLAN.md open question 19).
func TestGitSubdirOfRepo(t *testing.T) {
	e := newGitEnv(t, gitTimeout)
	parent := filepath.Dir(e.root)
	initRepo(t, parent)
	before := commitCount(t, parent)

	res := createNote(t, e)

	if !e.exists("learning/ddia-ch5.md") {
		t.Error("the write must succeed")
	}
	if got := commitCount(t, parent); got != before {
		t.Errorf("commit count in the enclosing repo = %s, want %s", got, before)
	}
	wantWarning(t, res, "not committed")
	if res.Commit != "" {
		t.Errorf("Result.Commit = %q, want empty", res.Commit)
	}
}

// ~/prep may be a symlink to the real repo. The repo's toplevel and the vault
// root must be compared after resolving symlinks, both through the writer and
// when Git is handed the unresolved path directly.
func TestGitRootViaSymlink(t *testing.T) {
	e := newGitEnv(t, gitTimeout)
	initRepo(t, e.root)
	link := filepath.Join(t.TempDir(), "prep")
	if err := os.Symlink(e.root, link); err != nil {
		t.Fatal(err)
	}

	t.Run("writer", func(t *testing.T) {
		v, err := vault.Open(link, e.cfg, testvault.Clock())
		if err != nil {
			t.Fatal(err)
		}
		testvault.CloseOnCleanup(t, v)
		w := write.New(v, e.cfg, gitFor(e, "git", gitTimeout), testvault.Clock())
		before := commitCount(t, e.root)

		res, err := w.Create(ctx, write.Draft{Type: "learning", Frontmatter: node(t, `{"title":"DDIA ch5","track":"ddia"}`), Body: "x"})
		if err != nil {
			t.Fatal(err)
		}

		wantNoWarnings(t, res)
		wantCommitted(t, e, res, before, "create_doc: learning/ddia-ch5.md", "learning/ddia-ch5.md")
	})

	t.Run("git_given_the_link", func(t *testing.T) {
		testvault.Write(t, e.root, "direct.md", "x\n")
		before := commitCount(t, e.root)

		hash, warning := gitFor(e, "git", gitTimeout).Commit(ctx, link, "direct.md", "direct: direct.md")

		if warning != "" {
			t.Fatalf("warning = %q, want none", warning)
		}
		wantCommitted(t, e, &write.Result{Commit: hash}, before, "direct: direct.md", "direct.md")
	})
}

func writeHook(t *testing.T, root, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("hooks are shell scripts")
	}
	p := filepath.Join(root, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// A failing hook costs the commit, never the write, and the human sees why.
func TestGitHookFails(t *testing.T) {
	e := newGitEnv(t, gitTimeout)
	initRepo(t, e.root)
	writeHook(t, e.root, "echo 'hook says no: lint failed' >&2\nexit 1")
	before := commitCount(t, e.root)

	res := createNote(t, e)

	if !e.exists("learning/ddia-ch5.md") {
		t.Error("the write must be kept when the hook rejects the commit")
	}
	if got := commitCount(t, e.root); got != before {
		t.Errorf("commit count = %s, want %s", got, before)
	}
	wantWarning(t, res, "not committed", "hook says no: lint failed")
	if res.Commit != "" {
		t.Errorf("Result.Commit = %q, want empty", res.Commit)
	}
}

// A hook that never returns must not hang the tool call. The hook's own child
// process keeps git's output pipes open, so killing git alone is not enough:
// the call has to return soon after the timeout, not after the hook ends.
func TestGitHookHangs(t *testing.T) {
	e := newGitEnv(t, time.Second)
	initRepo(t, e.root)
	writeHook(t, e.root, "sleep 30")
	before := commitCount(t, e.root)

	start := time.Now()
	res := createNote(t, e)
	elapsed := time.Since(start)

	if elapsed > 10*time.Second {
		t.Errorf("the write took %v with a 1s git timeout; it must not wait for the hook", elapsed)
	}
	if !e.exists("learning/ddia-ch5.md") {
		t.Error("the write must be kept when git times out")
	}
	if got := commitCount(t, e.root); got != before {
		t.Errorf("commit count = %s, want %s", got, before)
	}
	wantWarning(t, res, "git timed out")
}

// A stale or concurrent index.lock is the human's to clear. Report it, keep the write, leave the lock.
func TestGitIndexLock(t *testing.T) {
	e := newGitEnv(t, gitTimeout)
	initRepo(t, e.root)
	lock := filepath.Join(e.root, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	before := commitCount(t, e.root)

	res := createNote(t, e)

	if !e.exists("learning/ddia-ch5.md") {
		t.Error("the write must be kept")
	}
	if got := commitCount(t, e.root); got != before {
		t.Errorf("commit count = %s, want %s", got, before)
	}
	wantWarning(t, res, "not committed", "index.lock")
	if _, err := os.Stat(lock); err != nil {
		t.Errorf("the server must not remove a lock it did not create: %v", err)
	}
}

func TestGitNoBinary(t *testing.T) {
	e := newGitEnv(t, gitTimeout)
	initRepo(t, e.root)
	e.w = write.New(e.v, e.cfg, gitFor(e, "/nonexistent/git", gitTimeout), testvault.Clock())
	before := commitCount(t, e.root)

	res := createNote(t, e)

	if !e.exists("learning/ddia-ch5.md") {
		t.Error("the write must be kept")
	}
	if got := commitCount(t, e.root); got != before {
		t.Errorf("commit count = %s, want %s", got, before)
	}
	wantWarning(t, res, "not committed", "/nonexistent/git")
}

// An update that changes nothing writes nothing, so there is nothing to commit
// and nothing to warn about.
func TestGitNoopUpdate(t *testing.T) {
	e := newGitEnv(t, gitTimeout)
	initRepo(t, e.root)
	before := commitCount(t, e.root)

	res, err := e.w.UpdateFrontmatter(ctx, "improvements/buries-headline.md", write.Patch{Set: node(t, `{"status":"open"}`)})
	if err != nil {
		t.Fatal(err)
	}

	if got := commitCount(t, e.root); got != before {
		t.Errorf("commit count = %s, want %s: a no-op must not commit", got, before)
	}
	wantNoWarnings(t, res)
	if res.Commit != "" {
		t.Errorf("Result.Commit = %q, want empty", res.Commit)
	}
}
