package read

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// isLink reports whether s names a file the way a [[wikilink]] does rather
// than by path. A bare .md name such as tags.md is a path to a root file.
func isLink(s string) bool {
	bracketed := strings.HasPrefix(s, "[[") && strings.HasSuffix(s, "]]")
	return bracketed || !strings.EqualFold(path.Ext(s), ".md")
}

// linkTarget returns the target of a [[wikilink]], or of a bare name:
// brackets, an Obsidian |alias or #heading and a .md extension removed.
func linkTarget(s string) string {
	s = strings.TrimSuffix(strings.TrimPrefix(s, "[["), "]]")
	if i := strings.IndexAny(s, "|#"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if strings.EqualFold(path.Ext(s), ".md") {
		s = strings.TrimSuffix(s, path.Ext(s))
	}
	return s
}

// matchLink returns the paths a link target names: the file name without its
// extension, or a trailing folder/name, compared case-insensitively.
func matchLink(paths []string, target string) []string {
	target = strings.ToLower(target)
	var out []string
	for _, p := range paths {
		stem := strings.ToLower(strings.TrimSuffix(p, path.Ext(p)))
		if stem == target || strings.HasSuffix(stem, "/"+target) {
			out = append(out, p)
		}
	}
	return out
}

// resolveLink returns the one vault path that link names.
func (r *Reader) resolveLink(link string) (string, error) {
	target := linkTarget(link)
	if target == "" {
		return "", errors.New(
			"empty name; pass a file name such as x, [[x]] or a vault-relative path such as notes/x.md",
		)
	}
	var paths []string
	// Unreadable folders are left out here; list_docs reports them.
	_, err := r.walkMarkdown(func(p string, _ fs.DirEntry) { paths = append(paths, p) })
	if err != nil {
		return "", fmt.Errorf("list vault files: %w", err)
	}
	matches := matchLink(paths, target)
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no file named %q; use list_docs or search_text to find it", target)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf(
			"%q matches several files: %s; pass one of these paths",
			target,
			strings.Join(matches, ", "),
		)
	}
}
