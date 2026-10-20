package write_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/arkoes07/prep-mcp/internal/testvault"
	"github.com/arkoes07/prep-mcp/internal/vault"
	"github.com/arkoes07/prep-mcp/internal/write"
)

// These tests shell out to git. Each one isolates itself from the developer's
// git setup: no global or system config, no identity from the
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
	cmd.Env = append(
		os.Environ(),
		"GIT_AUTHOR_NAME=Human",
		"GIT_AUTHOR_EMAIL=human@local",
		"GIT_COMMITTER_NAME=Human",
		"GIT_COMMITTER_EMAIL=human@local",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// initRepo makes dir a git repo on branch main with everything in it
// committed by the human.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	git(t, dir, "init", "-q", "-b", "main")
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
	e.w = write.New(e.v, e.cfg, gitFor(e.v.Dir(), "git", timeout), testvault.NewClock())
	return e
}

// gitFor is a Git that commits in dir as Claude <claude@local>.
func gitFor(dir, bin string, timeout time.Duration) *write.Git {
	return &write.Git{Dir: dir, Name: "Claude", Email: "claude@local", Bin: bin, Timeout: timeout}
}

const gitTimeout = 10 * time.Second

// ddiaOp creates learning/ddia-ch5.md.
func ddiaOp(t *testing.T) write.Op {
	return write.NewCreateOp(write.Draft{
		Type:        "learning",
		Frontmatter: node(t, `{"title":"DDIA ch5","track":"ddia"}`),
		Body:        "x",
	})
}

