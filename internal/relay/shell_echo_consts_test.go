//go:build !windows

package relay

import "testing"

func TestShellEchoIoctlConstsDefined(t *testing.T) {
	// These constants must be non-zero for the ioctl calls to work.
	if ioctlReadTermios == 0 {
		t.Fatal("ioctlReadTermios is 0")
	}
	if ioctlWriteTermios == 0 {
		t.Fatal("ioctlWriteTermios is 0")
	}
}
