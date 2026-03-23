//go:build !windows

package relay

import (
	"golang.org/x/sys/unix"
)

// SetEcho enables or disables PTY ECHO. Safe no-op if the PTY is not running.
// Called by the dispatcher when the browser sends shell_echo_off / shell_echo_on.
func (sm *ShellManager) SetEcho(on bool) {
	sm.mu.Lock()
	ptmx := sm.ptmx
	running := sm.running
	sm.mu.Unlock()

	if !running || ptmx == nil {
		return
	}

	termios, err := unix.IoctlGetTermios(int(ptmx.Fd()), ioctlReadTermios)
	if err != nil {
		return
	}
	if on {
		termios.Lflag |= unix.ECHO
	} else {
		termios.Lflag &^= unix.ECHO
	}
	_ = unix.IoctlSetTermios(int(ptmx.Fd()), ioctlWriteTermios, termios)
}
