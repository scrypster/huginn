package tui

import (
	"strings"
	"testing"
)

// ============================================================
// muninnCallLabel
// ============================================================

func TestMuninnCallLabel_Recall(t *testing.T) {
	for _, name := range []string{"muninn_recall", "muninn_recall_tree"} {
		got := muninnCallLabel(name)
		if got != "recalling from memory" {
			t.Errorf("muninnCallLabel(%q) = %q, want 'recalling from memory'", name, got)
		}
	}
}

func TestMuninnCallLabel_Remember(t *testing.T) {
	for _, name := range []string{"muninn_remember", "muninn_remember_batch", "muninn_remember_tree"} {
		got := muninnCallLabel(name)
		if got != "storing in memory" {
			t.Errorf("muninnCallLabel(%q) = %q, want 'storing in memory'", name, got)
		}
	}
}

func TestMuninnCallLabel_Read(t *testing.T) {
	got := muninnCallLabel("muninn_read")
	if got != "reading memory" {
		t.Errorf("muninnCallLabel('muninn_read') = %q, want 'reading memory'", got)
	}
}

func TestMuninnCallLabel_Search(t *testing.T) {
	for _, name := range []string{"muninn_find_by_entity", "muninn_entities", "muninn_similar_entities", "muninn_entity_clusters"} {
		got := muninnCallLabel(name)
		if got != "searching memory" {
			t.Errorf("muninnCallLabel(%q) = %q, want 'searching memory'", name, got)
		}
	}
}

func TestMuninnCallLabel_Link(t *testing.T) {
	got := muninnCallLabel("muninn_link")
	if got != "linking memories" {
		t.Errorf("muninnCallLabel('muninn_link') = %q, want 'linking memories'", got)
	}
}

func TestMuninnCallLabel_Forget(t *testing.T) {
	got := muninnCallLabel("muninn_forget")
	if got != "forgetting" {
		t.Errorf("muninnCallLabel('muninn_forget') = %q, want 'forgetting'", got)
	}
}

func TestMuninnCallLabel_Evolve(t *testing.T) {
	for _, name := range []string{"muninn_evolve", "muninn_consolidate"} {
		got := muninnCallLabel(name)
		if got != "evolving memory" {
			t.Errorf("muninnCallLabel(%q) = %q, want 'evolving memory'", name, got)
		}
	}
}

func TestMuninnCallLabel_State(t *testing.T) {
	for _, name := range []string{"muninn_session", "muninn_state"} {
		got := muninnCallLabel(name)
		if got != "checking memory state" {
			t.Errorf("muninnCallLabel(%q) = %q, want 'checking memory state'", name, got)
		}
	}
}

func TestMuninnCallLabel_WhereLeftOff(t *testing.T) {
	got := muninnCallLabel("muninn_where_left_off")
	if got != "orienting from memory" {
		t.Errorf("muninnCallLabel('muninn_where_left_off') = %q, want 'orienting from memory'", got)
	}
}

func TestMuninnCallLabel_Unknown(t *testing.T) {
	got := muninnCallLabel("muninn_something_new")
	if got != "memory operation" {
		t.Errorf("muninnCallLabel(unknown) = %q, want 'memory operation'", got)
	}
}

func TestMuninnCallLabel_NonMuninn(t *testing.T) {
	got := muninnCallLabel("bash")
	if got != "memory operation" {
		t.Errorf("muninnCallLabel('bash') = %q, want 'memory operation'", got)
	}
}

// ============================================================
// muninnDoneLabel
// ============================================================

func TestMuninnDoneLabel_Recall(t *testing.T) {
	for _, name := range []string{"muninn_recall", "muninn_recall_tree"} {
		got := muninnDoneLabel(name)
		if got != "recalled from memory" {
			t.Errorf("muninnDoneLabel(%q) = %q, want 'recalled from memory'", name, got)
		}
	}
}

func TestMuninnDoneLabel_Remember(t *testing.T) {
	for _, name := range []string{"muninn_remember", "muninn_remember_batch", "muninn_remember_tree"} {
		got := muninnDoneLabel(name)
		if got != "stored in memory" {
			t.Errorf("muninnDoneLabel(%q) = %q, want 'stored in memory'", name, got)
		}
	}
}

func TestMuninnDoneLabel_Read(t *testing.T) {
	got := muninnDoneLabel("muninn_read")
	if got != "read from memory" {
		t.Errorf("muninnDoneLabel('muninn_read') = %q, want 'read from memory'", got)
	}
}

func TestMuninnDoneLabel_Search(t *testing.T) {
	for _, name := range []string{"muninn_find_by_entity", "muninn_entities", "muninn_similar_entities", "muninn_entity_clusters"} {
		got := muninnDoneLabel(name)
		if got != "searched memory" {
			t.Errorf("muninnDoneLabel(%q) = %q, want 'searched memory'", name, got)
		}
	}
}

func TestMuninnDoneLabel_Link(t *testing.T) {
	got := muninnDoneLabel("muninn_link")
	if got != "linked memories" {
		t.Errorf("muninnDoneLabel('muninn_link') = %q, want 'linked memories'", got)
	}
}

