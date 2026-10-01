// Package vault reads a folder of markdown files with YAML frontmatter.
//
// Every file access goes through an *os.Root, so the kernel confines it to
// the vault even if a path check is wrong. Resolve adds the readable errors.
package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Arkoes07/prep-mcp/internal/config"
)

// Vault is one root directory of markdown files.
type Vault struct {
	dir  string
	root *os.Root
	cfg  *config.Config
	now  func() time.Time
}

// Open opens the vault at dir. The directory is resolved through symlinks once.
// A nil now means the system clock; a nil cfg means an empty config. The
// caller must Close the vault when done with it.
func Open(dir string, cfg *config.Config, now func() time.Time) (*Vault, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("vault root %q: %w (pass an absolute --root)", dir, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("vault root %q: %w (check --root or PREP_ROOT)", dir, err)
	}
	root, err := os.OpenRoot(resolved)
	if err != nil {
		return nil, fmt.Errorf("vault root %q: %w (check --root or PREP_ROOT)", dir, err)
	}
	if cfg == nil {
		if cfg, err = config.Parse(nil); err != nil {
			return nil, errors.Join(fmt.Errorf("empty default config: %w", err), root.Close())
		}
	}
	if now == nil {
		now = time.Now
	}
	return &Vault{dir: resolved, root: root, cfg: cfg, now: now}, nil
}

// Dir returns the vault's absolute directory, resolved through symlinks.
func (v *Vault) Dir() string { return v.dir }

// Now returns the vault's clock, which is the server clock in production.
func (v *Vault) Now() time.Time { return v.now() }

// Root returns the confining root. All file I/O must go through it.
func (v *Vault) Root() *os.Root { return v.root }

// Close releases the vault's directory handle. The vault must not be used
// afterwards.
func (v *Vault) Close() error {
	if err := v.root.Close(); err != nil {
		return fmt.Errorf("close vault %s: %w", v.dir, err)
	}
	return nil
}

// Access says what a path is about to be used for.
type Access int

// AccessRead is the zero value on purpose: a forgotten Access gets the rules
// of a read, which never creates or changes a file.
const (
	AccessRead Access = iota
	AccessWrite
)

// PathError is a rejected path. Msg says what to do next. Err is the file
// system error behind it, if any, so errors.Is(err, fs.ErrNotExist) works.
type PathError struct {
	Path, Msg string
	Err       error
}

// Error returns Msg, which already names the path.
func (e *PathError) Error() string { return e.Msg }

// Unwrap returns the file system error behind the rejection, or nil.
func (e *PathError) Unwrap() error { return e.Err }

// notFound is the error for a document path that does not exist.
func notFound(p, clean string) error {
	return &PathError{
		Path: p,
		Msg:  fmt.Sprintf("not found: %s (use list_docs or search_text)", clean),
		Err:  fs.ErrNotExist,
	}
}

// Resolve checks a vault-relative path and returns it cleaned, with "/"
// separators. Reads follow symlinks that stay inside the vault. Writes refuse
// any symlink, refuse templates/, and allow files that do not exist yet.
func (v *Vault) Resolve(p string, a Access) (string, error) {
	clean, err := checkPathText(p, a)
	if err != nil {
		return "", err
	}
	fail := func(format string, args ...any) (string, error) {
		return "", &PathError{Path: p, Msg: fmt.Sprintf(format, args...)}
	}
	parts := strings.Split(clean, "/")

	// Walk the components, looking for symlinks and missing pieces.
	for i := range parts {
		prefix := strings.Join(parts[:i+1], "/")
		last := i == len(parts)-1
		fi, err := v.root.Lstat(prefix)
		if errors.Is(err, fs.ErrNotExist) {
			if a == AccessWrite {
				return clean, nil
			}
			return "", notFound(p, clean)
		}
		if err != nil {
			return "", &PathError{Path: p, Msg: fmt.Sprintf("cannot access %q: %v", p, err), Err: err}
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(filepath.Join(v.dir, filepath.FromSlash(prefix)))
			if err != nil {
				msg := fmt.Sprintf("%q goes through a broken symlink (%s); remove the link or fix its target", p, prefix)
				return "", &PathError{Path: p, Msg: msg, Err: err}
			}
			rel, err := filepath.Rel(v.dir, target)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return fail("%q is outside the vault: %s is a symlink that leaves it", p, prefix)
			}
			if a == AccessWrite {
				return fail("%s is a symlink; write to %s instead", prefix, filepath.ToSlash(rel))
			}
			continue
		}
		if last && fi.IsDir() {
			return fail("%q is a directory; name a file", p)
		}
		if !last && !fi.IsDir() {
			return fail("%q: %s is a file, not a folder", p, prefix)
		}
	}
	return clean, nil
}

// checkPathText rejects a path by its text alone and returns it cleaned. The
// file system is not touched; Resolve does that next.
func checkPathText(p string, a Access) (string, error) {
	fail := func(format string, args ...any) (string, error) {
		return "", &PathError{Path: p, Msg: fmt.Sprintf(format, args...)}
	}
	switch {
	case p == "":
		return fail("empty path; pass a vault-relative path such as notes/x.md")
	case strings.ContainsRune(p, 0) || strings.ContainsRune(p, '\\'):
		return fail(`invalid path %q; use "/" separators and no control characters`, p)
	case strings.HasPrefix(p, "/") || filepath.IsAbs(p):
		return fail("absolute paths not allowed; use a vault-relative path (got %q)", p)
	case strings.HasSuffix(p, "/"):
		return fail("%q ends with /; name a file, for example notes/x.md", p)
	}
	clean := path.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fail("%q is outside the vault; use a path relative to the vault root", p)
	}
	if clean == "." {
		return fail("%q is the vault root; name a file inside it", p)
	}
	parts := strings.Split(clean, "/")
	for _, part := range parts {
		if strings.HasPrefix(part, ".") {
			return fail("%q has a hidden component (%s); dot-folders and dot-files are not accessible", p, part)
		}
	}
	if !strings.EqualFold(path.Ext(clean), ".md") {
		return fail("%q is not a markdown file (.md only); use list_docs to find documents", p)
	}
	if a == AccessWrite && strings.EqualFold(parts[0], "templates") {
		return fail("templates/ is read-only; read %q with read_doc and create the new file with create_doc", p)
	}
	return clean, nil
}

// Read returns a file's cleaned path, bytes and hash. Any .md file in the
// vault can be read, including templates/, but not dot-folders.
func (v *Vault) Read(p string) (rel string, content []byte, hash string, err error) {
	rel, err = v.Resolve(p, AccessRead)
	if err != nil {
		return "", nil, "", err
	}
	content, err = v.root.ReadFile(rel)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil, "", notFound(p, rel)
		}
		return "", nil, "", fmt.Errorf("read %s: %w", rel, err)
	}
	return rel, content, Hash(content), nil
}

// Hash is the lowercase hex sha256 of b. replace_doc compares it.
func Hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
