//go:build windows

package relay

func cpuLoad1m() (float64, error) {
	// Windows does not expose a 1-minute load average.
	// Return an error so the heartbeat omits the field rather than sending 0.
	return 0, ErrServiceUnsupported
}
