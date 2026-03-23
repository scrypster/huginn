package tui

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

// ============================================================
// sidebarModel — SetAgentActive tests
// ============================================================

// buildSidebarWithAgents returns a sidebarModel populated with the given agent names.
func buildSidebarWithAgents(names ...string) sidebarModel {
	s := newSidebarModel()
	s.visible = true
	reg := agents.NewRegistry()
	for _, n := range names {
		reg.Register(&agents.Agent{Name: n, Color: "#58A6FF"})
	}
	s.SetAgents(reg)
	return s
}

func TestSidebar_SetAgentActive_MarksAgentActive(t *testing.T) {
	s := buildSidebarWithAgents("Alpha")
	s.SetAgentActive("Alpha", true)

	for _, it := range s.items {
		if it.name == "Alpha" {
			if !it.active {
				t.Error("expected Alpha to be marked active")
			}
			return
		}
	}
	t.Error("Alpha not found in sidebar items")
}

func TestSidebar_SetAgentActive_MarksAgentInactive(t *testing.T) {
	s := buildSidebarWithAgents("Beta")
	// Set active first, then clear it.
	s.SetAgentActive("Beta", true)
	s.SetAgentActive("Beta", false)

	for _, it := range s.items {
		if it.name == "Beta" {
			if it.active {
				t.Error("expected Beta to be marked inactive after clearing")
			}
			return
		}
	}
	t.Error("Beta not found in sidebar items")
}

func TestSidebar_SetAgentActive_UnknownAgent_NoPanic(t *testing.T) {
	s := buildSidebarWithAgents("Gamma")
	// Should not panic — unknown name is a no-op.
	s.SetAgentActive("NonExistent", true)
	// No assertion needed — test passes if it doesn't panic.
}

func TestSidebar_MultipleAgentsActive(t *testing.T) {
	s := buildSidebarWithAgents("Alpha", "Beta", "Gamma")
	s.SetAgentActive("Alpha", true)
	s.SetAgentActive("Beta", true)
	s.SetAgentActive("Gamma", true)

	activeCount := 0
	for _, it := range s.items {
		if it.kind == sidebarSectionDMs && it.active {
			activeCount++
		}
	}
	if activeCount != 3 {
		t.Errorf("expected 3 active agents, got %d", activeCount)
	}
}

func TestSidebar_ActiveThenInactive_ClearsState(t *testing.T) {
	s := buildSidebarWithAgents("Delta", "Echo")
	s.SetAgentActive("Delta", true)
	s.SetAgentActive("Echo", true)
	// Now clear Delta.
	s.SetAgentActive("Delta", false)

	for _, it := range s.items {
		if it.name == "Delta" && it.active {
			t.Error("Delta should be inactive after clearing")
		}
		if it.name == "Echo" && !it.active {
			t.Error("Echo should still be active")
		}
	}
}

func TestSidebar_SetAgentActive_RenderContainsWorkingIndicator(t *testing.T) {
	s := buildSidebarWithAgents("Foxtrot")
	s.SetAgentActive("Foxtrot", true)
	rendered := s.View(30)
	if !strings.Contains(rendered, "working") {
		t.Errorf("expected 'working' indicator in sidebar render, got:\n%s", rendered)
	}
}

func TestSidebar_SetAgentActive_Inactive_NoWorkingIndicator(t *testing.T) {
	s := buildSidebarWithAgents("Golf")
	// Leave inactive — should not show "working".
	rendered := s.View(30)
	if strings.Contains(rendered, "working") {
		t.Errorf("inactive agent should not show 'working' indicator, got:\n%s", rendered)
	}
}

func TestSidebar_SetAgents_NilRegistry_ClearsItems(t *testing.T) {
	s := buildSidebarWithAgents("Hotel")
	s.SetAgents(nil)
	if len(s.items) != 0 {
		t.Errorf("expected empty items after nil registry, got %d", len(s.items))
	}
}

func TestSidebar_SetAgents_PreservesChannels(t *testing.T) {
	s := newSidebarModel()
	s.visible = true
	s.SetChannels([]string{"general", "alerts"})

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "India", Color: "#3FB950"})
	s.SetAgents(reg)

	channelCount := 0
	for _, it := range s.items {
		if it.kind == sidebarSectionChannels {
			channelCount++
		}
	}
	if channelCount != 2 {
		t.Errorf("expected 2 channel items preserved, got %d", channelCount)
	}
}

