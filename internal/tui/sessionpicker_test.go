package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/scrypster/huginn/internal/session"
)

func TestSessionPicker_FilterByTitle(t *testing.T) {
	now := time.Now()
	manifests := []session.Manifest{
		{SessionID: "1", Title: "fix auth bug", UpdatedAt: now},
		{SessionID: "2", Title: "refactor database", UpdatedAt: now.Add(-time.Hour)},
		{SessionID: "3", Title: "add tests", UpdatedAt: now.Add(-2 * time.Hour)},
	}
	picker := newSessionPickerModel(manifests, "/workspace")
	picker.filter = "auth"
	picker.applyFilter()

	if len(picker.filtered) != 1 {
		t.Errorf("expected 1 match for 'auth', got %d", len(picker.filtered))
	}
	if picker.filtered[0].SessionID != "1" {
		t.Errorf("expected session 1, got %s", picker.filtered[0].SessionID)
	}
}

func TestSessionPicker_WorkspaceScopedByDefault(t *testing.T) {
	now := time.Now()
	manifests := []session.Manifest{
		{SessionID: "1", Title: "local", WorkspaceRoot: "/my/workspace", UpdatedAt: now},
		{SessionID: "2", Title: "other", WorkspaceRoot: "/other/workspace", UpdatedAt: now},
	}
	picker := newSessionPickerModel(manifests, "/my/workspace")

	if len(picker.filtered) != 1 {
		t.Errorf("expected 1 scoped session, got %d", len(picker.filtered))
	}
}

func TestSessionPicker_TabTogglesAllWorkspaces(t *testing.T) {
	now := time.Now()
	manifests := []session.Manifest{
		{SessionID: "1", WorkspaceRoot: "/my/workspace", UpdatedAt: now},
		{SessionID: "2", WorkspaceRoot: "/other/workspace", UpdatedAt: now},
	}
	picker := newSessionPickerModel(manifests, "/my/workspace")
	picker.allMode = true
	picker.applyFilter()

	if len(picker.filtered) != 2 {
		t.Errorf("expected 2 sessions in allMode, got %d", len(picker.filtered))
	}
}

func TestSessionPicker_Navigation(t *testing.T) {
	now := time.Now()
	manifests := []session.Manifest{
		{SessionID: "1", Title: "Session 0", UpdatedAt: now},
		{SessionID: "2", Title: "Session 1", UpdatedAt: now.Add(-time.Hour)},
		{SessionID: "3", Title: "Session 2", UpdatedAt: now.Add(-2 * time.Hour)},
	}
	picker := newSessionPickerModel(manifests, "")
	picker.visible = true

	// Navigate down with "j"
	updated, _ := picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if updated.cursor != 1 {
		t.Errorf("cursor after down: got %d, want 1", updated.cursor)
	}

	// Navigate down again
	updated2, _ := updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if updated2.cursor != 2 {
		t.Errorf("cursor after second down: got %d, want 2", updated2.cursor)
	}

	// Navigate up with "k"
	updated3, _ := updated2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if updated3.cursor != 1 {
		t.Errorf("cursor after up: got %d, want 1", updated3.cursor)
	}
}

func TestSessionPicker_NavigationBoundaries(t *testing.T) {
	now := time.Now()
	manifests := []session.Manifest{
		{SessionID: "1", Title: "only session", UpdatedAt: now},
	}
	picker := newSessionPickerModel(manifests, "")
	picker.visible = true

	// Cannot go above 0
	updated, _ := picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if updated.cursor != 0 {
		t.Errorf("cursor should stay at 0 when at top, got %d", updated.cursor)
	}

	// Cannot go below last item
	updated2, _ := updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if updated2.cursor != 0 {
		t.Errorf("cursor should stay at 0 with single item, got %d", updated2.cursor)
	}
}

func TestSessionPicker_EscCancels(t *testing.T) {
	now := time.Now()
	manifests := []session.Manifest{
		{SessionID: "abc", Title: "some session", UpdatedAt: now},
	}
	picker := newSessionPickerModel(manifests, "")
	picker.visible = true

	updated, cmd := picker.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.visible {
		t.Error("picker should not be visible after Esc")
	}
	if cmd == nil {
		t.Fatal("expected a command after Esc, got nil")
	}
	msg := cmd()
	if _, ok := msg.(SessionPickerDismissMsg); !ok {
		t.Errorf("expected SessionPickerDismissMsg, got %T", msg)
	}
}

func TestSessionPicker_EnterSelectsSession(t *testing.T) {
	now := time.Now()
	manifests := []session.Manifest{
		{SessionID: "first", Title: "first session", UpdatedAt: now},
		{SessionID: "second", Title: "second session", UpdatedAt: now.Add(-time.Hour)},
	}
	picker := newSessionPickerModel(manifests, "")
	picker.visible = true
	picker.cursor = 1

	updated, cmd := picker.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.visible {
		t.Error("picker should close after Enter")
	}
	if cmd == nil {
		t.Fatal("expected a command after Enter, got nil")
	}
	msg := cmd()
	pickMsg, ok := msg.(SessionPickerMsg)
	if !ok {
		t.Fatalf("expected SessionPickerMsg, got %T", msg)
	}
	if pickMsg.ID != "second" {
		t.Errorf("expected session ID 'second', got %q", pickMsg.ID)
	}
}

