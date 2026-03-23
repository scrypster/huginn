//go:build windows

package relay

// SetEcho is a no-op on Windows: ConPTY manages echo internally.
func (sm *ShellManager) SetEcho(_ bool) {}
