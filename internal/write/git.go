package write

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Git commits written files in the vault folder Dir, as Name <Email>, so
// every AI change can be reviewed (git log --author) and reverted. It shells
// out to the git binary and never pushes.
type Git struct {
	Dir, Name, Email string
	Bin              string        // default: git on the PATH
	Timeout          time.Duration // for the whole commit, not per git command; zero means 10 s
}

// lockRE finds the lock file git names when another git process holds it, or
// left it behind (git runs with LC_ALL=C, so the message is stable).
var lockRE = regexp.MustCompile(`Unable to create '([^']+\.lock)': File exists`)

// shellSafeRE matches a word a POSIX shell takes as it is.
var shellSafeRE = regexp.MustCompile(`^[A-Za-z0-9_./@%+=:,-]+$`)

const (
	defaultGitTimeout = 10 * time.Second
	maxOutputInWarn   = 400
	maxPathsInWarn    = 5
)

// Commit commits paths (slash-separated, relative to Dir) with message msg. It
// only commits when Dir is the root of its git repository, and only those
// paths, so staged work by a human is left alone. It never fails a write:
// every problem comes back as warning, and then hash is empty.
//
// Root only, because a vault inside a bigger repository, like examples/vault
// in this one, must never receive AI commits. A commit covers whole files, so
// a human's uncommitted edit to a written file goes into the AI's commit.
func (g *Git) Commit(ctx context.Context, paths []string, msg string) (hash, warning string) {
	timeout := g.Timeout
	if timeout <= 0 {
		timeout = defaultGitTimeout
	}
	// The files are saved by now, and a later commit covers only its own paths,
	// so a commit cut short is never made: a cancelled call still commits, and
	// only the timeout stops git.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	hash, err := g.commit(ctx, paths, msg, timeout)
	if err != nil {
		return "", "not committed: " + err.Error()
	}
	return hash, ""
}

// commit runs Commit's steps in order and stops at the first that fails.
func (g *Git) commit(ctx context.Context, paths []string, msg string, timeout time.Duration) (string, error) {
	bin := cmp.Or(g.Bin, "git")
	if _, err := exec.LookPath(bin); err != nil {
		return "", fmt.Errorf(
			"cannot run git (%w). Install git, fix the path %q, or set commit_on_write: false in the config",
			err,
			bin,
		)
	}
	c := &commitRun{
		git: runner{bin: bin, dir: g.Dir, env: append(
			os.Environ(),
			"GIT_TERMINAL_PROMPT=0",
			"LC_ALL=C", // stable messages, so "not a git repository" can be recognised
			"GIT_COMMITTER_NAME="+g.Name,
			"GIT_COMMITTER_EMAIL="+g.Email,
		)},
		paths:   paths,
		msg:     msg,
		author:  g.Name + " <" + g.Email + ">",
		timeout: timeout,
	}

	if err := c.dropRepoEnv(ctx); err != nil {
		return "", err
	}
	if err := c.checkRoot(ctx); err != nil {
		return "", err
	}
	if err := c.stage(ctx); err != nil {
		return "", err
	}
	// A write that left the files as they already were in HEAD has nothing to commit.
	if changed, err := c.diffStaged(ctx); err != nil || !changed {
		return "", err
	}
	if err := c.commitStaged(ctx); err != nil {
		return "", err
	}
	return c.readHead(ctx)
}

// commitRun is one Commit: git set up to run in the vault, and what to commit.
type commitRun struct {
	git         runner
	paths       []string
	msg, author string
	timeout     time.Duration
}

// dropRepoEnv removes from git's environment the variables that tie git to one
// repository (GIT_DIR, GIT_WORK_TREE, GIT_INDEX_FILE and the rest). The server
// inherits them when, say, another repository's hook starts it, and they would
// send the commit to that repository. git lists them itself, so the list
// matches the git that runs.
func (c *commitRun) dropRepoEnv(ctx context.Context) error {
	out, err := c.git.run(ctx, "rev-parse", "--local-env-vars")
	if err != nil {
		return c.explainFailure(ctx, "git rev-parse", out, err)
	}
	local := strings.Fields(out)
	c.git.env = slices.DeleteFunc(c.git.env, func(kv string) bool {
		k, _, _ := strings.Cut(kv, "=")
		return slices.Contains(local, k)
	})
	return nil
}

// checkRoot fails unless the vault is the root of its git repository.
func (c *commitRun) checkRoot(ctx context.Context) error {
	dir := c.git.dir
	top, err := c.git.run(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		if strings.Contains(top, "not a git repository") {
			return fmt.Errorf(
				"%s is not a git repository. Run `git init` in %s to get a commit per write, "+
					"or set commit_on_write: false",
				dir,
				dir,
			)
		}
		return c.explainFailure(ctx, "git rev-parse", top, err)
	}
	if top = strings.TrimSpace(top); !samePath(dir, top) {
		return fmt.Errorf(
			"%s is inside the git repository at %s but is not its root, so nothing is committed there. "+
				"Run `git init` in %s to make the vault its own repository, or set commit_on_write: false",
			dir,
			top,
			dir,
		)
	}
	return nil
}

