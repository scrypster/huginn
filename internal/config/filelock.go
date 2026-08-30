package config

// lockPathFor returns the sidecar lock file path for a config file — held
// via flock(2) (see filelock_unix.go) across UpdateAt's read-modify-write so
// two separate OS processes (the TUI and `serve`, or two `serve` instances)
// serialize their config writes instead of racing. updateMu only serializes
// callers within a single process; it has no effect across processes, which
// each get their own zero-value updateMu.
func lockPathFor(path string) string {
	return path + ".lock"
}

// fileLock is a held cross-process lock; release() must be called exactly
// once to unlock and close it. The zero value is not valid — obtain one via
// acquireFileLock.
type fileLock struct {
	closer func() error
}

func (l *fileLock) release() error {
	if l == nil || l.closer == nil {
		return nil
	}
	return l.closer()
}
