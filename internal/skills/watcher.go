package skills

import (
	"hash/fnv"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// watchPollInterval is how often the watcher checks for file-system changes.
	// 2 seconds is imperceptible for a dev hot-reload workflow and avoids
	// hammering stat() calls on slow file systems.
	watchPollInterval = 2 * time.Second

	// watchDebounce is the minimum quiet period after a change is detected
	// before the reload callback fires. Prevents multiple rapid reloads when
	// an editor writes several files in quick succession.
	watchDebounce = 500 * time.Millisecond
)

// SkillsWatcher polls a directory for changes to *.md skill files and triggers
// a reload callback when modifications are detected.
//
// It uses a stable hash of all file names + sizes + mtimes to detect changes
// without reading file contents on every poll cycle. A change is debounced for
// 500 ms so that editors that write files in stages (temp file → rename) only
// trigger one reload.
//
// The reload callback is invoked in a separate goroutine and must not block.
// Any panic inside the callback is recovered and logged — it never crashes the
// watcher loop.
type SkillsWatcher struct {
	dir      string
	onChange func()

	mu        sync.Mutex
	lastHash  uint64
	debounce  *time.Timer
	stopCh    chan struct{}
	stopped   bool
}

// NewSkillsWatcher creates a watcher for dir. onChange is called (in a new
// goroutine) whenever a *.md file is created, modified, or deleted.
// The watcher does not start until Start() is called.
func NewSkillsWatcher(dir string, onChange func()) *SkillsWatcher {
	return &SkillsWatcher{
		dir:      dir,
		onChange: onChange,
		stopCh:   make(chan struct{}),
	}
}

// Start begins polling. It is safe to call Start more than once (idempotent).
func (w *SkillsWatcher) Start() {
	go w.loop()
}

// Stop halts polling. Safe to call if Start was never called.
func (w *SkillsWatcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.stopped {
		w.stopped = true
		close(w.stopCh)
		if w.debounce != nil {
			w.debounce.Stop()
		}
	}
}

func (w *SkillsWatcher) loop() {
	// Seed the hash with the current state so we don't fire on first poll.
	w.mu.Lock()
	w.lastHash = w.computeHash()
	w.mu.Unlock()

	ticker := time.NewTicker(watchPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.check()
		}
	}
}

func (w *SkillsWatcher) check() {
	current := w.computeHash()
	w.mu.Lock()
	defer w.mu.Unlock()

	if current == w.lastHash {
		return
	}
	w.lastHash = current

	// Debounce: reset the timer on each change so we only fire after things
	// have been stable for watchDebounce.
	if w.debounce != nil {
		w.debounce.Stop()
	}
	onChange := w.onChange
	w.debounce = time.AfterFunc(watchDebounce, func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("skills watcher: panic in reload callback", "panic", r)
			}
		}()
		onChange()
	})
}

// computeHash walks dir recursively and hashes the name, size, and mtime of
// every *.md file. The hash is seeded with the directory path itself so that
// an empty directory has a different hash from a different empty directory
// (prevents stale-cache collisions on registry reset).
func (w *SkillsWatcher) computeHash() uint64 {
	h := fnv.New64a()
	// Seed with the dir path so empty directories are distinguishable.
	_, _ = h.Write([]byte(w.dir))

	_ = filepath.WalkDir(w.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		// Include relative path, size, and mtime in the hash.
		rel, _ := filepath.Rel(w.dir, path)
		_, _ = h.Write([]byte(rel))
		// Pack size and mtime into 8 bytes each.
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

	// If the directory doesn't exist, return 0 so any non-existent dir
	// appears stable (no change events until it's created).
	if _, err := os.Stat(w.dir); os.IsNotExist(err) {
		return 0
	}
	return h.Sum64()
}
