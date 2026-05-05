package agent

import (
	"context"
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/logger"
	"github.com/scrypster/huginn/internal/memory"
	"github.com/scrypster/huginn/internal/modelconfig"
)

const (
	agentsWatcherPollInterval = 2 * time.Second
	agentsWatcherDebounce     = 500 * time.Millisecond
)

// AgentsWatcher polls the agents directory for changes to *.yaml/*.json agent
// files and calls the provided callback with a freshly-built AgentRegistry on
// each detected change.
type AgentsWatcher struct {
	baseDir  string
	callback func(*agents.AgentRegistry)

	mu       sync.Mutex
	lastHash uint64
	debounce *time.Timer
}

// NewAgentsWatcher creates an AgentsWatcher. callback is called (in a new
// goroutine) with a rebuilt AgentRegistry each time the agents directory changes.
func NewAgentsWatcher(baseDir string, callback func(*agents.AgentRegistry)) *AgentsWatcher {
	return &AgentsWatcher{
		baseDir:  baseDir,
		callback: callback,
	}
}

// Start begins polling. Blocks until ctx is cancelled. Call in a goroutine.
// Seeds the initial hash without firing the callback (caller already loaded
// the registry at startup).
func (w *AgentsWatcher) Start(ctx context.Context) {
	w.mu.Lock()
	w.lastHash = w.computeHash()
	w.mu.Unlock()

	ticker := time.NewTicker(agentsWatcherPollInterval)
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

func (w *AgentsWatcher) check() {
	h := w.computeHash()
	w.mu.Lock()
	defer w.mu.Unlock()
	if h == w.lastHash {
		return
	}
	w.lastHash = h
	if w.debounce != nil {
		w.debounce.Stop()
	}
	w.debounce = time.AfterFunc(agentsWatcherDebounce, func() {
		w.reload()
	})
}

func (w *AgentsWatcher) reload() {
	cfg, err := agents.LoadAgentsFromBase(w.baseDir)
	if err != nil {
		logger.Warn("agents watcher: reload failed", "err", err)
		return
	}
	models := modelconfig.DefaultModels()
	username := memory.ResolveUsername("")
	reg := agents.BuildRegistryWithUsername(cfg, models, username)
	logger.Info("agents watcher: registry reloaded", "count", len(cfg.Agents))
	go w.callback(reg)
}

func (w *AgentsWatcher) computeHash() uint64 {
	h := fnv.New64a()
	agentsDir := filepath.Join(w.baseDir, "agents")
	_ = filepath.WalkDir(agentsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".yaml" && ext != ".json" {
			return nil
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			return nil
		}
		_, _ = h.Write([]byte(path))
		buf := make([]byte, 16)
		size := info.Size()
		mod := info.ModTime().UnixNano()
		for i := 0; i < 8; i++ {
			buf[i] = byte(size >> (i * 8))
			buf[i+8] = byte(mod >> (i * 8))
		}
		_, _ = h.Write(buf)
		return nil
	})
	return h.Sum64()
}
