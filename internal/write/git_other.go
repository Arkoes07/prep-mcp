//go:build !unix

package write

import "os/exec"

// killGroupOnCancel has no process groups to use here: a cancelled git is
// killed by the standard library, and a hook's children may outlive it.
func killGroupOnCancel(cmd *exec.Cmd) (stop func()) { return func() {} }