func TestSidebar_AutoShow_WidthBelowMin_NotVisible(t *testing.T) {
	s := newSidebarModel()
	s.AutoShow(80)
	if s.IsVisible() {
		t.Error("sidebar should not be visible when terminal width < sidebarMinTotal")
	}
}

func TestSidebar_AutoShow_WidthAtMin_IsVisible(t *testing.T) {
	s := newSidebarModel()
	s.AutoShow(sidebarMinTotal)
	if !s.IsVisible() {
		t.Errorf("sidebar should be visible at width=%d", sidebarMinTotal)
	}
}

func TestSidebar_SetActive_UpdatesCursor(t *testing.T) {
	s := buildSidebarWithAgents("Juliet", "Kilo")
	s.visible = true
	s.SetActive("Kilo")
	if s.active != "Kilo" {
		t.Errorf("expected active='Kilo', got %q", s.active)
	}
}

func TestSidebar_SetChannels_AppearsInRender(t *testing.T) {
	s := newSidebarModel()
	s.visible = true
	s.SetChannels([]string{"ops-alerts"})
	rendered := s.View(30)
	if !strings.Contains(rendered, "ops-alerts") {
		t.Errorf("expected channel name in sidebar render, got:\n%s", rendered)
	}
}

func TestSidebar_EmptyItems_RenderNone(t *testing.T) {
	s := newSidebarModel()
	s.visible = true
	rendered := s.View(30)
	if !strings.Contains(rendered, "(none)") {
		t.Errorf("expected '(none)' for empty sidebar sections, got:\n%s", rendered)
	}
}

func TestSidebar_SetAgentActive_ChannelItem_NoMatch(t *testing.T) {
	// SetAgentActive only affects DM items, not channel items.
	s := newSidebarModel()
	s.visible = true
	s.SetChannels([]string{"general"})
	// This should be a no-op and not panic.
	s.SetAgentActive("general", true)

	for _, it := range s.items {
		if it.kind == sidebarSectionChannels && it.active {
			t.Error("channel item should not be marked active by SetAgentActive")
		}
	}
}

// ============================================================
// WS event delegation tests (handleWsEventMsg via Update)
// ============================================================

func TestWsEvent_AgentBriefingStart_AddsLine(t *testing.T) {
	a := newMinimalApp()

	payload := map[string]any{"agent_name": "Lima"}
	model, _ := a.Update(wsEventMsg{Type: "agent_briefing_start", Payload: payload})
	updated := model.(*App)

	if len(updated.chat.history) == 0 {
		t.Fatal("expected chat history to have an entry after agent_briefing_start")
	}
	found := false
	for _, line := range updated.chat.history {
		if strings.Contains(line.content, "briefing from memory") && line.agentName == "Lima" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected briefing-start line for 'Lima', history: %v", updated.chat.history)
	}
}

func TestWsEvent_AgentBriefingStart_MarksAgentActiveInSidebar(t *testing.T) {
	a := newMinimalApp()
	// Populate sidebar with the agent.
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Mike", Color: "#3FB950"})
	a.agentReg = reg
	a.sidebar = buildSidebarWithAgents("Mike")

	payload := map[string]any{"agent_name": "Mike"}
	model, _ := a.Update(wsEventMsg{Type: "agent_briefing_start", Payload: payload})
	updated := model.(*App)

	if !updated.activeAgents["Mike"] {
		t.Error("expected Mike to be in activeAgents map after briefing_start")
	}
	// Sidebar item should also be marked active.
	for _, it := range updated.sidebar.items {
		if it.name == "Mike" && !it.active {
			t.Error("expected sidebar item for Mike to be marked active")
		}
	}
}

func TestWsEvent_AgentBriefingDone_ReplacesLine(t *testing.T) {
	a := newMinimalApp()

	// First send a briefing_start to populate the history.
	a.Update(wsEventMsg{
		Type:    "agent_briefing_start",
		Payload: map[string]any{"agent_name": "November"},
	})

	histLenBefore := len(a.chat.history)

	// Now send briefing_done — it should replace, not append.
	model, _ := a.Update(wsEventMsg{
		Type: "agent_briefing_done",
		Payload: map[string]any{
			"agent_name":      "November",
			"memories_loaded": float64(5),
			"artifacts_loaded": float64(2),
		},
	})
	updated := model.(*App)

	// History length should be the same (replace, not append).
	if len(updated.chat.history) != histLenBefore {
		t.Errorf("expected history length %d (in-place replace), got %d",
			histLenBefore, len(updated.chat.history))
	}

	// The replaced line should contain "ready".
	found := false
	for _, line := range updated.chat.history {
		if line.agentName == "November" && strings.Contains(line.content, "ready") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'ready' line for 'November' after briefing_done, history: %v", updated.chat.history)
	}
}

