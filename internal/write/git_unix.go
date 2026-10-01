//go:build unix

package write

import (
	"os/exec"
	"syscall"
	"time"
)

// termGrace is how long git gets to clean up after SIGTERM before the group is killed.
const termGrace = 2 * time.Second

// killGroupOnCancel puts cmd in its own process group and makes a context
// cancellation SIGTERM that group, then SIGKILL it after termGrace. WaitDelay
// stops Run from waiting on pipes that a surviving child still holds open. The
// returned func disarms the pending SIGKILL; call it once the command has ended.
func killGroupOnCancel(cmd *exec.Cmd) (stop func()) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var kill *time.Timer
	cmd.Cancel = func() error {
		pid := cmd.Process.Pid
		kill = time.AfterFunc(termGrace, func() {
			_ = syscall.Kill(-pid, syscall.SIGKILL) // the group may already be gone; nothing to do then
		})
		return syscall.Kill(-pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = termGrace + 500*time.Millisecond
	return func() {
		if kill != nil {
			kill.Stop()
		}
	}
}