func TestSessionPicker_InvisibleIgnoresKeys(t *testing.T) {
	now := time.Now()
	manifests := []session.Manifest{
		{SessionID: "1", Title: "a", UpdatedAt: now},
		{SessionID: "2", Title: "b", UpdatedAt: now.Add(-time.Hour)},
	}
	picker := newSessionPickerModel(manifests, "")
	// visible is false by default from newSessionPickerModel

	updated, cmd := picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if updated.cursor != 0 {
		t.Errorf("cursor should not change when not visible, got %d", updated.cursor)
	}
	if cmd != nil {
		t.Error("expected no command when not visible")
	}
}

func TestSessionPicker_FilterByWorkspaceName(t *testing.T) {
	now := time.Now()
	manifests := []session.Manifest{
		{SessionID: "1", Title: "task", WorkspaceName: "frontend", UpdatedAt: now},
		{SessionID: "2", Title: "task", WorkspaceName: "backend", UpdatedAt: now},
	}
	picker := newSessionPickerModel(manifests, "")
	picker.filter = "front"
	picker.applyFilter()

	if len(picker.filtered) != 1 {
		t.Errorf("expected 1 match for workspace filter 'front', got %d", len(picker.filtered))
	}
	if picker.filtered[0].SessionID != "1" {
		t.Errorf("expected session 1, got %s", picker.filtered[0].SessionID)
	}
}

func TestSessionPicker_FilterByModel(t *testing.T) {
	now := time.Now()
	manifests := []session.Manifest{
		{SessionID: "1", Title: "task", Model: "claude-3-5-sonnet", UpdatedAt: now},
		{SessionID: "2", Title: "task", Model: "claude-3-opus", UpdatedAt: now},
	}
	picker := newSessionPickerModel(manifests, "")
	picker.filter = "opus"
	picker.applyFilter()

	if len(picker.filtered) != 1 {
		t.Errorf("expected 1 match for model filter 'opus', got %d", len(picker.filtered))
	}
	if picker.filtered[0].SessionID != "2" {
		t.Errorf("expected session 2, got %s", picker.filtered[0].SessionID)
	}
}

func TestSessionPicker_BackspaceRemovesFilterChar(t *testing.T) {
	now := time.Now()
	manifests := []session.Manifest{
		{SessionID: "1", Title: "auth flow", UpdatedAt: now},
		{SessionID: "2", Title: "db migration", UpdatedAt: now},
	}
	picker := newSessionPickerModel(manifests, "")
	picker.visible = true
	picker.filter = "auth"
	picker.applyFilter()

	if len(picker.filtered) != 1 {
		t.Fatalf("setup: expected 1 filtered, got %d", len(picker.filtered))
	}

	updated, _ := picker.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if updated.filter != "aut" {
		t.Errorf("filter after backspace: got %q, want %q", updated.filter, "aut")
	}
}

func TestPickerFormatAge(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{3 * time.Hour, "3h ago"},
		{48 * time.Hour, "2d ago"},
	}
	for _, c := range cases {
		got := pickerFormatAge(time.Now().Add(-c.d))
		if got != c.want {
			t.Errorf("pickerFormatAge(-%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestPickerTruncate(t *testing.T) {
	cases := []struct {
		input string
		max   int
		want  string
	}{
		{"short", 10, "short"},
		{"exactly10c", 10, "exactly10c"},
		{"this is a longer string", 10, "this is a…"},
	}
	for _, c := range cases {
		got := pickerTruncate(c.input, c.max)
		if got != c.want {
			t.Errorf("pickerTruncate(%q, %d) = %q, want %q", c.input, c.max, got, c.want)
		}
	}
}

func TestSessionPicker_ViewRendersRows(t *testing.T) {
	now := time.Now()
	manifests := []session.Manifest{
		{SessionID: "1", Title: "visible session", UpdatedAt: now},
	}
	picker := newSessionPickerModel(manifests, "")
	picker.visible = true

	view := picker.View()
	if view == "" {
		t.Error("View() returned empty string when visible")
	}
	if !containsStr(view, "visible session") {
		t.Error("View() should contain the session title")
	}
}

func TestSessionPicker_ViewEmptyWhenNotVisible(t *testing.T) {
	manifests := []session.Manifest{
		{SessionID: "1", Title: "hidden", UpdatedAt: time.Now()},
	}
	picker := newSessionPickerModel(manifests, "")
	// visible defaults to false

	view := picker.View()
	if view != "" {
		t.Errorf("View() should be empty when not visible, got %q", view)
	}
}

// containsStr is a helper to check if haystack contains needle (used in view tests).
func containsStr(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(haystack) > 0 && searchSubstring(haystack, needle))
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
