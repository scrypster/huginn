package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/swarm"
	"github.com/scrypster/huginn/internal/threadmgr"
)

// capturedBroadcast records all broadcast calls for assertion.
type capturedBroadcast struct {
	mu   sync.Mutex
	calls []broadcastCall
}

type broadcastCall struct {
	sessionID string
	msgType   string
	payload   map[string]any
}

func (c *capturedBroadcast) fn() threadmgr.BroadcastFn {
	return func(sessionID, msgType string, payload map[string]any) {
		c.mu.Lock()
		defer c.mu.Unlock()
		// Clone payload to avoid mutation after capture.
		cloned := make(map[string]any, len(payload))
		for k, v := range payload {
			cloned[k] = v
		}
		c.calls = append(c.calls, broadcastCall{sessionID, msgType, cloned})
	}
}

func (c *capturedBroadcast) byType(msgType string) []broadcastCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []broadcastCall
	for _, call := range c.calls {
		if call.msgType == msgType {
			out = append(out, call)
		}
	}
	return out
}

func (c *capturedBroadcast) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

// runBridgeWithSwarm creates a swarm, runs tasks, bridges events, and waits for completion.
func runBridgeWithSwarm(t *testing.T, tasks []swarm.SwarmTask, cap *capturedBroadcast) {
	t.Helper()
	ctx := context.Background()
	sw := swarm.NewSwarm(len(tasks))

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		BridgeSwarmEvents(ctx, sw, "sess-1", tasks, cap.fn(), nil)
	}()

	sw.Run(ctx, tasks) //nolint:errcheck
	wg.Wait()
}

// ── agentStatusString ─────────────────────────────────────────────────────────

