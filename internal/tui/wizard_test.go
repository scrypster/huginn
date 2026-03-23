package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/scrypster/huginn/internal/skills"
)

// --- filteredCommands (method form) ---

func TestFilteredCommands_EmptyFilter_ReturnsAll(t *testing.T) {
	w := newWizardModel()
	cmds := w.filteredCommands("")
	if len(cmds) != len(utilityCommands) {
		t.Errorf("expected %d commands, got %d", len(utilityCommands), len(cmds))
	}
}

func TestFilteredCommands_WhitespaceOnly_ReturnsAll(t *testing.T) {
	w := newWizardModel()
	cmds := w.filteredCommands("   ")
	if len(cmds) != len(utilityCommands) {
		t.Errorf("expected %d commands for whitespace-only filter, got %d", len(utilityCommands), len(cmds))
	}
}

func TestFilteredCommands_ExactNameMatch(t *testing.T) {
	w := newWizardModel()
	cmds := w.filteredCommands("stats")
	found := false
	for _, c := range cmds {
		if c.Name == "stats" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'stats' command in results for filter 'stats'")
	}
}

func TestFilteredCommands_PartialMatch(t *testing.T) {
	// "wo" should match "workspace"
	w := newWizardModel()
	cmds := w.filteredCommands("wo")
	found := false
	for _, c := range cmds {
		if c.Name == "workspace" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'workspace' in partial match results for filter 'wo'")
	}
}

func TestFilteredCommands_NoMatch_ReturnsEmpty(t *testing.T) {
	w := newWizardModel()
	cmds := w.filteredCommands("zzznomatch")
	if len(cmds) != 0 {
		t.Errorf("expected empty results for unmatchable filter, got %d", len(cmds))
	}
}

func TestFilteredCommands_CaseInsensitive(t *testing.T) {
	w := newWizardModel()
	cmds := w.filteredCommands("STATS")
	found := false
	for _, c := range cmds {
		if c.Name == "stats" {
			found = true
		}
	}
	if !found {
		t.Error("expected filteredCommands to be case-insensitive")
	}
}

func TestFilteredCommands_DescriptionMatch(t *testing.T) {
	// "keybindings" should match the "help" command description
	w := newWizardModel()
	cmds := w.filteredCommands("keybindings")
	found := false
	for _, c := range cmds {
		if c.Name == "help" {
			found = true
		}
	}
	if !found {
		t.Error("expected description-based match for 'keybindings'")
	}
}

// --- commonPrefix ---

func TestCommonPrefix_EmptySlice(t *testing.T) {
	result := commonPrefix(nil)
	if result != "" {
		t.Errorf("expected empty string for nil slice, got %q", result)
	}
}

func TestCommonPrefix_OneCommand(t *testing.T) {
	cmds := []SlashCommand{{Name: "stats"}}
	result := commonPrefix(cmds)
	if result != "stats" {
		t.Errorf("expected 'stats' for single-item slice, got %q", result)
	}
}

func TestCommonPrefix_AllSame(t *testing.T) {
	cmds := []SlashCommand{{Name: "stats"}, {Name: "stats"}, {Name: "stats"}}
	result := commonPrefix(cmds)
	if result != "stats" {
		t.Errorf("expected 'stats' when all names are same, got %q", result)
	}
}

func TestCommonPrefix_CommonPrefixPartial(t *testing.T) {
	// "swarm" and "stats" both start with "s"
	cmds := []SlashCommand{{Name: "swarm"}, {Name: "stats"}}
	result := commonPrefix(cmds)
	if result != "s" {
		t.Errorf("expected 's' as common prefix, got %q", result)
	}
}

func TestCommonPrefix_NoCommonPrefix(t *testing.T) {
	// "workspace" and "radar" share no prefix
	cmds := []SlashCommand{{Name: "workspace"}, {Name: "radar"}}
	result := commonPrefix(cmds)
	if result != "" {
		t.Errorf("expected empty string for disjoint names, got %q", result)
	}
}

