package threadmgr

import (
	"context"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
)

// EvaluateDAG walks all queued threads in the session, calls IsReady() on each,
// and spawns any that are now unblocked. Safe to call from multiple goroutines;
// threads are spawned at most once because SpawnThread transitions them out of
// StatusQueued atomically via ThreadManager.Start().
func (tm *ThreadManager) EvaluateDAG(
	ctx context.Context,
	sessionID string,
	store session.StoreInterface,
	sess *session.Session,
	reg *agents.AgentRegistry,
	b backend.Backend,
	broadcast BroadcastFn,
	ca *CostAccumulator,
) {
	queued := tm.ListBySession(sessionID)
	for _, t := range queued {
		if t.Status != StatusQueued {
			continue
		}
		if !tm.IsReady(t.ID) {
			continue
		}
		// Capture tid for the closure
		tid := t.ID
		dagFn := func() {
			tm.EvaluateDAG(ctx, sessionID, store, sess, reg, b, broadcast, ca)
		}
		tm.SpawnThread(ctx, tid, store, sess, reg, b, broadcast, ca, dagFn)
	}
}
