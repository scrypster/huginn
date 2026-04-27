package scheduler

import (
	"context"
	"hash/fnv"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

const (
	watcherPollInterval = 2 * time.Second
	watcherDebounce     = 500 * time.Millisecond
)

// WatcherScheduler is the subset of Scheduler used by WorkflowsWatcher.
// Using an interface keeps the watcher independently testable.
type WatcherScheduler interface {
	RegisterWorkflow(w *Workflow) error
	RemoveWorkflow(id string)
}

// WorkflowsWatcher polls a directory for changes to *.yaml workflow files and
// syncs the scheduler's cron entries when files are added, modified, or deleted.
//
// On each detected change it:
//   - Loads all *.yaml files in the directory via LoadWorkflows.
//   - Calls RegisterWorkflow for every workflow (respects enabled:false).
//   - Calls RemoveWorkflow for every previously-known workflow no longer on disk.
//
// Uses FNV-64a hashing of path+size+mtime to detect changes without reading
// file contents on every poll. Changes are debounced 500ms.
type WorkflowsWatcher struct {
	dir      string
	sched    WatcherScheduler
	onChange func() // optional; called after each sync (for tests)

	lastHash uint64
	known    map[string]string // workflow id → file path
}

// NewWorkflowsWatcher creates a WorkflowsWatcher. onChange is called synchronously
// inside the watcher goroutine after each sync where a change was detected.
// Pass nil if you don't need the callback.
func NewWorkflowsWatcher(dir string, sched WatcherScheduler, onChange func()) *WorkflowsWatcher {
	return &WorkflowsWatcher{
		dir:      dir,
		sched:    sched,
		onChange: onChange,
		known:    make(map[string]string),
	}
}

// Start begins polling. Blocks until ctx is cancelled. Call in a goroutine.
func (w *WorkflowsWatcher) Start(ctx context.Context) {
	// Seed hash and load initial workflows.
	w.lastHash = w.computeHash()
	w.sync()

	ticker := time.NewTicker(watcherPollInterval)
	defer ticker.Stop()

	var debounceTimer *time.Timer
	defer func() {
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current := w.computeHash()
			if current == w.lastHash {
				continue
			}
			w.lastHash = current

			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(watcherDebounce, func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("workflows watcher: panic in sync", "panic", r)
					}
				}()
				w.sync()
				if w.onChange != nil {
					w.onChange()
				}
			})
		}
	}
}

func (w *WorkflowsWatcher) sync() {
	workflows, err := LoadWorkflows(w.dir)
	if err != nil {
		slog.Error("workflows watcher: load failed", "dir", w.dir, "err", err)
		return
	}

	onDisk := make(map[string]string, len(workflows))
	for _, wf := range workflows {
		onDisk[wf.ID] = wf.FilePath
		if err := w.sched.RegisterWorkflow(wf); err != nil {
			slog.Warn("workflows watcher: register failed", "id", wf.ID, "err", err)
		}
	}

	for id := range w.known {
		if _, stillThere := onDisk[id]; !stillThere {
			w.sched.RemoveWorkflow(id)
		}
	}

	w.known = onDisk
}

func (w *WorkflowsWatcher) computeHash() uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(w.dir))

	_ = filepath.WalkDir(w.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(w.dir, path)
		_, _ = h.Write([]byte(rel))
		size := info.Size()
		mtime := info.ModTime().UnixNano()
		buf := [16]byte{
			byte(size >> 56), byte(size >> 48), byte(size >> 40), byte(size >> 32),
			byte(size >> 24), byte(size >> 16), byte(size >> 8), byte(size),
			byte(mtime >> 56), byte(mtime >> 48), byte(mtime >> 40), byte(mtime >> 32),
			byte(mtime >> 24), byte(mtime >> 16), byte(mtime >> 8), byte(mtime),
		}
		_, _ = h.Write(buf[:])
		return nil
	})

	if _, err := os.Stat(w.dir); os.IsNotExist(err) {
		return 0
	}
	return h.Sum64()
}