func TestCommonPrefix_MultipleWithSharedPrefix(t *testing.T) {
	cmds := []SlashCommand{
		{Name: "workspace"},
		{Name: "wizard"},
	}
	result := commonPrefix(cmds)
	// Both start with "w"
	if len(result) < 1 || result[0] != 'w' {
		t.Errorf("expected prefix starting with 'w', got %q", result)
	}
}

// --- WizardModel: Show / Hide / Visible ---

func TestWizardModel_ShowMakesVisible(t *testing.T) {
	w := newWizardModel()
	w.Show("")
	if !w.Visible() {
		t.Error("expected Visible()=true after Show()")
	}
}

func TestWizardModel_HideMakesInvisible(t *testing.T) {
	w := newWizardModel()
	w.Show("")
	w.Hide()
	if w.Visible() {
		t.Error("expected Visible()=false after Hide()")
	}
}

func TestWizardModel_HideClearsState(t *testing.T) {
	w := newWizardModel()
	w.Show("wo")
	w.cursor = 2
	w.Hide()

	if w.filter != "" {
		t.Errorf("expected empty filter after Hide(), got %q", w.filter)
	}
	if w.cursor != 0 {
		t.Errorf("expected cursor=0 after Hide(), got %d", w.cursor)
	}
	if w.filtered != nil {
		t.Error("expected filtered=nil after Hide()")
	}
}

func TestWizardModel_ShowWithFilter_FiltersCommands(t *testing.T) {
	w := newWizardModel()
	w.Show("workspace")
	if len(w.filtered) == 0 {
		t.Error("expected at least one filtered command after Show('workspace')")
	}
	for _, c := range w.filtered {
		if c.Name != "workspace" && !containsString(c.Description, "workspace") {
			t.Errorf("unexpected command in filtered list for 'workspace': %q", c.Name)
		}
	}
}

func TestWizardModel_ShowClampsOOBCursor(t *testing.T) {
	w := newWizardModel()
	w.cursor = 999
	w.Show("workspace") // only a few matches
	if w.cursor >= len(w.filtered) {
		t.Errorf("cursor %d out of bounds for filtered len %d", w.cursor, len(w.filtered))
	}
}

// --- WizardModel.UpdateFilter ---

func TestWizardModel_UpdateFilter_Refilters(t *testing.T) {
	w := newWizardModel()
	w.Show("")
	allLen := len(w.filtered)

	w.UpdateFilter("workspace")
	if len(w.filtered) >= allLen {
		t.Errorf("expected fewer results after filtering, all=%d filtered=%d", allLen, len(w.filtered))
	}
}

func TestWizardModel_UpdateFilter_ClampsOOBCursor(t *testing.T) {
	w := newWizardModel()
	w.Show("")
	w.cursor = len(w.filtered) - 1 // last item in full list
	w.UpdateFilter("workspace")    // much shorter list
	if w.cursor >= len(w.filtered) {
		t.Errorf("cursor %d is OOB for filtered len %d", w.cursor, len(w.filtered))
	}
}

// --- WizardModel.Update: keyboard handling ---

func TestWizardUpdate_NotVisible_NoOp(t *testing.T) {
	w := newWizardModel()
	// visible is false by default
	updated, cmd := w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.Visible() {
		t.Error("expected invisible wizard to remain invisible after key press")
	}
	if cmd != nil {
		t.Error("expected nil cmd when wizard is not visible")
	}
}

func TestWizardUpdate_EscDismisses(t *testing.T) {
	w := newWizardModel()
	w.Show("")

	updated, cmd := w.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.Visible() {
		t.Error("expected wizard to be hidden after Esc")
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd after Esc")
	}
	msg := cmd()
	if _, ok := msg.(WizardDismissMsg); !ok {
		t.Errorf("expected WizardDismissMsg, got %T", msg)
	}
}

