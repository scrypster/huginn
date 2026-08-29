//go:build !windows

package agent

import (
	"os/exec"
	"syscall"
)

// setHookProcAttrs starts cmd as the leader of its own process group so a
// timeout can kill the whole tree — including any background children the
// hook command spawns (e.g. "sleep 30 &") — not just the direct "sh -c"
// process. See runHookCommand (F1).
func setHookProcAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killHookProcessGroup kills the process group cmd's process belongs to
// (set up by setHookProcAttrs), so a backgrounded child doesn't outlive —
// and keep the stdout/stderr pipe open past — the hook's own timeout.
func killHookProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	// A negative pid targets the whole process group; Setpgid made
	// cmd.Process.Pid the group leader's pid, so -pid is the group id.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
