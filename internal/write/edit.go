package write

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/arkoes07/prep-mcp/internal/vault"
)

// EditOp replaces the one occurrence of old in a file with replacement. An
// empty replacement deletes it.
func EditOp(path, old, replacement string) Op {
	return Op{tool: ToolEdit, target: path, apply: func(p *batchPlan) (*file, []byte, []string, error) {
		return p.modify(path, editRules, func(f *file, _ parsed) ([]byte, *vault.Frontmatter, []string, error) {
			next, err := editText(f.cur, f.rel, old, replacement)
			return next, nil, nil, err
		})
	}}
}

// editText replaces the one occurrence of old in src, the file rel, with
// replacement. It needs no hash: if the text changed under the caller, old
// does not match.
func editText(src []byte, rel, old, replacement string) ([]byte, error) {
	if old == "" {
		return nil, errors.New("old is empty; pass the exact text to replace, copied from read_doc")
	}
	switch n := bytes.Count(src, []byte(old)); n {
	case 0:
		return nil, fmt.Errorf("%q not found in %s; read_doc it and copy the text exactly", old, rel)
	case 1:
	default:
		return nil, fmt.Errorf(
			"%q appears %d times in %s; include more surrounding text so it matches once",
			old,
			n,
			rel,
		)
	}
	return bytes.Replace(src, []byte(old), []byte(replacement), 1), nil
}