func TestWizardUpdate_EnterSelectsCommand(t *testing.T) {
	w := newWizardModel()
	w.Show("stats")
	w.cursor = 0

	updated, cmd := w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.Visible() {
		t.Error("expected wizard hidden after Enter")
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd after Enter")
	}
	msg := cmd()
	sel, ok := msg.(WizardSelectMsg)
	if !ok {
		t.Fatalf("expected WizardSelectMsg, got %T", msg)
	}
	if sel.Command.Name == "" {
		t.Error("expected non-empty command name in WizardSelectMsg")
	}
}

func TestWizardUpdate_EnterOnEmptyList_NoOp(t *testing.T) {
	w := newWizardModel()
	w.Show("zzznomatch")

	updated, cmd := w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = updated
	if cmd != nil {
		t.Error("expected nil cmd when Enter pressed on empty list")
	}
}

// --- Tab: 1 match → full name completion ---

// TestWizardUpdate_Tab_OneMatch_FullCompletion directly injects a single-item
// filtered list so the test does not depend on utilityCommands description text.
func TestWizardUpdate_Tab_OneMatch_FullCompletion(t *testing.T) {
	w := newWizardModel()
	w.visible = true
	w.filtered = []SlashCommand{{Name: "workspace", Description: "show workspace"}}

	_, cmd := w.Update(tea.KeyMsg{Type: tea.KeyTab})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from Tab with one match")
	}
	msg := cmd()
	tabMsg, ok := msg.(WizardTabCompleteMsg)
	if !ok {
		t.Fatalf("expected WizardTabCompleteMsg, got %T", msg)
	}
	if tabMsg.Text != "workspace" {
		t.Errorf("expected full completion 'workspace', got %q", tabMsg.Text)
	}
}

// --- Tab: multiple matches → common prefix ---

func TestWizardUpdate_Tab_MultipleMatches_CommonPrefix(t *testing.T) {
	w := newWizardModel()
	// "s" matches "stats", "save", "swarm"
	w.Show("s")
	if len(w.filtered) < 2 {
		t.Skipf("expected at least 2 matches for 's', got %d", len(w.filtered))
	}

	_, cmd := w.Update(tea.KeyMsg{Type: tea.KeyTab})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from Tab")
	}
	msg := cmd()
	tabMsg, ok := msg.(WizardTabCompleteMsg)
	if !ok {
		t.Fatalf("expected WizardTabCompleteMsg, got %T", msg)
	}
	// The returned text must be a valid prefix of every filtered command.
	for _, c := range w.filtered {
		if len(tabMsg.Text) > 0 && len(c.Name) >= len(tabMsg.Text) {
			if c.Name[:len(tabMsg.Text)] != tabMsg.Text {
				t.Errorf("text %q is not a prefix of command %q", tabMsg.Text, c.Name)
			}
		}
	}
}

// --- Tab: 0 matches → no-op (nil cmd) ---

func TestWizardUpdate_Tab_NoMatches_NoOp(t *testing.T) {
	w := newWizardModel()
	w.Show("zzznomatch")

	_, cmd := w.Update(tea.KeyMsg{Type: tea.KeyTab})
	if cmd != nil {
		t.Error("expected nil cmd when Tab pressed with no matches")
	}
}

// --- Up/Down cursor navigation ---

func TestWizardUpdate_Down_MovesCursor(t *testing.T) {
	w := newWizardModel()
	w.Show("")
	w.cursor = 0

	updated, _ := w.Update(tea.KeyMsg{Type: tea.KeyDown})
	if updated.cursor != 1 {
		t.Errorf("expected cursor=1 after Down, got %d", updated.cursor)
	}
}

func TestWizardUpdate_Down_DoesNotExceedBound(t *testing.T) {
	w := newWizardModel()
	w.Show("")
	w.cursor = len(w.filtered) - 1

	updated, _ := w.Update(tea.KeyMsg{Type: tea.KeyDown})
	if updated.cursor != len(w.filtered)-1 {
		t.Errorf("expected cursor to stay at last item, got %d", updated.cursor)
	}
}

