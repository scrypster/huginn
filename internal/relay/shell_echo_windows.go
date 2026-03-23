//go:build windows

package relay

// Windows ConPTY manages echo internally; these are unused no-ops.
const (
	ioctlReadTermios  = 0
	ioctlWriteTermios = 0
)
