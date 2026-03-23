package tui

import (
	"testing"

	"github.com/scrypster/huginn/internal/swarm"
)

// TestSwarmEventMsg_UpdatesSwarmView verifies that a swarmEventMsg dispatched
// to the app updates the SwarmViewModel correctly.
func TestSwarmEventMsg_UpdatesSwarmView(t *testing.T) {
	a := newTestApp()
	a.state = stateSwarm

	// First register agent "alice" so the SwarmViewModel knows about it.
	a.swarmView = NewSwarmViewModel(80, 24)
	a.swarmView.AddAgent("alice", "alice", "#ff0")

	// Simulate a token event from swarm agent "alice"
	msg := swarmEventMsg{event: swarm.SwarmEvent{
		AgentID:   "alice",
		AgentName: "alice",
		Type:      swarm.EventToken,
		Payload:   "Hello",
	}}

	m, _ := a.Update(msg)
	got := m.(*App)

	// SwarmView should have agent "alice" registered
	view := got.swarmView.View()
	if view == "" {
		t.Error("expected non-empty swarm view after token event")
	}
}

// TestSwarmDoneMsg_TransitionsToChat verifies swarmDoneMsg returns to stateChat.
func TestSwarmDoneMsg_TransitionsToChat(t *testing.T) {
	a := newTestApp()
	a.state = stateSwarm

	m, _ := a.Update(swarmDoneMsg{output: "swarm complete"})
	got := m.(*App)

	if got.state != stateChat {
		t.Errorf("expected stateChat after swarmDoneMsg, got %d", got.state)
	}
}
