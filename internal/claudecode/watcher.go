package claudecode

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// debounceWindow coalesces the burst of write events Claude Code produces
// while streaming a single turn, so we re-read a transcript at most this often.
const debounceWindow = 150 * time.Millisecond

// Watcher tails every Claude Code transcript under a root directory.
//
// It watches the root and each project subdirectory. Claude Code creates a new
// subdirectory the first time it is run in a given working directory, so newly
// created directories are picked up and watched as they appear.
type Watcher struct {
	root string
	ing  *Ingester

	mu      sync.Mutex
	pending map[string]*time.Timer
}

// NewWatcher returns a watcher over root (typically DefaultRoot()).
func NewWatcher(root string, ing *Ingester) *Watcher {
	return &Watcher{root: root, ing: ing, pending: map[string]*time.Timer{}}
}

// DefaultRoot is where Claude Code stores session transcripts.
func DefaultRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// Run watches until ctx is cancelled. A missing root is not an error: Claude
// Code may not have been run on this machine yet. Watcher errors are logged
// and retried rather than returned, so a transient failure never stops serve.
func (w *Watcher) Run(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Warn("claudecode: cannot create watcher, bridge disabled", "err", err)
		<-ctx.Done()
		return nil
	}
	defer watcher.Close()

	w.addTree(watcher)

	// Re-scan periodically so project directories created while the watcher
	// was running (or a root that appears later) are picked up.
	rescan := time.NewTicker(30 * time.Second)
	defer rescan.Stop()

	for {
		select {
		case <-ctx.Done():
			// Flush messages held back for unresolved tool calls before
			// exiting, so a graceful shutdown never loses a turn that was
			// waiting on the next 30s FlushIdle tick. A hard crash can still
			// lose up to one tick's worth of pending messages — that is
			// accepted; only clean shutdown is covered here.
			w.ing.FlushIdle()
			return nil

		case <-rescan.C:
			w.addTree(watcher)
			// Persist messages held back for sessions that have gone quiet,
			// so a conversation ending on an unanswered tool call is not
			// left unwritten.
			w.ing.FlushIdle()

		case ev, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			w.handle(watcher, ev)

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			slog.Warn("claudecode: watcher error", "err", err)
		}
	}
}

// addTree registers the root and every immediate subdirectory. Adding a path
// that is already watched is a no-op in fsnotify.
func (w *Watcher) addTree(watcher *fsnotify.Watcher) {
	if w.root == "" {
		return
	}
	if err := watcher.Add(w.root); err != nil {
		slog.Debug("claudecode: cannot watch root", "root", w.root, "err", err)
		return
	}
	entries, err := os.ReadDir(w.root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if err := watcher.Add(filepath.Join(w.root, e.Name())); err != nil {
			slog.Debug("claudecode: cannot watch project dir", "dir", e.Name(), "err", err)
		}
	}
}

func (w *Watcher) handle(watcher *fsnotify.Watcher, ev fsnotify.Event) {
	// A new project directory: start watching it. Except "subagents" — see
	// transcriptPaths in backfill.go for why sub-agent transcripts are
	// deliberately never ingested, live or otherwise.
	if ev.Op&fsnotify.Create != 0 {
		if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
			if filepath.Base(ev.Name) == "subagents" {
				return
			}
			if err := watcher.Add(ev.Name); err != nil {
				slog.Debug("claudecode: cannot watch new dir", "dir", ev.Name, "err", err)
			}
			return
		}
	}
	if !strings.HasSuffix(ev.Name, ".jsonl") {
		return
	}
	if ev.Op&(fsnotify.Write|fsnotify.Create) == 0 {
		return
	}
	w.schedule(ev.Name)
}

// schedule debounces repeated writes to the same transcript.
func (w *Watcher) schedule(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if t, ok := w.pending[path]; ok {
		t.Reset(debounceWindow)
		return
	}
	w.pending[path] = time.AfterFunc(debounceWindow, func() {
		w.mu.Lock()
		delete(w.pending, path)
		w.mu.Unlock()

		if _, err := w.ing.IngestFile(path); err != nil {
			slog.Warn("claudecode: ingest failed", "path", path, "err", err)
		}
	})
}
