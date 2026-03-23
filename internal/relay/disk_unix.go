//go:build !windows

package relay

import "golang.org/x/sys/unix"

func diskFreeGB(path string) (float64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}
	bytes := stat.Bavail * uint64(stat.Bsize)
	return float64(bytes) / (1 << 30), nil
}
