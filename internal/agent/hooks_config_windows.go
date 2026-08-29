//go:build windows

package agent

import "os/exec"

// setHookProcAttrs is a no-op on windows: process-group kill uses
// syscall.Kill, which doesn't exist there. cmd.WaitDelay (set by
// runHookCommand) still bounds the timeout — see hooks_config_unix.go.
func setHookProcAttrs(cmd *exec.Cmd) {}

// killHookProcessGroup is a no-op on windows; see setHookProcAttrs.
func killHookProcessGroup(cmd *exec.Cmd) {}
