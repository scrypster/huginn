//go:build windows

package tray

import "golang.org/x/sys/windows"

// stillActive is the exit code returned by GetExitCodeProcess when the process
// is still running. This is defined by the Win32 API as STATUS_PENDING (0x103).
// Reference: https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-getexitcodeprocess
const stillActive = 259 // STATUS_PENDING / STILL_ACTIVE

// processIsLive returns true if the process with the given PID is still running
// on Windows. It uses OpenProcess + GetExitCodeProcess rather than signal(0)
// because on Windows os.FindProcess always succeeds and Signal(0) returns
// syscall.EWINDOWS for all processes, making the Unix approach unreliable.
func processIsLive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// Access denied or PID not found — treat as not live.
		return false
	}
	defer windows.CloseHandle(h)

	var exitCode uint32
	if err := windows.GetExitCodeProcess(h, &exitCode); err != nil {
		return false
	}
	return exitCode == stillActive
}
