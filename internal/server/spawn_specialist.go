package server

import (
	"context"
	"log/slog"
	"time"
)

// specialistStaleTTL is the TTL sweep fallback for S5 auto-eviction — a
// specialist whose thread never fires its terminal-status hook (process
// crash, stuck watchdog) is evicted from the ephemeral overlay after this
// long regardless. Precedent: server.go's swarmSnapshotTTL (1h) for the
// analogous reconnect-snapshot cleanup; specialists get a longer ceiling
// since a legitimate long-running specialist thread should not be evicted
// out from under itself.
const specialistStaleTTL = 2 * time.Hour

// evictStaleSpecialists runs until ctx is cancelled, sweeping the thread
// manager's specialist-thread tracking for entries older than
// specialistStaleTTL every 15 minutes. Nil-safe — no-ops when s.tm is nil
// (multi-agent not configured).
func (s *Server) evictStaleSpecialists(ctx context.Context) {
	if s.tm == nil {
		return
	}
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if stale := s.tm.EvictStaleSpecialists(specialistStaleTTL); len(stale) > 0 {
				slog.Warn("server: TTL-swept stale ephemeral specialists", "names", stale, "ttl", specialistStaleTTL)
			}
		}
	}
}

// SpaceCompanyIDForSpawn exposes the company a space belongs to (empty for
// desk-level spaces) to callers outside this package — specifically
// main.go's spawn_specialist wiring, which needs it to fix an ephemeral
// specialist's company at spawn time (S12: threadmgr.SetSpecialistCompany).
// Mirrors the unexported SpaceCompanyID closure in create_agent.go's
// NewCreateAgentTool, exported here because spawn_specialist's Deps.Spawn is
// wired in main.go (it also needs threadmgr/agents/backend locals that live
// there, same as delegate_to_agent's wiring).
func (s *Server) SpaceCompanyIDForSpawn(spaceID string) (string, error) {
	type spaceCo interface {
		SpaceCompanyID(spaceID string) (string, error)
	}
	if sc, ok := s.spaceStore.(spaceCo); ok {
		return sc.SpaceCompanyID(spaceID)
	}
	return "", nil
}
