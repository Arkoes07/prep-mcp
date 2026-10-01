package write

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Arkoes07/prep-mcp/internal/config"
)

// Git commits written files. It shells out to the git binary and never pushes.
type Git struct {
	Bin, Author string
	Timeout     time.Duration // for the whole commit, not per git command; zero means 10 s
}

const (
	defaultGitTimeout = 10 * time.Second
	maxOutputInWarn   = 400
)

// Commit commits rel (slash-separated, relative to dir) with message msg. It
// only commits when dir is the root of its git repository, and only the one
// path, so staged work by a human is left alone. It never fails a write:
// every problem comes back as warning, and then hash is empty.
func (g *Git) Commit(ctx context.Context, dir, rel, msg string) (hash, warning string) {
	timeout := g.Timeout
	if timeout <= 0 {
		timeout = defaultGitTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fail := func(format string, args ...any) (string, string) {
		return "", "not committed: " + fmt.Sprintf(format, args...)
	}
	// problem turns a failed git run into a warning, telling a timeout from other failures.
	problem := func(what string, out string, err error) (string, string) {
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return fail("git timed out after %s during %s (a hook or a prompt may be hanging). The file is saved; commit it yourself in %s", timeout, what, dir)
		case ctx.Err() != nil:
			return fail("cancelled during %s. The file is saved; commit it yourself in %s", what, dir)
		}
		return fail("%s failed: %s. The file is saved; fix the cause, then run `git add -- %s && git commit --only -- %s` in %s", what, describe(out, err), rel, rel, dir)
	}

	bin := g.Bin
	if bin == "" {
		bin = "git"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return fail("cannot run git (%v). Install git, fix the path %q, or set commit_on_write: false in the config", err, bin)
	}
	author := g.Author
	if strings.TrimSpace(author) == "" {
		author = config.DefaultAuthor
	}
	name, email, ok := config.ParseAuthor(author)
	if !ok {
		return fail("author %q must look like \"Name <email>\"; fix author in the config", g.Author)
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
			return fail("%s is not a git repository. Run `git init` in %s to get a commit per write, or set commit_on_write: false", dir, dir)
		}
		return problem("git rev-parse", top, err)
	}
	if top = strings.TrimSpace(top); !samePath(dir, top) {
		return fail("%s is inside the git repository at %s but is not its root, so nothing is committed there. "+
			"Run `git init` in %s to make the vault its own repository, or set commit_on_write: false",
			dir, top, dir)
	}

	if out, err := r.run(ctx, "add", "--", rel); err != nil {
		return problem("git add", out, err)
	}
	// A write that left the file as it already was in HEAD has nothing to commit.
	if _, err := r.run(ctx, "diff", "--cached", "--quiet", "--", rel); err == nil {
		return "", ""
	} else if ctx.Err() != nil {
		return problem("git diff", "", err)
	}
	if out, err := r.run(ctx, "commit", "--only", "-m", msg, "--author="+name+" <"+email+">", "--", rel); err != nil {
		return problem("git commit", out, err)
	}
	out, err := r.run(ctx, "rev-parse", "--short", "HEAD")
	if err != nil {
		return problem("git rev-parse", out, err)
	}
	return strings.TrimSpace(out), ""
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
