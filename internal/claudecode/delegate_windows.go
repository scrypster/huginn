//go:build windows

package claudecode

import "os/exec"

// setProcessGroup is a no-op on Windows: process groups and SIGKILL do not
// exist there. exec.CommandContext's default Cancel (which calls
// cmd.Process.Kill()) terminates the claude process itself on timeout; child
// tool processes it spawned are not separately tracked.
func setProcessGroup(cmd *exec.Cmd) {}
