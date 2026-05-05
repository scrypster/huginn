package server

import (
	"context"
	"log/slog"
	"os"
	"sync/atomic"
	"time"
)

// staleCheckInterval is the poll cadence. Package-level var so tests can
// override it without modifying the constructor.
var staleCheckInterval = 60 * time.Second

// StaleWatcher detects when the on-disk binary has been replaced (e.g. by
// `brew upgrade huginn`) while the server is running. It records the binary's
// mtime at construction time and polls periodically. Once stale, it stays stale
// until the process is restarted.
type StaleWatcher struct {
	exePath   string
	baseMtime time.Time
	stale     atomic.Bool
}

// newStaleWatcher creates a StaleWatcher for the binary at exePath.
// exePath should already be resolved through symlinks.
func newStaleWatcher(exePath string) (*StaleWatcher, error) {
	info, err := os.Stat(exePath)
	if err != nil {
		return nil, err
	}
	return &StaleWatcher{
		exePath:   exePath,
		baseMtime: info.ModTime(),
	}, nil
}

// Start launches the background polling goroutine. It stops when ctx is done.
func (w *StaleWatcher) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(staleCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.check()
			}
		}
	}()
}

// IsStale reports whether the on-disk binary has been replaced since startup.
func (w *StaleWatcher) IsStale() bool {
	return w.stale.Load()
}

func (w *StaleWatcher) check() {
	info, err := os.Stat(w.exePath)
	if err != nil {
		slog.Debug("stale watcher: stat error", "path", w.exePath, "err", err)
		return
	}
	if !info.ModTime().Equal(w.baseMtime) {
		if w.stale.CompareAndSwap(false, true) {
			slog.Warn("huginn binary has been updated on disk — restart to activate",
				"path", w.exePath)
		}
	}
}