// stage adds the paths to the index.
func (c *commitRun) stage(ctx context.Context) error {
	if out, err := c.git.run(ctx, append([]string{"add", "--"}, c.paths...)...); err != nil {
		return c.explainFailure(ctx, "git add", out, err)
	}
	return nil
}

// diffStaged reports whether the staged paths differ from HEAD.
func (c *commitRun) diffStaged(ctx context.Context) (bool, error) {
	_, err := c.git.run(ctx, append([]string{"diff", "--cached", "--quiet", "--"}, c.paths...)...)
	switch {
	case err == nil:
		return false, nil
	case ctx.Err() != nil:
		return false, c.explainFailure(ctx, "git diff", "", err)
	}
	return true, nil
}

// commitStaged commits the paths and nothing else, as the configured author.
// A failed commit leaves them staged. No git reset: it is one more call that
// can fail in a repo already in an odd state, and they belong in the next commit.
func (c *commitRun) commitStaged(ctx context.Context) error {
	args := append([]string{"commit", "--only", "-m", c.msg, "--author=" + c.author, "--"}, c.paths...)
	if out, err := c.git.run(ctx, args...); err != nil {
		return c.explainFailure(ctx, "git commit", out, err)
	}
	return nil
}

// readHead returns the short hash of HEAD, the commit just made.
func (c *commitRun) readHead(ctx context.Context) (string, error) {
	out, err := c.git.run(ctx, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", c.explainFailure(ctx, "git rev-parse", out, err)
	}
	return strings.TrimSpace(out), nil
}

// explainFailure turns a failed git run into the error a warning shows,
// telling a timeout and a lock left by another git process from other failures.
func (c *commitRun) explainFailure(ctx context.Context, what, out string, err error) error {
	fix := c.renderFix()
	if ctx.Err() != nil { // only the timeout ends ctx: Commit ignores the caller's cancellation
		return fmt.Errorf(
			"git timed out after %s during %s (a hook or a prompt may be hanging). The file is saved; to commit it, %s",
			c.timeout,
			what,
			fix,
		)
	}
	if m := lockRE.FindStringSubmatch(out); m != nil {
		// A lock stops git add (index.lock) or only the commit (HEAD.lock, a branch's lock).
		// Never delete it: a live lock cannot be told from a stale one, and
		// deleting the lock of a running git (a GUI, the Obsidian Git plugin, an
		// open commit editor) can corrupt the index.
		staged := "nothing was staged"
		if what == "git commit" {
			staged = "the change is staged"
		}
		return fmt.Errorf(
			"git's lock file %s exists. Another git process may be running, or one crashed and left it behind. "+
				"The file is saved; %s. Once no git process is running in %s, delete %s, then %s. "+
				"prep-mcp never deletes it",
			m[1],
			staged,
			c.git.dir,
			m[1],
			fix,
		)
	}
	return fmt.Errorf("%s failed: %s. The file is saved; fix the cause, then %s", what, describe(out, err), fix)
}

// renderFix renders the command that makes this commit by hand, author and message included.
func (c *commitRun) renderFix() string {
	return fmt.Sprintf(
		"run `git add -- %[1]s && git commit --only -m %[2]s --author=%[3]s -- %[1]s` in %[4]s",
		pathList(c.paths),
		shellQuote(c.msg),
		shellQuote(c.author),
		c.git.dir,
	)
}

type runner struct {
	bin, dir string
	env      []string
}

// run starts git and returns its combined output. On a timeout the whole
// process group gets SIGTERM, so git removes its own index.lock and a hook's
// children die too, and the call returns without waiting for them.
func (r runner) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, r.bin, args...)
	cmd.Dir = r.dir
	cmd.Env = r.env
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	stop := killGroupOnCancel(cmd)
	defer stop()
	err := cmd.Run()
	return out.String(), err
}

// describe renders git's output and error on one line, trimmed to a sane length.
func describe(out string, err error) string {
	var lines []string
	for l := range strings.SplitSeq(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	s := strings.Join(lines, "; ")
	if len(s) > maxOutputInWarn {
		n := maxOutputInWarn
		for n > 0 && !utf8.RuneStart(s[n]) { // cut between characters, so the warning stays valid UTF-8
			n--
		}
		s = s[:n] + "..."
	}
	if s == "" {
		return err.Error()
	}
	return s
}

// pathList is paths quoted for a hint's command, or a pointer to the result's
// paths when there are more than maxPathsInWarn.
func pathList(paths []string) string {
	if len(paths) > maxPathsInWarn {
		return "<the paths in results>"
	}
	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = shellQuote(p)
	}
	return strings.Join(quoted, " ")
}

// shellQuote quotes s for a POSIX shell, so a hint's command runs as shown.
func shellQuote(s string) string {
	if shellSafeRE.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// samePath compares two directories after resolving symlinks (~/prep may be a
// link, and /var is a link to /private/var on macOS).
func samePath(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return false
	}
	return filepath.Clean(ra) == filepath.Clean(rb)
}
