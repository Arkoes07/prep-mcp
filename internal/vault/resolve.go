package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
)

type Access int

// AccessRead is the zero value on purpose: a forgotten Access gets the rules
// of a read, which never creates or changes a file.
const (
	AccessRead Access = iota
	AccessWrite
)

// fail rejects a path. The message names the path and says what to do next.
func fail(format string, args ...any) (string, error) {
	return "", fmt.Errorf(format, args...)
}

func notFound(clean string) error {
	return fmt.Errorf("not found: %s (use list_docs or search_text)", clean)
}

// Resolve checks a vault-relative path and returns it cleaned, with "/"
// separators. Reads follow symlinks that stay inside the vault. Writes refuse
// any symlink, refuse templates/, and allow files that do not exist yet.
func (v *Vault) Resolve(p string, a Access) (string, error) {
	clean, err := checkPathText(p, a)
	if err != nil {
		return "", err
	}
	parts := strings.Split(clean, "/")

	for i := range parts {
		prefix := strings.Join(parts[:i+1], "/")
		last := i == len(parts)-1
		fi, err := v.root.Lstat(prefix)
		if errors.Is(err, fs.ErrNotExist) {
			if a == AccessWrite {
				return clean, nil
			}
			return "", notFound(clean)
		}
		if err != nil {
			return fail("cannot access %q: %w", p, err)
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(filepath.Join(v.dir, filepath.FromSlash(prefix)))
			if err != nil {
				return fail("%q goes through a broken symlink (%s); remove the link or fix its target", p, prefix)
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
