//go:build !windows && !darwin

package relay

import "syscall"

func cpuLoad1m() (float64, error) {
	var info syscall.Sysinfo_t
	if err := syscall.Sysinfo(&info); err != nil {
		return 0, err
	}
	// Sysinfo.Loads[0] is 1-minute load average in units of 1/65536.
	return float64(info.Loads[0]) / 65536.0, nil
}
