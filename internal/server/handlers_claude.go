package server

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/scrypster/huginn/internal/claudecode"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// SetClaudeAgentOwned tells the transcript ingester which Claude Code sessions
// are driven by a Huginn agent, so the bridge does not import turns the agent's
// own chat path has already persisted — without this every message from a
// claude-code agent is stored twice, once by the agent and once by the bridge.
//
// Replaces the whole set, so callers pass the complete list on every change.
// A no-op when the bridge is disabled: there is then nothing ingesting.
func (s *Server) SetClaudeAgentOwned(externalIDs []string) {
	s.claudeMu.RLock()
	ing := s.claudeIngester
	s.claudeMu.RUnlock()
	if ing == nil {
		return
	}
	ing.SetAgentOwned(externalIDs)
}

// StartClaudeBridge starts transcript ingestion if the bridge is enabled.
// It returns immediately; the watcher runs until ctx is cancelled. A failure
// to start is logged and swallowed — the bridge is never allowed to prevent
// the server from serving.
func (s *Server) StartClaudeBridge(ctx context.Context, cfg claudecode.Config, db *sqlitedb.DB) error {
	s.claudeMu.Lock()
	s.claudeCfg = cfg
	s.claudeMu.Unlock()

	if !cfg.WatchEnabled() {
		slog.Info("claudecode: bridge disabled")
		return nil
	}
	if db == nil {
		slog.Warn("claudecode: no database, bridge disabled")
		return nil
	}

	root := claudecode.DefaultRoot()
	if root == "" {
		slog.Warn("claudecode: cannot determine home directory, bridge disabled")
		return nil
	}

	sink, ok := s.store.(claudecode.SessionSink)
	if !ok {
		slog.Warn("claudecode: session store does not support external sessions, bridge disabled")
		return nil
	}

	ing := claudecode.NewIngester(sink, claudecode.NewIngestStore(db), s)

	// Compute everything locally, then assign once under a single lock — easier
	// to see as correct than locking around each individual field write.
	s.claudeMu.Lock()
	s.claudeCfg = cfg
	s.claudeIngester = ing
	s.claudeRoot = root
	s.claudeWatching = true
	s.claudeMu.Unlock()

	if cfg.Watch.Backfill {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("claudecode: backfill panicked, bridge backfill abandoned",
						"panic", r, "stack", string(debug.Stack()))
				}
			}()
			res, err := claudecode.Backfill(ctx, root, ing, cfg.Watch.MaxFileMB)
			if err != nil {
				slog.Warn("claudecode: backfill failed", "err", err)
				return
			}
			slog.Info("claudecode: backfill complete",
				"files", res.Files, "messages", res.Messages, "skipped", res.Skipped)
		}()
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("claudecode: watcher panicked, bridge watching stopped",
					"panic", r, "stack", string(debug.Stack()))
			}
		}()
		if err := claudecode.NewWatcher(root, ing).Run(ctx); err != nil {
			slog.Warn("claudecode: watcher stopped", "err", err)
		}
	}()

	slog.Info("claudecode: bridge started", "root", root)
	return nil
}

func (s *Server) handleClaudeStatus(w http.ResponseWriter, r *http.Request) {
	s.claudeMu.RLock()
	enabled := s.claudeCfg.Enabled
	watching := s.claudeWatching
	root := s.claudeRoot
	s.claudeMu.RUnlock()

	jsonOK(w, map[string]any{
		"enabled":  enabled,
		"watching": watching,
		"root":     root,
	})
}

func (s *Server) handleClaudeBackfill(w http.ResponseWriter, r *http.Request) {
	s.claudeMu.RLock()
	watchEnabled := s.claudeCfg.WatchEnabled()
	ing := s.claudeIngester
	root := s.claudeRoot
	maxFileMB := s.claudeCfg.Watch.MaxFileMB
	s.claudeMu.RUnlock()

	if !watchEnabled || ing == nil {
		jsonError(w, http.StatusConflict,
			"claude code bridge is disabled; set claude_code.enabled in config.json")
		return
	}

	// Do NOT hold the read lock across Backfill — it can run for a long time
	// over a large history and would block every status request meanwhile.
	res, err := claudecode.Backfill(r.Context(), root, ing, maxFileMB)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{
		"files": res.Files, "messages": res.Messages,
		"skipped": res.Skipped, "failed": res.Failed,
	})
}