func TestAgentStatusString(t *testing.T) {
	cases := []struct {
		status swarm.AgentStatus
		want   string
	}{
		{swarm.StatusQueued, "waiting"},
		{swarm.StatusThinking, "running"},
		{swarm.StatusTooling, "running"},
		{swarm.StatusDone, "done"},
		{swarm.StatusError, "error"},
		{swarm.StatusCancelled, "cancelled"},
		{swarm.AgentStatus(99), "waiting"}, // unknown → default
	}
	for _, tc := range cases {
		if got := agentStatusString(tc.status); got != tc.want {
			t.Errorf("agentStatusString(%d) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

// ── swarm_start synthesis ─────────────────────────────────────────────────────

func TestBridgeSwarmEvents_EmitsSwarmStart(t *testing.T) {
	cap := &capturedBroadcast{}
	tasks := []swarm.SwarmTask{
		{ID: "a1", Name: "Agent 1", Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error { return nil }},
		{ID: "a2", Name: "Agent 2", Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error { return nil }},
	}
	runBridgeWithSwarm(t, tasks, cap)

	starts := cap.byType("swarm_start")
	if len(starts) != 1 {
		t.Fatalf("expected 1 swarm_start, got %d", len(starts))
	}
	agents, ok := starts[0].payload["agents"].([]map[string]any)
	if !ok {
		t.Fatalf("swarm_start payload.agents unexpected type")
	}
	if len(agents) != 2 {
		t.Errorf("expected 2 agents in swarm_start, got %d", len(agents))
	}
	if agents[0]["id"] != "a1" || agents[0]["name"] != "Agent 1" {
		t.Errorf("unexpected agent[0]: %v", agents[0])
	}
}

func TestBridgeSwarmEvents_SwarmStartUsesTaskList(t *testing.T) {
	cap := &capturedBroadcast{}
	tasks := []swarm.SwarmTask{
		{ID: "x", Name: "X Agent", Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error { return nil }},
	}
	runBridgeWithSwarm(t, tasks, cap)

	starts := cap.byType("swarm_start")
	agents := starts[0].payload["agents"].([]map[string]any)
	if agents[0]["id"] != "x" || agents[0]["name"] != "X Agent" {
		t.Errorf("wrong agent info: %v", agents[0])
	}
}

// ── swarm_complete ────────────────────────────────────────────────────────────

func TestBridgeSwarmEvents_EmitsSwarmComplete(t *testing.T) {
	cap := &capturedBroadcast{}
	tasks := []swarm.SwarmTask{
		{ID: "a1", Name: "A1", Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error { return nil }},
	}
	runBridgeWithSwarm(t, tasks, cap)

	completes := cap.byType("swarm_complete")
	if len(completes) != 1 {
		t.Fatalf("expected 1 swarm_complete, got %d", len(completes))
	}
	if completes[0].payload["cancelled"] != false {
		t.Errorf("expected cancelled=false for normal completion")
	}
}

func TestBridgeSwarmEvents_CancelledOnContextDone(t *testing.T) {
	cap := &capturedBroadcast{}
	ctx, cancel := context.WithCancel(context.Background())

	sw := swarm.NewSwarm(1)
	tasks := []swarm.SwarmTask{
		{
			ID:   "a1",
			Name: "Slow Agent",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(10 * time.Second):
					return nil
				}
			},
		},
	}

	var bridgeDone sync.WaitGroup
	bridgeDone.Add(1)
	go func() {
		defer bridgeDone.Done()
		BridgeSwarmEvents(ctx, sw, "sess-cancel", tasks, cap.fn(), nil)
	}()

	// Cancel context quickly
	go func() {
		sw.Run(ctx, tasks) //nolint:errcheck
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	bridgeDone.Wait()

	completes := cap.byType("swarm_complete")
	if len(completes) == 0 {
		t.Fatal("expected swarm_complete after context cancel")
	}
	if completes[0].payload["cancelled"] != true {
		t.Errorf("expected cancelled=true, got %v", completes[0].payload["cancelled"])
	}
}

// ── status events ─────────────────────────────────────────────────────────────

func TestBridgeSwarmEvents_EmitsStatusChanges(t *testing.T) {
	cap := &capturedBroadcast{}
	tasks := []swarm.SwarmTask{
		{
			ID:   "a1",
			Name: "A1",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				// The swarm core emits EventStatusChange(StatusThinking) automatically.
				// This test verifies the bridge forwards non-terminal status changes.
				return nil
			},
		},
	}
	runBridgeWithSwarm(t, tasks, cap)

	// At minimum we expect a swarm_agent_status with status "done" from EventComplete.
	statuses := cap.byType("swarm_agent_status")
	if len(statuses) == 0 {
		t.Fatal("expected at least one swarm_agent_status event")
	}

	// Find the terminal "done" event.
	var foundDone bool
	for _, s := range statuses {
		if s.payload["status"] == "done" && s.payload["success"] == true {
			foundDone = true
		}
	}
	if !foundDone {
		t.Errorf("expected swarm_agent_status with status=done success=true, got: %v", statuses)
	}
}

func TestBridgeSwarmEvents_NoTerminalStatusFromEventStatusChange(t *testing.T) {
	// EventStatusChange with StatusDone should be suppressed (EventComplete handles it).
	cap := &capturedBroadcast{}
	ev := swarm.SwarmEvent{
		AgentID:   "a1",
		AgentName: "A1",
		Type:      swarm.EventStatusChange,
		Payload:   swarm.StatusDone,
	}
	tokenBufs := make(map[string]*strings.Builder)
	callsBefore := cap.count()
	handleSwarmEvent(ev, "sess-1", cap.fn(), tokenBufs)
	if cap.count() != callsBefore {
		t.Error("EventStatusChange(StatusDone) should NOT broadcast (handled by EventComplete)")
	}

	ev.Payload = swarm.StatusError
	handleSwarmEvent(ev, "sess-1", cap.fn(), tokenBufs)
	if cap.count() != callsBefore {
		t.Error("EventStatusChange(StatusError) should NOT broadcast (handled by EventError)")
	}
}

func TestBridgeSwarmEvents_ErrorAgentEmitsFailedStatus(t *testing.T) {
	cap := &capturedBroadcast{}
	tasks := []swarm.SwarmTask{
		{
			ID:   "fail-agent",
			Name: "Fail",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				return errors.New("something went wrong")
			},
		},
	}
	runBridgeWithSwarm(t, tasks, cap)

	statuses := cap.byType("swarm_agent_status")
	var foundError bool
	for _, s := range statuses {
		if s.payload["status"] == "error" && s.payload["success"] == false {
			foundError = true
		}
	}
	if !foundError {
		t.Errorf("expected swarm_agent_status with status=error success=false, got: %v", statuses)
	}
}

// ── token batching ────────────────────────────────────────────────────────────

func TestHandleSwarmEvent_TokenAccumulates(t *testing.T) {
	cap := &capturedBroadcast{}
	tokenBufs := make(map[string]*strings.Builder)

	handleSwarmEvent(swarm.SwarmEvent{AgentID: "a1", Type: swarm.EventToken, Payload: "hello "}, "s1", cap.fn(), tokenBufs)
	handleSwarmEvent(swarm.SwarmEvent{AgentID: "a1", Type: swarm.EventToken, Payload: "world"}, "s1", cap.fn(), tokenBufs)

	// Nothing broadcast yet (batched).
	if cap.count() > 0 {
		t.Errorf("tokens should be batched, not broadcast immediately")
	}
	if tokenBufs["a1"].String() != "hello world" {
		t.Errorf("token buffer = %q, want %q", tokenBufs["a1"].String(), "hello world")
	}
}

func TestHandleSwarmEvent_TokenFlushOnFlush(t *testing.T) {
	cap := &capturedBroadcast{}
	tokenBufs := make(map[string]*strings.Builder)

	handleSwarmEvent(swarm.SwarmEvent{AgentID: "a1", Type: swarm.EventToken, Payload: "data"}, "s1", cap.fn(), tokenBufs)

	// Simulate flush (as the ticker would do).
	buf := tokenBufs["a1"]
	if buf.Len() > 0 {
		cap.fn()("s1", "swarm_agent_token", map[string]any{
			"agent_id": "a1",
			"content":  buf.String(),
		})
		buf.Reset()
	}

	tokens := cap.byType("swarm_agent_token")
	if len(tokens) != 1 {
		t.Fatalf("expected 1 swarm_agent_token after flush, got %d", len(tokens))
	}
	if tokens[0].payload["content"] != "data" {
		t.Errorf("expected content=data, got %v", tokens[0].payload["content"])
	}
}

func TestHandleSwarmEvent_EmptyTokenIgnored(t *testing.T) {
	cap := &capturedBroadcast{}
	tokenBufs := make(map[string]*strings.Builder)
	handleSwarmEvent(swarm.SwarmEvent{AgentID: "a1", Type: swarm.EventToken, Payload: ""}, "s1", cap.fn(), tokenBufs)
	if tokenBufs["a1"] != nil && tokenBufs["a1"].Len() > 0 {
		t.Error("empty token should not be buffered")
	}
}

// ── tool events ───────────────────────────────────────────────────────────────

func TestHandleSwarmEvent_ToolStart(t *testing.T) {
	cap := &capturedBroadcast{}
	tokenBufs := make(map[string]*strings.Builder)
	handleSwarmEvent(swarm.SwarmEvent{
		AgentID: "a1", AgentName: "A1",
		Type:    swarm.EventToolStart,
		Payload: "bash",
	}, "s1", cap.fn(), tokenBufs)

	starts := cap.byType("swarm_agent_tool_start")
	if len(starts) != 1 {
		t.Fatalf("expected 1 swarm_agent_tool_start, got %d", len(starts))
	}
	if starts[0].payload["tool_name"] != "bash" {
		t.Errorf("tool_name = %v, want bash", starts[0].payload["tool_name"])
	}
}

func TestHandleSwarmEvent_ToolDone(t *testing.T) {
	cap := &capturedBroadcast{}
	tokenBufs := make(map[string]*strings.Builder)
	handleSwarmEvent(swarm.SwarmEvent{
		AgentID: "a1", AgentName: "A1",
		Type:    swarm.EventToolDone,
		Payload: "bash",
	}, "s1", cap.fn(), tokenBufs)

	dones := cap.byType("swarm_agent_tool_done")
	if len(dones) != 1 || dones[0].payload["tool_name"] != "bash" {
		t.Errorf("unexpected swarm_agent_tool_done: %v", dones)
	}
}

// ── session ID propagation ────────────────────────────────────────────────────

func TestBridgeSwarmEvents_SessionIDPropagated(t *testing.T) {
	cap := &capturedBroadcast{}
	tasks := []swarm.SwarmTask{
		{ID: "a1", Name: "A1", Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error { return nil }},
	}
	runBridgeWithSwarm(t, tasks, cap)

	cap.mu.Lock()
	defer cap.mu.Unlock()
	for _, call := range cap.calls {
		if call.sessionID != "sess-1" {
			t.Errorf("expected sessionID=sess-1, got %q (msgType=%s)", call.sessionID, call.msgType)
		}
	}
}
