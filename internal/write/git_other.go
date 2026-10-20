//go:build !unix

package write

import (
	"os/exec"
	"time"
)

// killGroupOnCancel has no process groups to use here: a cancelled git is
// killed by the standard library, and a hook's children may outlive it.
// WaitDelay stops Run from waiting on pipes such a child still holds open,
// which would hold the write lock, and stall every later write, for as long as
// the child lives.
func killGroupOnCancel(cmd *exec.Cmd) (stop func()) {
	cmd.WaitDelay = 500 * time.Millisecond
	return func() {}
}
