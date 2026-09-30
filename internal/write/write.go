// Package write changes files in a vault: create, append, patch frontmatter,
// replace. Byte transforms are pure functions; Writer applies them safely.
//
// M2a: every function here is a stub so the tests compile and fail.
package write

import "errors"

var errStub = errors.New("write: not implemented yet (M2c)")