func TestWsEvent_AgentBriefingDone_NoStartLine_Appends(t *testing.T) {
	a := newMinimalApp()

	// No preceding briefing_start — done event should append a new line.
	model, _ := a.Update(wsEventMsg{
		Type: "agent_briefing_done",
		Payload: map[string]any{
			"agent_name":      "Oscar",
			"memories_loaded": float64(3),
		},
	})
	updated := model.(*App)

	if len(updated.chat.history) == 0 {
		t.Fatal("expected a new line appended when no start line exists")
	}
	found := false
	for _, line := range updated.chat.history {
		if line.agentName == "Oscar" && strings.Contains(line.content, "ready") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected appended 'ready' line for 'Oscar', history: %v", updated.chat.history)
	}
}

func TestWsEvent_AgentBriefingStart_EmptyAgentName_Ignored(t *testing.T) {
	a := newMinimalApp()
	histBefore := len(a.chat.history)

	// Empty agent_name — should be ignored.
	model, _ := a.Update(wsEventMsg{
		Type:    "agent_briefing_start",
		Payload: map[string]any{"agent_name": ""},
	})
	updated := model.(*App)

	if len(updated.chat.history) != histBefore {
		t.Errorf("expected no history change for empty agent_name, before=%d after=%d",
			histBefore, len(updated.chat.history))
	}
}

func TestWsEvent_BriefingDone_MemoriesCount(t *testing.T) {
	a := newMinimalApp()

	model, _ := a.Update(wsEventMsg{
		Type: "agent_briefing_done",
		Payload: map[string]any{
			"agent_name":      "Papa",
			"memories_loaded": float64(7),
			"artifacts_loaded": float64(0),
		},
	})
	updated := model.(*App)

	found := false
	for _, line := range updated.chat.history {
		if line.agentName == "Papa" && strings.Contains(line.content, "7") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected '7' in readyLine for 'Papa', history: %v", updated.chat.history)
	}
}

func TestWsEvent_AgentBriefingDone_MarksAgentIdleInSidebar(t *testing.T) {
	a := newMinimalApp()
	a.sidebar = buildSidebarWithAgents("Quebec")

	// First activate.
	a.Update(wsEventMsg{
		Type:    "agent_briefing_start",
		Payload: map[string]any{"agent_name": "Quebec"},
	})

	// Then deactivate via briefing_done.
	model, _ := a.Update(wsEventMsg{
		Type: "agent_briefing_done",
		Payload: map[string]any{
			"agent_name":      "Quebec",
			"memories_loaded": float64(0),
		},
	})
	updated := model.(*App)

	// activeAgents map should not contain Quebec.
	if updated.activeAgents != nil && updated.activeAgents["Quebec"] {
		t.Error("expected Quebec to be removed from activeAgents after briefing_done")
	}
}

func TestWsEvent_PrimaryAgentChanged(t *testing.T) {
	a := newMinimalApp()

	payload := map[string]any{"agent": "Romeo"}
	model, _ := a.Update(wsEventMsg{Type: "primary_agent_changed", Payload: payload})
	updated := model.(*App)

	if updated.primaryAgent != "Romeo" {
		t.Errorf("expected primaryAgent='Romeo', got %q", updated.primaryAgent)
	}
}

func TestWsEvent_CostUpdate(t *testing.T) {
	a := newMinimalApp()
	a.sessionCostUSD = 0

	payload := map[string]any{"total": float64(2.50)}
	model, _ := a.Update(wsEventMsg{Type: "cost_update", Payload: payload})
	updated := model.(*App)

	if updated.sessionCostUSD != 2.50 {
		t.Errorf("expected sessionCostUSD=2.50, got %f", updated.sessionCostUSD)
	}
}

func TestWsEvent_UnknownType_NoAction(t *testing.T) {
	a := newMinimalApp()
	a.primaryAgent = "Sierra"
	histBefore := len(a.chat.history)

	model, _ := a.Update(wsEventMsg{
		Type:    "completely_unknown_event",
		Payload: map[string]any{"data": "irrelevant"},
	})
	updated := model.(*App)

	if updated.primaryAgent != "Sierra" {
		t.Error("unknown WS event should not modify primaryAgent")
	}
	if len(updated.chat.history) != histBefore {
		t.Error("unknown WS event should not modify chat history")
	}
}

