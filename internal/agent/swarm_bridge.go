package agent

import (
	"context"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/swarm"
	"github.com/scrypster/huginn/internal/threadmgr"
)

// swarmTokenFlushMs is the interval at which buffered token content is flushed
// to the WebSocket relay. Batching reduces WS message volume when many agents
// stream tokens simultaneously.
const swarmTokenFlushMs = 75

// BridgeSwarmEvents reads lifecycle events from sw.Events() and broadcasts
// them as WebSocket messages to the given session.
//
// It must be called in a goroutine alongside sw.Run():
//
//	go agent.BridgeSwarmEvents(ctx, sw, sessionID, tasks, broadcastFn)
//	results, errs, _, _, err := sw.Run(ctx, tasks)
//
// The function exits when ctx is cancelled (emits swarm_complete{cancelled:true})
// or when the events channel closes after Run() finishes (emits swarm_complete{cancelled:false}).
//
// tasks is used to synthesize the initial swarm_start event, since the swarm
// core does not emit EventSwarmReady via the event channel for web consumers.
// BridgeSwarmEvents reads lifecycle events from sw.Events() and broadcasts
// them as WebSocket messages. snapshotFn, if non-nil, is called with the
// swarm_complete payload BEFORE the broadcast so reconnect state is available
// immediately when the client receives the event.
func BridgeSwarmEvents(
	ctx context.Context,
	sw *swarm.Swarm,
	sessionID string,
	tasks []swarm.SwarmTask,
	broadcast threadmgr.BroadcastFn,
	snapshotFn func(sessionID string, payload map[string]any),
) {
	// Synthesize swarm_start from task list before any agent events arrive.
	agents := make([]map[string]any, len(tasks))
	for i, t := range tasks {
		agents[i] = map[string]any{"id": t.ID, "name": t.Name}
	}
	broadcast(sessionID, "swarm_start", map[string]any{"agents": agents})

	tokenBufs := make(map[string]*strings.Builder)
	flushTicker := time.NewTicker(swarmTokenFlushMs * time.Millisecond)
	defer flushTicker.Stop()

	// Drop-rate warning thresholds: warn at these cumulative counts, then every 500 beyond 200.
	// lastWarnedAt is local to this goroutine — no synchronization needed.
	dropWarnThresholds := [...]int64{10, 50, 200}
	var lastWarnedAt int64

	maybeWarnDrops := func() {
		current := sw.DroppedEvents()
		if current == 0 {
			return
		}
		// Check fixed thresholds.
		for _, threshold := range dropWarnThresholds {
			if current >= threshold && lastWarnedAt < threshold {
				broadcast(sessionID, "swarm_drop_warning", map[string]any{
					"dropped": current, "threshold": threshold,
				})
				lastWarnedAt = current
				return
			}
		}
		// Open-ended: warn every 500 additional drops after the 200 threshold.
		if current >= 200 && (current-lastWarnedAt) >= 500 {
			broadcast(sessionID, "swarm_drop_warning", map[string]any{
				"dropped": current, "threshold": int64(0),
			})
			lastWarnedAt = current
		}
	}

	flushTokens := func() {
		for agentID, buf := range tokenBufs {
			if buf.Len() > 0 {
				broadcast(sessionID, "swarm_agent_token", map[string]any{
					"agent_id": agentID,
					"content":  buf.String(),
				})
				buf.Reset()
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			flushTokens()
			completionPayload := map[string]any{
				"cancelled":      true,
				"dropped_events": sw.DroppedEvents(),
			}
			if snapshotFn != nil {
				snapshotFn(sessionID, completionPayload)
			}
			broadcast(sessionID, "swarm_complete", completionPayload)
			return

		case <-flushTicker.C:
			flushTokens()
			maybeWarnDrops()

		case ev, ok := <-sw.Events():
			if !ok {
				// Channel closed: swarm finished normally.
				flushTokens()
				completionPayload := map[string]any{
					"cancelled":      false,
					"dropped_events": sw.DroppedEvents(),
				}
				if snapshotFn != nil {
					snapshotFn(sessionID, completionPayload)
				}
				broadcast(sessionID, "swarm_complete", completionPayload)
				return
			}
			handleSwarmEvent(ev, sessionID, broadcast, tokenBufs)
			maybeWarnDrops()
		}
	}
}

// handleSwarmEvent converts a single swarm.SwarmEvent into a WebSocket broadcast.
// Token events are accumulated in tokenBufs for batched flushing.
func handleSwarmEvent(
	ev swarm.SwarmEvent,
	sessionID string,
	broadcast threadmgr.BroadcastFn,
	tokenBufs map[string]*strings.Builder,
) {
	switch ev.Type {
	case swarm.EventToken:
		if content, ok := ev.Payload.(string); ok && content != "" {
			if tokenBufs[ev.AgentID] == nil {
				tokenBufs[ev.AgentID] = &strings.Builder{}
			}
			tokenBufs[ev.AgentID].WriteString(content)
		}

	case swarm.EventStatusChange:
		if status, ok := ev.Payload.(swarm.AgentStatus); ok {
			// Terminal statuses (Done, Error) are handled by EventComplete/EventError
			// to avoid duplicate status events on the success/error path.
			if status == swarm.StatusDone || status == swarm.StatusError {
				return
			}
			broadcast(sessionID, "swarm_agent_status", map[string]any{
				"agent_id":   ev.AgentID,
				"agent_name": ev.AgentName,
				"status":     agentStatusString(status),
			})
		}

	case swarm.EventComplete:
		broadcast(sessionID, "swarm_agent_status", map[string]any{
			"agent_id":   ev.AgentID,
			"agent_name": ev.AgentName,
			"status":     "done",
			"success":    true,
		})

	case swarm.EventError:
		errMsg := ""
		if err, ok := ev.Payload.(error); ok {
			errMsg = err.Error()
		}
		broadcast(sessionID, "swarm_agent_status", map[string]any{
			"agent_id":   ev.AgentID,
			"agent_name": ev.AgentName,
			"status":     "error",
			"success":    false,
			"error":      errMsg,
		})

	case swarm.EventToolStart:
		if toolName, ok := ev.Payload.(string); ok {
			broadcast(sessionID, "swarm_agent_tool_start", map[string]any{
				"agent_id":  ev.AgentID,
				"tool_name": toolName,
			})
		}

	case swarm.EventToolDone:
		if toolName, ok := ev.Payload.(string); ok {
			broadcast(sessionID, "swarm_agent_tool_done", map[string]any{
				"agent_id":  ev.AgentID,
				"tool_name": toolName,
			})
		}
	}
}

// agentStatusString maps a swarm.AgentStatus to the string values used by
// the frontend SwarmState type: 'waiting' | 'running' | 'done' | 'error' | 'cancelled'.
func agentStatusString(s swarm.AgentStatus) string {
	switch s {
	case swarm.StatusQueued:
		return "waiting"
	case swarm.StatusThinking:
		return "running"
	case swarm.StatusTooling:
		return "running"
	case swarm.StatusDone:
		return "done"
	case swarm.StatusError:
		return "error"
	case swarm.StatusCancelled:
		return "cancelled"
	default:
		return "waiting"
	}
}
