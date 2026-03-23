//go:build darwin

package relay

// cpuLoad1m retrieves the 1-minute load average on macOS.
// Darwin (macOS) does not expose Sysinfo_t in the standard syscall package.
// For now, we return an error to omit this metric from the heartbeat.
// A more sophisticated implementation could use system framework calls via cgo or
// parse /proc-style interfaces if available in future macOS versions.
func cpuLoad1m() (float64, error) {
	return 0, ErrServiceUnsupported
}