// createNote writes learning/ddia-ch5.md through the writer.
func createNote(t *testing.T, e *env) *write.Result {
	t.Helper()
	res, err := e.w.Apply(ctx, ddiaOp(t))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if res.Path != "learning/ddia-ch5.md" {
		t.Fatalf("created %s, want learning/ddia-ch5.md", res.Path)
	}
	return res
}

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
	const wantAuthors = "Claude <claude@local>|Claude <claude@local>"
	if got := git(t, e.root, "log", "-1", "--format=%an <%ae>|%cn <%ce>"); got != wantAuthors {
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

func TestGitNoCommitOnUnknownTag(t *testing.T) {
	e := newGitEnv(t, gitTimeout)
	initRepo(t, e.root)
	before := commitCount(t, e.root)

	_, err := e.w.Apply(ctx, write.NewPatchOp(
		"improvements/buries-headline.md",
		write.Patch{AddTags: []string{"issue/buries-headlin"}},
	))

	mustContain(t, err, unknownTagErr)
	if got := commitCount(t, e.root); got != before {
		t.Errorf("commit count = %s, want %s: a rejected write must not commit", got, before)
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
			return e.w.Apply(ctx, ddiaOp(t))
		}},
		{"append_section: weeks/2026-w03.md", "weeks/2026-w03.md", func(t *testing.T) (*write.Result, error) {
			return e.w.Apply(ctx, write.NewAppendOp("weeks/2026-w03.md", "Log", "- Tue: x"))
		}},
		{"update_frontmatter: improvements/buries-headline.md", "improvements/buries-headline.md", func(t *testing.T) (*write.Result, error) {
			patch := write.Patch{Set: node(t, `{"status":"improving"}`)}
			return e.w.Apply(ctx, write.NewPatchOp("improvements/buries-headline.md", patch))
		}},
		{"edit_text: weeks/2026-w03.md", "weeks/2026-w03.md", func(t *testing.T) (*write.Result, error) {
			return e.w.Apply(ctx, write.NewEditOp("weeks/2026-w03.md", "- Tue: x", "- Tue: y"))
		}},
		{"replace_doc: learning/ddia-ch5.md", "learning/ddia-ch5.md", func(t *testing.T) (*write.Result, error) {
			old := e.read(t, "learning/ddia-ch5.md")
			return e.w.Apply(ctx, write.NewReplaceOp("learning/ddia-ch5.md", old+"more\n", vault.Hash([]byte(old))))
		}},
		{"edit_text: templates/week.md", "templates/week.md", func(t *testing.T) (*write.Result, error) {
			return e.w.Apply(ctx, write.NewEditOp("templates/week.md", "Weekly review", "Retro"))
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

// A batch is one commit by the AI author, holding every file it wrote. The
// message is the caller's, else a count of ops per tool.
func TestGitBatchOneCommit(t *testing.T) {
	e := newGitEnv(t, gitTimeout)
	initRepo(t, e.root)
	set := func(p, js string) write.Op {
		return write.NewPatchOp(p, write.Patch{Set: node(t, js)})
	}
	threeFiles := []string{"improvements/buries-headline.md", "improvements/missing-tradeoff.md", "weeks/2026-w03.md"}
	steps := []struct {
		name    string
		ops     []write.Op
		message string
		wantMsg string
		paths   []string
	}{
		{
			name: "generated_message",
			ops: []write.Op{
				set("improvements/buries-headline.md", `{"status":"improving"}`),
				set("improvements/missing-tradeoff.md", `{"status":"open"}`),
				set("weeks/2026-w03.md", `{"mood":"ok"}`),
			},
			wantMsg: "batch: 3 update_frontmatter",
			paths:   threeFiles,
		},
		{
			name: "mixed_tools",
			ops: []write.Op{
				write.NewAppendOp("weeks/2026-w03.md", "Log", "- Tue: x"),
				set("improvements/buries-headline.md", `{"status":"open"}`),
				set("improvements/missing-tradeoff.md", `{"status":"improving"}`),
			},
			wantMsg: "batch: 1 append_section, 2 update_frontmatter",
			paths:   threeFiles,
		},
		{
			name: "caller_message",
			ops: []write.Op{
				ddiaOp(t),
				write.NewAppendOp("weeks/2026-w03.md", "Log", "- Wed: y"),
			},
			message: "  Import reading notes\n",
			wantMsg: "Import reading notes",
			paths:   []string{"learning/ddia-ch5.md", "weeks/2026-w03.md"},
		},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			before := commitCount(t, e.root)

			res, err := e.w.Batch(ctx, s.ops, s.message)
			if err != nil {
				t.Fatal(err)
			}

			if len(res.Warnings) != 0 {
				t.Errorf("warnings = %q, want none", res.Warnings)
			}
			n, err := strconv.Atoi(before)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := commitCount(t, e.root), strconv.Itoa(n+1); got != want {
				t.Errorf("commit count = %s, want exactly one more (%s)", got, want)
			}
			wantCommitted(t, e, &write.Result{Commit: res.Commit}, before, s.wantMsg, s.paths...)
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

// The vault is a folder inside a bigger repo: never commit there.
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

// A GIT_DIR inherited from the server's environment, as when a hook of another
// repository starts it, must not send the commit there.
func TestGitInheritedGitDir(t *testing.T) {
	e := newGitEnv(t, gitTimeout)
	initRepo(t, e.root)
	other := t.TempDir()
	testvault.Write(t, other, "README.md", "other\n")
	initRepo(t, other)
	before, otherBefore := commitCount(t, e.root), commitCount(t, other)
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))

	res := createNote(t, e)
	os.Unsetenv("GIT_DIR") // the helpers below find each repository from its folder

	wantNoWarnings(t, res)
	wantCommitted(t, e, res, before, "create_doc: learning/ddia-ch5.md", "learning/ddia-ch5.md")
	if got := commitCount(t, other); got != otherBefore {
		t.Errorf("commit count in the other repository = %s, want %s", got, otherBefore)
	}
}

// A call cancelled while it writes still commits what it wrote: a later commit
// covers only its own paths, so this one would never be made.
func TestGitCancelledCall(t *testing.T) {
	e := newGitEnv(t, gitTimeout)
	initRepo(t, e.root)
	before := commitCount(t, e.root)
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	e.w.SetBeforeRename(func(string) error {
		cancel()
		return nil
	})

	res, err := e.w.Apply(cctx, ddiaOp(t))
	if err != nil {
		t.Fatal(err)
	}

	wantNoWarnings(t, res)
	wantCommitted(t, e, res, before, "create_doc: learning/ddia-ch5.md", "learning/ddia-ch5.md")
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
		v, err := vault.Open(link)
		if err != nil {
			t.Fatal(err)
		}
		testvault.CloseOnCleanup(t, v)
		w := write.New(v, e.cfg, gitFor(v.Dir(), "git", gitTimeout), testvault.NewClock())
		before := commitCount(t, e.root)

		res, err := w.Apply(ctx, ddiaOp(t))
		if err != nil {
			t.Fatal(err)
		}

		wantNoWarnings(t, res)
		wantCommitted(t, e, res, before, "create_doc: learning/ddia-ch5.md", "learning/ddia-ch5.md")
	})

	t.Run("git_given_the_link", func(t *testing.T) {
		testvault.Write(t, e.root, "direct.md", "x\n")
		before := commitCount(t, e.root)

		hash, warning := gitFor(link, "git", gitTimeout).Commit(ctx, []string{"direct.md"}, "direct: direct.md")

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

// Git output quoted in a warning is cut short, never inside a character.
func TestGitLongOutputStaysUTF8(t *testing.T) {
	e := newGitEnv(t, gitTimeout)
	initRepo(t, e.root)
	// 256 two-byte characters after an "x", so a cut at an even byte count splits one.
	writeHook(t, e.root, "s=é; for i in 1 2 3 4 5 6 7 8; do s=$s$s; done\necho \"x$s\" >&2\nexit 1")

	res := createNote(t, e)

	wantWarning(t, res, "not committed", "x"+strings.Repeat("é", 10))
	if !utf8.ValidString(res.Warnings[0]) {
		t.Errorf("warning is not valid UTF-8: %q", res.Warnings[0])
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

// A hook that leaves a background process holding git's output open, and exits
// 0, does not cost the commit: git made it, so the result reports it.
func TestGitHookLeavesBackgroundProcess(t *testing.T) {
	e := newGitEnv(t, gitTimeout)
	initRepo(t, e.root)
	writeHook(t, e.root, "sleep 30 &\nexit 0")
	before := commitCount(t, e.root)

	res := createNote(t, e)

	wantNoWarnings(t, res)
	wantCommitted(t, e, res, before, "create_doc: learning/ddia-ch5.md", "learning/ddia-ch5.md")
}

// A stale or concurrent git lock is the human's to clear. Report it by its
// path and say whether the write was staged; keep the write; leave the lock.
// index.lock stops git add, so nothing is staged; HEAD.lock stops only the
// commit, so the file is staged.
func TestGitLock(t *testing.T) {
	tests := []struct {
		lock   string
		staged string // git diff --cached --name-only afterwards
		say    string
	}{
		{lock: "index.lock", staged: "", say: "nothing was staged"},
		{lock: "HEAD.lock", staged: "learning/ddia-ch5.md", say: "the change is staged"},
		{lock: "refs/heads/main.lock", staged: "learning/ddia-ch5.md", say: "the change is staged"},
	}
	for _, tc := range tests {
		t.Run(tc.lock, func(t *testing.T) {
			e := newGitEnv(t, gitTimeout)
			initRepo(t, e.root)
			lock := filepath.Join(e.root, ".git", tc.lock)
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
			wantWarning(
				t,
				res,
				"not committed",
				"/vault/.git/"+tc.lock+" exists",
				tc.say,
				"Once no git process is running",
				"delete",
				"prep-mcp never deletes it",
			)
			if got := git(t, e.root, "diff", "--cached", "--name-only"); got != tc.staged {
				t.Errorf("staged = %q, want %q", got, tc.staged)
			}
			if _, err := os.Stat(lock); err != nil {
				t.Errorf("the server must not remove a lock it did not create: %v", err)
			}
		})
	}
}

// A batch under a lock is written in full, with the one commit warning.
func TestGitBatchLock(t *testing.T) {
	e := newGitEnv(t, gitTimeout)
	initRepo(t, e.root)
	lock := filepath.Join(e.root, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ops := []write.Op{
		write.NewAppendOp("weeks/2026-w03.md", "Log", "- Tue: x"),
		write.NewPatchOp("improvements/buries-headline.md", write.Patch{Set: node(t, `{"status":"improving"}`)}),
	}

	res, err := e.w.Batch(ctx, ops, "")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(e.read(t, "weeks/2026-w03.md"), "- Tue: x\n") ||
		!strings.Contains(e.read(t, "improvements/buries-headline.md"), "status: improving\n") {
		t.Error("the batch's writes must be kept")
	}
	if res.Commit != "" || len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], ".git/index.lock exists") {
		t.Errorf("commit = %q, warnings = %q; want no commit and one lock warning", res.Commit, res.Warnings)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Errorf("the server must not remove a lock it did not create: %v", err)
	}
}

// The command in a warning makes the commit the write could not, with the
// configured author and the write's message, even for a path a shell would split.
func TestGitHintCommandRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the hint is a POSIX shell command")
	}
	const p = "weeks/odd 'name'.md"
	e := newGitEnv(t, gitTimeout)
	testvault.Write(t, e.root, p, "# Odd\n")
	initRepo(t, e.root)
	lock := filepath.Join(e.root, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := e.w.Apply(ctx, write.NewAppendOp(p, "Log", "- Tue: x"))
	if err != nil {
		t.Fatal(err)
	}
	_, after, _ := strings.Cut(strings.Join(res.Warnings, "\n"), "then run `")
	cmdline, _, ok := strings.Cut(after, "`")
	if !ok {
		t.Fatalf("no command in the warnings %q", res.Warnings)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("sh", "-c", cmdline)
	cmd.Dir = e.root
	cmd.Env = append(os.Environ(), "GIT_COMMITTER_NAME=Human", "GIT_COMMITTER_EMAIL=human@local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", cmdline, err, out)
	}
	got := git(t, e.root, "log", "-1", "--format=%an <%ae>|%s")
	if want := "Claude <claude@local>|append_section: " + p; got != want {
		t.Errorf("last commit = %q, want %q", got, want)
	}
}

func TestGitNoBinary(t *testing.T) {
	e := newGitEnv(t, gitTimeout)
	initRepo(t, e.root)
	e.w = write.New(e.v, e.cfg, gitFor(e.v.Dir(), "/nonexistent/git", gitTimeout), testvault.NewClock())
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

	patch := write.Patch{Set: node(t, `{"status":"open"}`)}
	res, err := e.w.Apply(ctx, write.NewPatchOp("improvements/buries-headline.md", patch))
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
