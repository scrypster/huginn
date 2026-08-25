//go:build !windows

package claudecode

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts cmd in its own process group and arranges for
// context cancellation (a timeout, or the caller's ctx being canceled) to
// SIGKILL the whole group, not just the claude CLI itself. Without this, a
// timed-out claude process can leave orphaned tool subprocesses running.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
