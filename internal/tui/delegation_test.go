package tui

import (
	"strings"
	"testing"
)

func TestDelegationStartMsg_UpdateAddsBox(t *testing.T) {
	a := newMinimalApp()
	a.state = stateStreaming

	model, _ := a.Update(delegationStartMsg{From: "Steve", To: "Mark", Question: "Is this safe?"})
	updated := model.(*App)

	if len(updated.chat.history) == 0 {
		t.Fatal("expected history entry for delegation start")
	}
	last := updated.chat.history[len(updated.chat.history)-1]
	if last.role != "delegation-start" {
		t.Errorf("expected role 'delegation-start', got %q", last.role)
	}
}

func TestDelegationTokenMsg_UpdateAppendsDelegationContent(t *testing.T) {
	a := newMinimalApp()
	a.state = stateStreaming
	a.delegationBuf = ""

	a.Update(delegationStartMsg{From: "Steve", To: "Mark", Question: "Q?"})
	model, _ := a.Update(delegationTokenMsg{Agent: "Mark", Token: "insight"})
	updated := model.(*App)

	if updated.delegationBuf != "insight" {
		t.Errorf("expected delegationBuf='insight', got %q", updated.delegationBuf)
	}
}

func TestDelegationDoneMsg_UpdateFlushesBox(t *testing.T) {
	a := newMinimalApp()
	a.state = stateStreaming
	a.delegationBuf = "accumulated answer"

	model, _ := a.Update(delegationDoneMsg{From: "Steve", To: "Mark", Answer: "final answer"})
	updated := model.(*App)

	// delegationBuf should be cleared
	if updated.delegationBuf != "" {
		t.Errorf("expected delegationBuf cleared, got %q", updated.delegationBuf)
	}
	// History should have a delegation-done entry
	found := false
	for _, h := range updated.chat.history {
		if h.role == "delegation-done" {
			found = true
		}
	}
	if !found {
		t.Error("expected delegation-done entry in history")
	}
}

func TestStyleDelegationBox_ReturnsStyle(t *testing.T) {
	style := StyleDelegationBox("#58A6FF")
	rendered := style.Render("test content")
	if rendered == "" {
		t.Error("expected non-empty rendered delegation box")
	}
}

// ============================================================
// Primary agent header
// ============================================================

func TestPrimaryAgentHeader_ShowsCurrentAgent(t *testing.T) {
	a := newMinimalApp()
	a.SetPrimaryAgent("Alex")
	view := a.HeaderView()
	if !strings.Contains(view, "Alex") {
		t.Errorf("HeaderView should show primary agent name, got: %q", view)
	}
}

func TestPrimaryAgentHeader_ContainsSuffix(t *testing.T) {
	a := newMinimalApp()
	a.SetPrimaryAgent("Alex")
	view := a.HeaderView()
	if !strings.Contains(view, "▾") {
		t.Errorf("HeaderView should contain '▾' suffix, got: %q", view)
	}
}

func TestPrimaryAgentHeader_EmptyWhenNoAgent(t *testing.T) {
	a := newMinimalApp()
	// No primary agent set — HeaderView should return "" and not crash.
	view := a.HeaderView()
	if view != "" {
		t.Errorf("HeaderView should be empty when no primary agent is set, got: %q", view)
	}
}

func TestPrimaryAgent_WSEvent_UpdatesModel(t *testing.T) {
	a := newMinimalApp()

	payload := map[string]any{"agent": "Jordan"}
	model, _ := a.Update(wsEventMsg{Type: "primary_agent_changed", Payload: payload})
	updated := model.(*App)

	if updated.primaryAgent != "Jordan" {
		t.Errorf("expected primaryAgent='Jordan' after primary_agent_changed event, got %q", updated.primaryAgent)
	}
}

func TestPrimaryAgent_WSEvent_IgnoresEmptyName(t *testing.T) {
	a := newMinimalApp()
	a.primaryAgent = "Existing"

	payload := map[string]any{"agent": ""}
	model, _ := a.Update(wsEventMsg{Type: "primary_agent_changed", Payload: payload})
	updated := model.(*App)

	// Empty name should not overwrite existing value.
	if updated.primaryAgent != "Existing" {
		t.Errorf("expected primaryAgent unchanged, got %q", updated.primaryAgent)
	}
}

func TestPrimaryAgent_WSEvent_UnknownType_NoOp(t *testing.T) {
	a := newMinimalApp()
	a.primaryAgent = "OriginalAgent"

	// Unknown WS event type should not change anything.
	model, _ := a.Update(wsEventMsg{Type: "some_other_event", Payload: map[string]any{"agent": "ShouldBeIgnored"}})
	updated := model.(*App)

	if updated.primaryAgent != "OriginalAgent" {
		t.Errorf("unknown WS event should not change primaryAgent, got %q", updated.primaryAgent)
	}
}

// ============================================================
// Session cost display in header
// ============================================================

func TestHeader_ShowsCostWhenPositive(t *testing.T) {
	a := newMinimalApp()
	a.SetPrimaryAgent("Alex")
	a.sessionCostUSD = 1.23
	view := a.HeaderView()
	if !strings.Contains(view, "$1.23") {
		t.Errorf("expected cost in header, got: %q", view)
	}
}

func TestHeader_HidesCostWhenZero(t *testing.T) {
	a := newMinimalApp()
	a.SetPrimaryAgent("Alex")
	a.sessionCostUSD = 0
	view := a.HeaderView()
	if strings.Contains(view, "$") {
		t.Errorf("header should not show cost when zero, got: %q", view)
	}
}
