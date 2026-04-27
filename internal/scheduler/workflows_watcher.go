package scheduler

import (
	"context"
	"hash/fnv"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	watcherPollInterval = 2 * time.Second
	watcherDebounce     = 500 * time.Millisecond
)

// WatcherScheduler is the subset of Scheduler used by WorkflowsWatcher.
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

	mu       sync.Mutex
	lastHash uint64
	debounce *time.Timer
	known    map[string]string // workflow id → file path
}

// NewWorkflowsWatcher creates a WorkflowsWatcher. onChange is called
// (in a new goroutine, after the mutex is released) after each sync.
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
// Seeds the initial hash and performs an initial sync before polling begins.
func (w *WorkflowsWatcher) Start(ctx context.Context) {
	// Seed initial state so the first poll doesn't fire spuriously,
	// and so the delete test can detect removed files correctly.
	w.mu.Lock()
	w.lastHash = w.computeHash()
	w.mu.Unlock()
	w.sync() // register any already-present workflows

	ticker := time.NewTicker(watcherPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.mu.Lock()
			if w.debounce != nil {
				w.debounce.Stop()
			}
			w.mu.Unlock()
			return
		case <-ticker.C:
			w.check()
		}
	}
}

func (w *WorkflowsWatcher) check() {
	current := w.computeHash()
	w.mu.Lock()
	defer w.mu.Unlock()

	if current == w.lastHash {
		return
	}
	w.lastHash = current

	if w.debounce != nil {
		w.debounce.Stop()
	}
	onChange := w.onChange
	w.debounce = time.AfterFunc(watcherDebounce, func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("workflows watcher: panic in sync", "panic", r)
			}
		}()
		w.sync()
		if onChange != nil {
			onChange()
		}
	})
}

// sync loads all YAML files from the directory, registers new/changed workflows,
// and removes stale ones. Serialized by the caller (AfterFunc fires one at a time
// within the debounce window; initial sync is called before polling begins).
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

	w.mu.Lock()
	known := w.known
	w.known = onDisk
	w.mu.Unlock()

	for id := range known {
		if _, stillThere := onDisk[id]; !stillThere {
			w.sched.RemoveWorkflow(id)
		}
	}
}

// computeHash walks dir and hashes the name, size, and mtime of every *.yaml file.
// Must be called without w.mu held (it does not need the lock; it reads only the filesystem).
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