func TestWsEvent_NilPayload_NoPanic(t *testing.T) {
	a := newMinimalApp()

	// nil Payload on agent_briefing_start and agent_briefing_done — must not panic.
	a.Update(wsEventMsg{Type: "agent_briefing_start", Payload: nil})
	a.Update(wsEventMsg{Type: "agent_briefing_done", Payload: nil})
	a.Update(wsEventMsg{Type: "swarm_status", Payload: nil})
	// Test passes if it doesn't panic.
}

func TestWsEvent_AgentBriefingStart_InitialisesActiveAgentsMap(t *testing.T) {
	a := newMinimalApp()
	// activeAgents is nil initially — briefing_start must initialise the map.
	a.activeAgents = nil

	model, _ := a.Update(wsEventMsg{
		Type:    "agent_briefing_start",
		Payload: map[string]any{"agent_name": "Tango"},
	})
	updated := model.(*App)

	if updated.activeAgents == nil {
		t.Fatal("expected activeAgents map to be initialised")
	}
	if !updated.activeAgents["Tango"] {
		t.Error("expected Tango to be in activeAgents after briefing_start")
	}
}

// ============================================================
// Agent identity helpers — agentColorFromName / agentIconFromName
// ============================================================

func TestAgentColorFromName_Deterministic_Workforce(t *testing.T) {
	names := []string{"Uniform", "Victor", "Whiskey", "X-Ray", "Yankee", "Zulu"}
	for _, name := range names {
		c1 := agentColorFromName(name)
		c2 := agentColorFromName(name)
		if c1 != c2 {
			t.Errorf("agentColorFromName(%q) not deterministic: got %q then %q", name, c1, c2)
		}
		if c1 == "" {
			t.Errorf("agentColorFromName(%q) returned empty string", name)
		}
	}
}

func TestAgentColorFromName_EmptyString_NoPanic(t *testing.T) {
	color := agentColorFromName("")
	// Should not panic and should return a palette entry.
	if color == "" {
		t.Error("agentColorFromName(\"\") returned empty string, expected a palette color")
	}
	found := false
	for _, p := range agentPalette {
		if p == color {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("agentColorFromName(\"\") = %q, not in palette", color)
	}
}

func TestAgentColorFromName_LongName_ValidColor(t *testing.T) {
	longName := strings.Repeat("LongAgentName", 50)
	color := agentColorFromName(longName)
	if color == "" {
		t.Error("expected non-empty color for long name")
	}
	found := false
	for _, p := range agentPalette {
		if p == color {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("agentColorFromName(longName) = %q, not in palette", color)
	}
}

func TestAgentIconFromName_ReturnsString(t *testing.T) {
	names := []string{"Alpha", "beta", "Charlie", "delta"}
	for _, name := range names {
		icon := agentIconFromName(name)
		if icon == "" {
			t.Errorf("agentIconFromName(%q) returned empty string", name)
		}
	}
}

func TestAgentColorFromName_DifferentNames_CanDiffer(t *testing.T) {
	// With a 6-color palette and diverse names, we expect at least 2 distinct
	// colors across a set of 10 names.
	names := []string{"Able", "Baker", "Charlie", "Dog", "Easy", "Fox", "George", "How", "Item", "Jig"}
	seen := map[string]bool{}
	for _, n := range names {
		seen[agentColorFromName(n)] = true
	}
	if len(seen) < 2 {
		t.Errorf("expected at least 2 distinct colors for %d names, got %d distinct", len(names), len(seen))
	}
}

func TestAgentIconFromName_EmptyName_ReturnsQuestionMark(t *testing.T) {
	icon := agentIconFromName("")
	if icon != "?" {
		t.Errorf("expected '?' for empty name, got %q", icon)
	}
}

func TestAgentIconFromName_UppercasesFirstRune(t *testing.T) {
	icon := agentIconFromName("zulu")
	if icon != "Z" {
		t.Errorf("expected 'Z' for 'zulu', got %q", icon)
	}
}

func TestAgentColorFromName_AllNamesInPalette(t *testing.T) {
	names := []string{"A", "B", "C", "D", "E", "F", "G"}
	for _, name := range names {
		color := agentColorFromName(name)
		found := false
		for _, p := range agentPalette {
			if p == color {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("agentColorFromName(%q) = %q, not in agentPalette", name, color)
		}
	}
}
