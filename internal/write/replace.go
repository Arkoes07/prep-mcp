package write

import (
	"errors"
	"fmt"
	"strings"

	"github.com/arkoes07/prep-mcp/internal/vault"
)

// NewReplaceOp returns the op that rewrites a whole file, but only if it still
// has the content whose hash the caller read.
func NewReplaceOp(path, content, expectedHash string) Op {
	return Op{tool: ToolReplace, target: path, apply: func(p *batchPlan) (*file, []byte, []string, error) {
		return p.modify(path, contentRules, func(f *file, _ parsed) ([]byte, *vault.Frontmatter, []string, error) {
			next, err := replaceContent(f, content, expectedHash)
			return next, nil, nil, err
		})
	}}
}

// replaceContent is content as f's new bytes, but only while f holds the file
// the caller read: expectedHash is of that file, so a path an earlier op in
// the batch changed cannot be replaced.
func replaceContent(f *file, content, expectedHash string) ([]byte, error) {
	expected := strings.ToLower(strings.TrimSpace(expectedHash))
	if expected == "" {
		return nil, errors.New("expected_hash is required; get it from read_doc")
	}
	if f.changed() {
		return nil, fmt.Errorf(
			"an earlier op in this batch changed %s; "+
				"put replace_doc first for %s, or fold that change into its content",
			f.rel,
			f.rel,
		)
	}
	if cur := vault.Hash(f.cur); cur != expected {
		return nil, &ConflictError{Path: f.rel, Current: cur}
	}
	return []byte(content), nil
}