func TestMuninnDoneLabel_Forget(t *testing.T) {
	got := muninnDoneLabel("muninn_forget")
	if got != "forgotten" {
		t.Errorf("muninnDoneLabel('muninn_forget') = %q, want 'forgotten'", got)
	}
}

func TestMuninnDoneLabel_Evolve(t *testing.T) {
	for _, name := range []string{"muninn_evolve", "muninn_consolidate"} {
		got := muninnDoneLabel(name)
		if got != "memory evolved" {
			t.Errorf("muninnDoneLabel(%q) = %q, want 'memory evolved'", name, got)
		}
	}
}

func TestMuninnDoneLabel_State(t *testing.T) {
	for _, name := range []string{"muninn_session", "muninn_state"} {
		got := muninnDoneLabel(name)
		if got != "memory checked" {
			t.Errorf("muninnDoneLabel(%q) = %q, want 'memory checked'", name, got)
		}
	}
}

func TestMuninnDoneLabel_WhereLeftOff(t *testing.T) {
	got := muninnDoneLabel("muninn_where_left_off")
	if got != "oriented from memory" {
		t.Errorf("muninnDoneLabel('muninn_where_left_off') = %q, want 'oriented from memory'", got)
	}
}

func TestMuninnDoneLabel_Unknown(t *testing.T) {
	got := muninnDoneLabel("muninn_something_new")
	if got != "memory updated" {
		t.Errorf("muninnDoneLabel(unknown) = %q, want 'memory updated'", got)
	}
}

// ============================================================
// muninnCallIcon
// ============================================================

func TestMuninnCallIcon_RecallOps_LeftArrow(t *testing.T) {
	for _, name := range []string{
		"muninn_recall", "muninn_recall_tree", "muninn_read",
		"muninn_where_left_off", "muninn_find_by_entity",
	} {
		got := muninnCallIcon(name)
		if got != "←" {
			t.Errorf("muninnCallIcon(%q) = %q, want '←'", name, got)
		}
	}
}

func TestMuninnCallIcon_StoreOps_RightArrow(t *testing.T) {
	for _, name := range []string{
		"muninn_remember", "muninn_remember_batch", "muninn_remember_tree",
		"muninn_evolve", "muninn_consolidate", "muninn_forget",
	} {
		got := muninnCallIcon(name)
		if got != "→" {
			t.Errorf("muninnCallIcon(%q) = %q, want '→'", name, got)
		}
	}
}

func TestMuninnCallIcon_NeutralOps_Circle(t *testing.T) {
	for _, name := range []string{"muninn_link", "muninn_session", "muninn_state"} {
		got := muninnCallIcon(name)
		if got != "◎" {
			t.Errorf("muninnCallIcon(%q) = %q, want '◎'", name, got)
		}
	}
}

// ============================================================
// isMuninnRecallOp
// ============================================================

func TestIsMuninnRecallOp_RecallTrue(t *testing.T) {
	for _, name := range []string{
		"muninn_recall", "muninn_recall_tree", "muninn_read",
		"muninn_where_left_off", "muninn_find_by_entity",
	} {
		if !isMuninnRecallOp(name) {
			t.Errorf("isMuninnRecallOp(%q) should be true", name)
		}
	}
}

func TestIsMuninnRecallOp_StoreFalse(t *testing.T) {
	for _, name := range []string{
		"muninn_remember", "muninn_remember_batch", "muninn_evolve",
		"muninn_consolidate", "muninn_forget", "muninn_link",
	} {
		if isMuninnRecallOp(name) {
			t.Errorf("isMuninnRecallOp(%q) should be false for write/neutral ops", name)
		}
	}
}

// ============================================================
// isMuninnEmptyResult
// ============================================================

func TestIsMuninnEmptyResult_Empty(t *testing.T) {
	for _, s := range []string{"", "null", "[]", "{}", "  []  "} {
		if !isMuninnEmptyResult(s) {
			t.Errorf("isMuninnEmptyResult(%q) should be true", s)
		}
	}
}

func TestIsMuninnEmptyResult_ZeroCount(t *testing.T) {
	cases := []string{
		`{"count": 0, "memories": []}`,
		`{"count":0}`,
	}
	for _, s := range cases {
		if !isMuninnEmptyResult(s) {
			t.Errorf("isMuninnEmptyResult(%q) should be true (zero count)", s)
		}
	}
}

func TestIsMuninnEmptyResult_ShortEmptyArray(t *testing.T) {
	s := `{"memories":[]}`
	if !isMuninnEmptyResult(s) {
		t.Errorf("isMuninnEmptyResult(%q) should be true (short empty array wrapper)", s)
	}
}

func TestIsMuninnEmptyResult_HasData(t *testing.T) {
	cases := []string{
		`[{"id":"abc","content":"hello"}]`,
		`{"memories":[{"id":"1"}],"count":1}`,
		`some plain text response`,
	}
	for _, s := range cases {
		if isMuninnEmptyResult(s) {
			t.Errorf("isMuninnEmptyResult(%q) should be false (has data)", s)
		}
	}
}

