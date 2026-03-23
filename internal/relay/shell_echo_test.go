//go:build !windows

package relay

import (
	"testing"
)

func TestSetEcho_NoopWhenNotRunning(t *testing.T) {
	sm := &ShellManager{} // ptmx = nil, running = false
	// Must not panic
	sm.SetEcho(false)
	sm.SetEcho(true)
}

func TestSetEcho_NoopWhenPtmxNil(t *testing.T) {
	sm := &ShellManager{running: true} // running but no ptmx
	sm.SetEcho(false)
	sm.SetEcho(true)
}