func TestWizardUpdate_Up_MovesCursorBack(t *testing.T) {
	w := newWizardModel()
	w.Show("")
	w.cursor = 2

	updated, _ := w.Update(tea.KeyMsg{Type: tea.KeyUp})
	if updated.cursor != 1 {
		t.Errorf("expected cursor=1 after Up, got %d", updated.cursor)
	}
}

func TestWizardUpdate_Up_DoesNotGoBelowZero(t *testing.T) {
	w := newWizardModel()
	w.Show("")
	w.cursor = 0

	updated, _ := w.Update(tea.KeyMsg{Type: tea.KeyUp})
	if updated.cursor != 0 {
		t.Errorf("expected cursor to stay at 0, got %d", updated.cursor)
	}
}

func TestWizardUpdate_CtrlN_MovesCursorDown(t *testing.T) {
	w := newWizardModel()
	w.Show("")
	w.cursor = 0

	updated, _ := w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n"), Alt: false})
	// ctrl+n is sent as a special key
	w2, _ := w.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	_ = updated
	if w2.cursor != 1 {
		t.Errorf("expected cursor=1 after ctrl+n, got %d", w2.cursor)
	}
}

func TestWizardUpdate_CtrlP_MovesCursorUp(t *testing.T) {
	w := newWizardModel()
	w.Show("")
	w.cursor = 3

	w2, _ := w.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if w2.cursor != 2 {
		t.Errorf("expected cursor=2 after ctrl+p, got %d", w2.cursor)
	}
}

// --- WizardTabCompleteMsg content accuracy ---

func TestWizardTabCompleteMsg_TextMatchesCommonPrefix(t *testing.T) {
	// Build two commands that share the prefix "sw"
	cmds := []SlashCommand{
		{Name: "swarm", Description: "show swarm"},
		{Name: "sw-other", Description: "other sw command"},
	}
	prefix := commonPrefix(cmds)
	if prefix != "sw" {
		t.Errorf("expected common prefix 'sw', got %q", prefix)
	}

	// Now simulate what Tab does in the wizard.
	w := newWizardModel()
	w.visible = true
	w.filtered = cmds
	_, cmd := w.Update(tea.KeyMsg{Type: tea.KeyTab})
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	msg := cmd()
	tabMsg, ok := msg.(WizardTabCompleteMsg)
	if !ok {
		t.Fatalf("expected WizardTabCompleteMsg, got %T", msg)
	}
	if tabMsg.Text != prefix {
		t.Errorf("WizardTabCompleteMsg.Text %q does not match commonPrefix %q", tabMsg.Text, prefix)
	}
}

// --- View rendering (smoke tests — no panic, correct visibility) ---

func TestWizardView_NotVisible_EmptyString(t *testing.T) {
	w := newWizardModel()
	out := w.View(80)
	if out != "" {
		t.Errorf("expected empty string from View when not visible, got %q", out)
	}
}

func TestWizardView_Visible_NonEmpty(t *testing.T) {
	w := newWizardModel()
	w.Show("")
	out := w.View(80)
	if out == "" {
		t.Error("expected non-empty View output when wizard is visible")
	}
}

func TestWizardView_NoMatches_ShowsNoCommandsMatch(t *testing.T) {
	w := newWizardModel()
	w.Show("zzznomatch")
	out := w.View(80)
	if out == "" {
		t.Error("expected non-empty View even with no matches")
	}
}

// --- Registry integration ---

func TestWizardModel_RegistrySkillsAppearInFilteredCommands(t *testing.T) {
	reg := skills.NewSkillRegistry()
	if errs := reg.LoadBuiltins(); len(errs) > 0 {
		t.Fatalf("LoadBuiltins: %v", errs)
	}
	w := newWizardModel()
	w.SetRegistry(reg)
	w.Show("")

	// plan should appear (it's a built-in skill)
	var found bool
	for _, c := range w.filtered {
		if c.Name == "plan" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'plan' skill from registry to appear in filtered commands")
	}
}

// --- helper ---

func containsString(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
