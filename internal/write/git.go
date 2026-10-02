package write

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Arkoes07/prep-mcp/internal/config"
)

// Git commits written files. It shells out to the git binary and never pushes.
type Git struct {
	Bin, Author string
	Timeout     time.Duration // for the whole commit, not per git command; zero means 10 s
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

// Commit commits paths (slash-separated, relative to dir) with message msg. It
// only commits when dir is the root of its git repository, and only those
// paths, so staged work by a human is left alone. It never fails a write:
// every problem comes back as warning, and then hash is empty.
func (g *Git) Commit(ctx context.Context, dir string, paths []string, msg string) (hash, warning string) {
	timeout := g.Timeout
	if timeout <= 0 {
		timeout = defaultGitTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fail := func(format string, args ...any) (string, string) {
		return "", "not committed: " + fmt.Sprintf(format, args...)
	}
	bin, name, email, err := g.identity()
	if err != nil {
		return fail("%s", err)
	}
	// fix is the command that makes this commit by hand, author and message included.
	fix := fmt.Sprintf(
		"run `git add -- %[1]s && git commit --only -m %[2]s --author=%[3]s -- %[1]s` in %[4]s",
		pathList(paths),
		shellQuote(msg),
		shellQuote(name+" <"+email+">"),
		dir,
	)
	// problem turns a failed git run into a warning, telling a timeout and a
	// lock left by another git process from other failures.
	problem := func(what, out string, err error) (string, string) {
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return fail("git timed out after %s during %s (a hook or a prompt may be hanging). The file is saved; "+
				"to commit it, %s", timeout, what, fix)
		case ctx.Err() != nil:
			return fail("cancelled during %s. The file is saved; to commit it, %s", what, fix)
		}
		if m := lockRE.FindStringSubmatch(out); m != nil {
			// A lock stops git add (index.lock) or only the commit (HEAD.lock, a branch's lock).
			staged := "nothing was staged"
			if what == "git commit" {
				staged = "the change is staged"
			}
			return fail(
				"git's lock file %s exists. Another git process may be running, or one crashed and left it behind. "+
					"The file is saved; %s. Once no git process is running in %s, delete %s, then %s. "+
					"prep-mcp never deletes it",
				m[1],
				staged,
				dir,
				m[1],
				fix,
			)
		}
		return fail(
			"%s failed: %s. The file is saved; fix the cause, then %s",
			what,
			describe(out, err),
			fix,
		)
	}

	r := runner{bin: bin, dir: dir, env: append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"LC_ALL=C", // stable messages, so "not a git repository" can be recognised
		"GIT_COMMITTER_NAME="+name, "GIT_COMMITTER_EMAIL="+email,
	)}

	// Is dir the root of its repository?
	top, err := r.run(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		if strings.Contains(top, "not a git repository") {
			return fail("%s is not a git repository. "+
				"Run `git init` in %s to get a commit per write, or set commit_on_write: false", dir, dir)
		}
		return problem("git rev-parse", top, err)
	}
	if top = strings.TrimSpace(top); !samePath(dir, top) {
		return fail("%s is inside the git repository at %s but is not its root, so nothing is committed there. "+
			"Run `git init` in %s to make the vault its own repository, or set commit_on_write: false",
			dir, top, dir)
	}

	if out, err := r.run(ctx, append([]string{"add", "--"}, paths...)...); err != nil {
		return problem("git add", out, err)
	}
	// A write that left the files as they already were in HEAD has nothing to commit.
	if _, err := r.run(ctx, append([]string{"diff", "--cached", "--quiet", "--"}, paths...)...); err == nil {
		return "", ""
	} else if ctx.Err() != nil {
		return problem("git diff", "", err)
	}
	commit := []string{"commit", "--only", "-m", msg, "--author=" + name + " <" + email + ">", "--"}
	if out, err := r.run(ctx, append(commit, paths...)...); err != nil {
		return problem("git commit", out, err)
	}
	out, err := r.run(ctx, "rev-parse", "--short", "HEAD")
	if err != nil {
		return problem("git rev-parse", out, err)
	}
	return strings.TrimSpace(out), ""
}

// identity finds the git binary, defaulting to git on the PATH, and splits the
// author into name and email.
func (g *Git) identity() (bin, name, email string, err error) {
	bin = cmp.Or(g.Bin, "git")
	if _, err := exec.LookPath(bin); err != nil {
		return "", "", "", fmt.Errorf(
			"cannot run git (%w). Install git, fix the path %q, or set commit_on_write: false in the config", err, bin)
	}
	name, email, ok := config.ParseAuthor(g.Author)
	if !ok {
		return "", "", "", fmt.Errorf("author %q must look like \"Name <email>\"; fix author in the config", g.Author)
	}
	return bin, name, email, nil
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
		s = s[:maxOutputInWarn] + "..."
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
