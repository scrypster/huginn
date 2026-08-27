package oneshot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/threadmgr"
	"github.com/scrypster/huginn/internal/tools"
)

// delegationToolNames are registered on the oneshot tool registry and tagged
// "builtin" so applyToolbelt injects them for LocalTools ["*"] and named lists.
var delegationToolNames = []string{
	"delegate_to_agent",
	"list_team_status",
	"recall_thread_result",
	"wait_for_threads",
}

// ephemeralSessionID returns cfg.SessionID when set, otherwise a new ULID.
func ephemeralSessionID(cfg Config) string {
	if id := strings.TrimSpace(cfg.SessionID); id != "" {
		return id
	}
	return session.NewID()
}

// attachDelegation registers the four A2A tools with a real DelegateFn,
// ThreadManager, and SpawnThread — the same stack the server wires at boot.
// spawnCtx must outlive individual tool-call contexts (the lead's request
// ctx is cancelled when a tool Execute returns).
func attachDelegation(
	spawnCtx context.Context,
	toolReg *tools.Registry,
	agentReg *agents.AgentRegistry,
	b backend.Backend,
	sessStore session.StoreInterface,
	sessionID string,
) *threadmgr.ThreadManager {
	tm := threadmgr.New()
	ca := threadmgr.NewCostAccumulator(0)
	// Oneshot has no UI. Preview is always off (HUGINN_DELEGATION_PREVIEW=off).
	previewGate := threadmgr.NewDelegationPreviewGateWithConfig(string(threadmgr.PreviewModeOff), time.Second)

	noopBroadcast := func(string, string, map[string]any) {}

	if sessionID != "" && sessStore != nil {
		if _, err := session.LoadForDelegate(sessStore, sessionID, ""); err != nil {
			// persist failure is non-fatal here; the tool call will surface it
		}
	}

	toolReg.Register(&threadmgr.DelegateToAgentTool{
		Fn: func(ctx context.Context, p threadmgr.DelegateParams) threadmgr.DelegateResult {
			sid := agent.GetSessionID(ctx)
			if sid == "" {
				sid = sessionID
			}
			if sid == "" {
				return threadmgr.DelegateResult{Err: fmt.Errorf("delegate_to_agent: no session ID in context")}
			}
			sess, loadErr := session.LoadForDelegate(sessStore, sid, agent.GetSpaceID(ctx))
			if loadErr != nil {
				return threadmgr.DelegateResult{Err: loadErr}
			}
			if _, found := agentReg.ByName(p.AgentName); !found {
				return threadmgr.DelegateResult{Err: fmt.Errorf("delegate_to_agent: unknown agent %q", p.AgentName)}
			}
			if caller := threadmgr.GetCallingAgent(ctx); caller != "" && strings.EqualFold(caller, p.AgentName) {
				return threadmgr.DelegateResult{Err: fmt.Errorf("delegate_to_agent: cannot delegate to yourself (%s) — do that work directly or pick a specialist", caller)}
			}

			t, createErr := tm.Create(threadmgr.CreateParams{
				SessionID:       sid,
				AgentID:         p.AgentName,
				Task:            p.Task,
				Rationale:       p.Rationale,
				DependsOnHints:  p.DependsOn,
				SpaceID:         sess.SpaceID(),
				ParentMessageID: agent.GetParentMessageID(ctx),
			})
			if createErr != nil {
				return threadmgr.DelegateResult{Err: createErr}
			}

			tm.ResolveDependencies(t.ID)

			if len(p.FileIntents) > 0 {
				conflicts, leaseErr := tm.AcquireLeases(t.ID, p.FileIntents)
				if leaseErr != nil {
					return threadmgr.DelegateResult{Err: leaseErr}
				}
				if len(conflicts) > 0 {
					tm.Cancel(t.ID)
					return threadmgr.DelegateResult{ThreadID: t.ID, Conflicts: conflicts}
				}
			}

			// Oneshot is unattended: preview is always off.
			if !previewGate.Approve(ctx, sid, t.ID, p.AgentName, p.Task, agent.GetParentMessageID(ctx), noopBroadcast) {
				tm.Cancel(t.ID)
				return threadmgr.DelegateResult{
					ThreadID: t.ID,
					Err:      fmt.Errorf("delegation to %q was not approved", p.AgentName),
				}
			}

			if tm.IsReady(t.ID) {
				tid := t.ID
				childCtx := threadmgr.CarryDelegationContext(spawnCtx, ctx)
				dagFn := func() {
					tm.EvaluateDAG(childCtx, sid, sessStore, sess, agentReg, b, noopBroadcast, ca)
				}
				tm.SpawnThread(childCtx, tid, sessStore, sess, agentReg, b, noopBroadcast, ca, dagFn)
				return threadmgr.DelegateResult{ThreadID: t.ID, Spawned: true}
			}
			return threadmgr.DelegateResult{ThreadID: t.ID, Spawned: false}
		},
	})

	toolReg.Register(&threadmgr.ListTeamStatusTool{
		Fn: func(ctx context.Context) ([]*threadmgr.Thread, error) {
			sid := agent.GetSessionID(ctx)
			if sid == "" {
				sid = sessionID
			}
			if sid == "" {
				return nil, fmt.Errorf("no session ID in context")
			}
			return tm.ListBySession(sid), nil
		},
	})

	toolReg.Register(&threadmgr.RecallThreadResultTool{
		Fn: func(ctx context.Context, threadID string) (*threadmgr.Thread, error) {
			sid := agent.GetSessionID(ctx)
			if sid == "" {
				sid = sessionID
			}
			t, ok := tm.Get(threadID)
			if !ok {
				return nil, fmt.Errorf("thread %q not found", threadID)
			}
			if sid != "" && t.SessionID != sid {
				return nil, fmt.Errorf("thread %q not found", threadID)
			}
			return t, nil
		},
	})

	toolReg.Register(&threadmgr.WaitForThreadsTool{
		Fn: func(ctx context.Context, threadIDs []string, timeout time.Duration) (threadmgr.WaitReport, error) {
			sid := agent.GetSessionID(ctx)
			if sid == "" {
				sid = sessionID
			}
			if sid == "" {
				return threadmgr.WaitReport{}, fmt.Errorf("no session ID in context")
			}
			return tm.WaitForThreads(ctx, sid, threadIDs, timeout), nil
		},
	})

	toolReg.TagTools(delegationToolNames, "builtin")

	tm.SetToolRegistry(toolReg)
	tm.SetToolExecutor(func(ctx context.Context, name string, args map[string]any) (string, error) {
		return toolReg.Execute(ctx, name, args)
	})
	return tm
}
