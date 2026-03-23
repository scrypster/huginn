package agent

import (
	"context"
	"fmt"
	"sync"

	huginsession "github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/swarm"
	"github.com/scrypster/huginn/internal/threadmgr"
)

// SpawnThread launches a delegated thread goroutine using the orchestrator's internal
// agent registry, backend, and session store. The goroutine is tied to ctx (use the
// server lifecycle context — not the HTTP request context — so the thread runs until
// server shutdown, independent of the triggering request). This is intentional
// fire-and-forget from the HTTP handler's perspective.
//
// wg, if non-nil, is incremented before launching and decremented on completion, enabling
// graceful drain during server shutdown.
//
// ca, if non-nil, is used for cost tracking; if nil, a new unlimited accumulator is created
// per thread (zero budget = unlimited, as documented in NewCostAccumulator).
func (o *Orchestrator) SpawnThread(ctx context.Context, threadID string, sess *huginsession.Session, tm *threadmgr.ThreadManager, broadcast threadmgr.BroadcastFn, wg *sync.WaitGroup, ca *threadmgr.CostAccumulator) {
	o.mu.RLock()
	reg := o.agentReg
	store := o.sessionStore
	b := o.backend
	o.mu.RUnlock()

	if ca == nil {
		// 0 = unlimited budget (documented contract in NewCostAccumulator).
		ca = threadmgr.NewCostAccumulator(0)
	}

	if wg != nil {
		wg.Add(1)
	}
	go func() {
		if wg != nil {
			defer wg.Done()
		}
		tm.SpawnThread(ctx, threadID, store, sess, reg, b, broadcast, ca, nil)
	}()
}

// BuildSwarmTask constructs a SwarmTask that runs a named agent on a given prompt.
// The agent runs via ChatWithAgent and its token output is streamed through the
// swarm event channel.
func (o *Orchestrator) BuildSwarmTask(agentName, prompt, sessionID string) (swarm.SwarmTask, error) {
	o.mu.RLock()
	reg := o.agentReg
	o.mu.RUnlock()
	if reg == nil {
		return swarm.SwarmTask{}, fmt.Errorf("agent registry not available")
	}

	ag, ok := reg.ByName(agentName)
	if !ok {
		return swarm.SwarmTask{}, fmt.Errorf("agent %q not found", agentName)
	}

	taskID := "agent-" + agentName
	return swarm.SwarmTask{
		ID:   taskID,
		Name: agentName,
		Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
			return o.ChatWithAgent(ctx, ag, prompt, sessionID+"-"+agentName,
				// onToken
				func(token string) {
					emit(swarm.SwarmEvent{
						AgentID:   taskID,
						AgentName: agentName,
						Type:      swarm.EventToken,
						Payload:   token,
					})
				},
				// onToolEvent: wire EventToolStart/EventToolDone and StatusTooling correctly.
				func(eventType string, payload map[string]any) {
					switch eventType {
					case "tool_call":
						toolName, _ := payload["tool"].(string)
						// Transition agent status to StatusTooling.
						emit(swarm.SwarmEvent{
							AgentID:   taskID,
							AgentName: agentName,
							Type:      swarm.EventStatusChange,
							Payload:   swarm.StatusTooling,
						})
						// Emit the tool-start event so the TUI and WS bridge
						// can display which tool is running.
						emit(swarm.SwarmEvent{
							AgentID:   taskID,
							AgentName: agentName,
							Type:      swarm.EventToolStart,
							Payload:   toolName,
						})
					case "tool_result":
						toolName, _ := payload["tool"].(string)
						// Emit the tool-done event.
						emit(swarm.SwarmEvent{
							AgentID:   taskID,
							AgentName: agentName,
							Type:      swarm.EventToolDone,
							Payload:   toolName,
						})
						// Transition agent status back to StatusThinking.
						emit(swarm.SwarmEvent{
							AgentID:   taskID,
							AgentName: agentName,
							Type:      swarm.EventStatusChange,
							Payload:   swarm.StatusThinking,
						})
					}
				},
				nil, // onEvent
			)
		},
	}, nil
}