// ============================================================
// dotFrames & dotPhase animation
// ============================================================

func TestDotFrames_Count(t *testing.T) {
	if len(dotFrames) != 3 {
		t.Errorf("dotFrames should have 3 frames, got %d", len(dotFrames))
	}
}

func TestDotFrames_ContainDots(t *testing.T) {
	for i, frame := range dotFrames {
		if !strings.Contains(frame, "●") && !strings.Contains(frame, "◦") {
			t.Errorf("dotFrames[%d] = %q should contain dot characters", i, frame)
		}
	}
}

func TestDotFrames_AllDifferent(t *testing.T) {
	seen := make(map[string]bool)
	for i, frame := range dotFrames {
		if seen[frame] {
			t.Errorf("dotFrames[%d] = %q is a duplicate", i, frame)
		}
		seen[frame] = true
	}
}

func TestDotPhase_AnimatesOnDotTick(t *testing.T) {
	a := newRenderApp()
	a.state = stateStreaming

	initialPhase := a.dotPhase
	model, _ := a.Update(dotTickMsg{})
	updated := model.(*App)

	expectedPhase := (initialPhase + 1) % 3
	if updated.dotPhase != expectedPhase {
		t.Errorf("dotPhase after tick: got %d, want %d", updated.dotPhase, expectedPhase)
	}
}

func TestDotPhase_CyclesThrough3Frames(t *testing.T) {
	a := newRenderApp()
	a.state = stateStreaming
	a.dotPhase = 0

	for i := 0; i < 9; i++ {
		model, _ := a.Update(dotTickMsg{})
		a = model.(*App)
	}
	// After 9 ticks, phase should be back to 0 (3 complete cycles)
	if a.dotPhase != 0 {
		t.Errorf("after 9 ticks, dotPhase should be 0, got %d", a.dotPhase)
	}
}

func TestDotPhase_NoAdvanceWhenNotStreaming(t *testing.T) {
	a := newRenderApp()
	a.state = stateChat
	a.dotPhase = 1

	model, _ := a.Update(dotTickMsg{})
	updated := model.(*App)

	// Not streaming → dotPhase must not change
	if updated.dotPhase != 1 {
		t.Errorf("dotPhase changed outside stateStreaming: got %d, want 1", updated.dotPhase)
	}
}

// ============================================================
// muninn tool rendering in refreshViewport
// ============================================================

func TestRefreshViewport_MuninnToolCall_ShowsDirectionArrow(t *testing.T) {
	a := newRenderApp()
	a.chat.history = []chatLine{
		{role: "tool-call", content: "muninn_recall args", toolName: "muninn_recall"},
	}
	a.refreshViewport()
	content := a.viewport.View()
	if !strings.Contains(content, "←") {
		t.Errorf("muninn recall tool-call should show ← direction arrow, viewport: %q", content)
	}
	if !strings.Contains(content, "recalling from memory") {
		t.Errorf("muninn recall tool-call should show label, viewport: %q", content)
	}
}

func TestRefreshViewport_MuninnToolCall_StoreShowsOutArrow(t *testing.T) {
	a := newRenderApp()
	a.chat.history = []chatLine{
		{role: "tool-call", content: "muninn_remember args", toolName: "muninn_remember"},
	}
	a.refreshViewport()
	content := a.viewport.View()
	if !strings.Contains(content, "→") {
		t.Errorf("muninn store tool-call should show → direction arrow, viewport: %q", content)
	}
}

func TestRefreshViewport_MuninnToolDone_ShowsFilledCircle(t *testing.T) {
	a := newRenderApp()
	a.chat.history = []chatLine{
		{role: "tool-done", content: "result", toolName: "muninn_remember", fullOutput: `{"id":"abc"}`},
	}
	a.refreshViewport()
	content := a.viewport.View()
	if !strings.Contains(content, "◉") {
		t.Errorf("muninn tool-done with results should show ◉ indicator, viewport: %q", content)
	}
}

func TestRefreshViewport_MuninnToolDone_EmptyRecall_ShowsNothingFound(t *testing.T) {
	a := newRenderApp()
	a.chat.history = []chatLine{
		{role: "tool-done", content: "", toolName: "muninn_recall", fullOutput: "[]"},
	}
	a.refreshViewport()
	content := a.viewport.View()
	if !strings.Contains(content, "nothing in memory yet") {
		t.Errorf("muninn recall with empty result should show 'nothing in memory yet', viewport: %q", content)
	}
	if !strings.Contains(content, "○") {
		t.Errorf("muninn empty recall should show ○ hollow circle, viewport: %q", content)
	}
}

func TestRefreshViewport_RegularToolCall_ShowsDollarSign(t *testing.T) {
	a := newRenderApp()
	a.chat.history = []chatLine{
		{role: "tool-call", content: "ls -la", toolName: "bash"},
	}
	a.refreshViewport()
	content := a.viewport.View()
	if !strings.Contains(content, "$") {
		t.Errorf("non-muninn tool-call should show $ prefix, viewport: %q", content)
	}
}
