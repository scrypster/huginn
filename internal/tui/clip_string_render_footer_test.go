package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/storage"
)

// --- clipString ---

func TestClipString_NoTruncation(t *testing.T) {
	got := clipString("hello", 10)
	if got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestClipString_Truncates(t *testing.T) {
	got := clipString("hello world", 7)
	// clipString truncates to exactly maxRunes without ellipsis
	runes := []rune(got)
	if len(runes) != 7 {
		t.Errorf("expected 7 runes, got %d in %q", len(runes), got)
	}
	if got != "hello w" {
		t.Errorf("expected 'hello w', got %q", got)
	}
}

func TestClipString_Exact(t *testing.T) {
	got := clipString("abcde", 5)
	if got != "abcde" {
		t.Errorf("exact length: expected 'abcde', got %q", got)
	}
}

func TestClipString_Empty(t *testing.T) {
	got := clipString("", 10)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// --- helpText ---

func TestHelpText_ContainsKeyBindings_Iter3(t *testing.T) {
	h := helpText()
	for _, key := range []string{"ctrl+c", "ctrl+o", "/reason"} {
		if !strings.Contains(h, key) {
			t.Errorf("helpText missing %q", key)
		}
	}
}

// --- fmtToolCallPreview (additional cases not in helpers_test.go) ---

func TestFmtToolCallPreview_EditFile_Iter3(t *testing.T) {
	got := fmtToolCallPreview("edit_file", map[string]any{"file_path": "/path/to/file.go"})
	if !strings.Contains(got, "edit_file") {
		t.Errorf("expected 'edit_file' in result, got %q", got)
	}
}

func TestFmtToolCallPreview_SearchFiles_Iter3(t *testing.T) {
	got := fmtToolCallPreview("search_files", map[string]any{"pattern": "*.go"})
	if !strings.Contains(got, "search_files") {
		t.Errorf("expected 'search_files' in result, got %q", got)
	}
}

func TestFmtToolCallPreview_Grep_Iter3(t *testing.T) {
	got := fmtToolCallPreview("grep", map[string]any{"pattern": "TODO"})
	if !strings.Contains(got, "grep") {
		t.Errorf("expected 'grep' in result, got %q", got)
	}
}

func TestFmtToolCallPreview_BashLong_Iter3(t *testing.T) {
	longCmd := strings.Repeat("x", 100)
	got := fmtToolCallPreview("bash", map[string]any{"command": longCmd})
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected truncation for long bash command, got %q", got)
	}
}

// --- formatDuration ---

func TestFormatDuration_Milliseconds(t *testing.T) {
	got := formatDuration(450 * time.Millisecond)
	if !strings.Contains(got, "ms") {
		t.Errorf("expected ms suffix for <1s, got %q", got)
	}
}

func TestFormatDuration_Seconds(t *testing.T) {
	got := formatDuration(1500 * time.Millisecond)
	if !strings.Contains(got, "s") {
		t.Errorf("expected s suffix for >=1s, got %q", got)
	}
	if !strings.Contains(got, "1.5") {
		t.Errorf("expected 1.5s, got %q", got)
	}
}

// --- convertStorageEdgesToSymbolEdges ---

func TestConvertStorageEdgesToSymbolEdges_Iter3(t *testing.T) {
	in := []storage.Edge{
		{From: "a.go", To: "b.go", Symbol: "Foo", Confidence: "HIGH", Kind: "Import"},
		{From: "b.go", To: "c.go", Symbol: "Bar", Confidence: "LOW", Kind: "Call"},
	}
	out := convertStorageEdgesToSymbolEdges(in)
	if len(out) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(out))
	}
	if out[0].From != "a.go" {
		t.Errorf("expected From=a.go, got %q", out[0].From)
	}
	if out[1].Symbol != "Bar" {
		t.Errorf("expected Symbol=Bar, got %q", out[1].Symbol)
	}
}

func TestConvertStorageEdgesToSymbolEdges_Empty(t *testing.T) {
	out := convertStorageEdgesToSymbolEdges(nil)
	if len(out) != 0 {
		t.Errorf("expected empty, got %d", len(out))
	}
}

// --- handleSlashCommand (cases not already covered in hardening_round4_test.go) ---

func TestHandleSlashCommand_Help_Iter3(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "help"})
	found := false
	for _, l := range a.chat.history {
		if strings.Contains(l.content, "ctrl+c") {
			found = true
		}
	}
	if !found {
		t.Error("expected help text in history after /help")
	}
}

func TestHandleSlashCommand_Stats_NilRegistry(t *testing.T) {
	a := newMinimalApp()
	a.statsReg = nil
	a.handleSlashCommand(SlashCommand{Name: "stats"})
	found := false
	for _, l := range a.chat.history {
		if strings.Contains(l.content, "Stats") {
			found = true
		}
	}
	if !found {
		t.Error("expected Stats in history after /stats with nil registry")
	}
}

func TestHandleSlashCommand_Workspace_WithRoot(t *testing.T) {
	a := newMinimalApp()
	a.workspaceRoot = "/my/project"
	a.handleSlashCommand(SlashCommand{Name: "workspace"})
	found := false
	for _, l := range a.chat.history {
		if strings.Contains(l.content, "/my/project") {
			found = true
		}
	}
	if !found {
		t.Error("expected workspace root in history after /workspace")
	}
}

func TestHandleSlashCommand_Workspace_NoRoot(t *testing.T) {
	a := newMinimalApp()
	a.workspaceRoot = ""
	a.handleSlashCommand(SlashCommand{Name: "workspace"})
	found := false
	for _, l := range a.chat.history {
		if strings.Contains(l.content, "not set") {
			found = true
		}
	}
	if !found {
		t.Error("expected '(not set)' for empty workspace root")
	}
}

func TestHandleSlashCommand_Radar_NilStore(t *testing.T) {
	a := newMinimalApp()
	a.store = nil
	a.handleSlashCommand(SlashCommand{Name: "radar"})
	found := false
	for _, l := range a.chat.history {
		if strings.Contains(l.content, "unavailable") {
			found = true
		}
	}
	if !found {
		t.Error("expected 'unavailable' when store is nil")
	}
}

func TestHandleSlashCommand_Radar_NoWorkspace(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer s.Close()

	a := newMinimalApp()
	a.store = s
	a.workspaceRoot = ""
	a.handleSlashCommand(SlashCommand{Name: "radar"})
	found := false
	for _, l := range a.chat.history {
		if strings.Contains(l.content, "workspace") || strings.Contains(l.content, "Radar") {
			found = true
		}
	}
	if !found {
		t.Error("expected workspace message for /radar without workspace")
	}
}

func TestHandleSlashCommand_Impact_NoSymbol(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "impact", Args: ""})
	found := false
	for _, l := range a.chat.history {
		if strings.Contains(l.content, "Usage") {
			found = true
		}
	}
	if !found {
		t.Error("expected Usage hint for /impact without symbol")
	}
}

func TestHandleSlashCommand_Impact_NilStore(t *testing.T) {
	a := newMinimalApp()
	a.store = nil
	a.handleSlashCommand(SlashCommand{Name: "impact", Args: "MyFunc"})
	found := false
	for _, l := range a.chat.history {
		if strings.Contains(l.content, "unavailable") {
			found = true
		}
	}
	if !found {
		t.Error("expected 'unavailable' message for /impact with nil store")
	}
}

func TestHandleSlashCommand_Impact_WithStore(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer s.Close()

	a := newMinimalApp()
	a.store = s
	a.handleSlashCommand(SlashCommand{Name: "impact", Args: "NonExistentSymbol"})
	found := false
	for _, l := range a.chat.history {
		if strings.Contains(l.content, "No references") || strings.Contains(l.content, "NonExistentSymbol") {
			found = true
		}
	}
	if !found {
		t.Error("expected 'No references' for unknown symbol")
	}
}

func TestHandleSlashCommand_Swarm_NilEvents(t *testing.T) {
	a := newMinimalApp()
	a.swarmEvents = nil
	a.state = stateChat
	a.handleSlashCommand(SlashCommand{Name: "swarm"})
	if a.state == stateSwarm {
		t.Error("expected state to remain chat when swarmEvents is nil")
	}
}

func TestHandleSlashCommand_Agents_ShowRoster(t *testing.T) {
	a := newMinimalApp()
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Alice", ModelID: "m"})
	a.agentReg = reg
	a.handleSlashCommand(SlashCommand{Name: "agents", Args: ""})
	found := false
	for _, l := range a.chat.history {
		if strings.Contains(l.content, "Alice") {
			found = true
		}
	}
	if !found {
		t.Error("expected Alice in roster output")
	}
}

// --- addLine / history ---

func TestAddLine_MultipleLines(t *testing.T) {
	a := newMinimalApp()
	a.addLine("user", "hello")
	a.addLine("assistant", "world")
	if len(a.chat.history) != 2 {
		t.Fatalf("expected 2 history lines, got %d", len(a.chat.history))
	}
	if a.chat.history[0].role != "user" {
		t.Errorf("expected role user, got %q", a.chat.history[0].role)
	}
	if a.chat.history[1].content != "world" {
		t.Errorf("expected content world, got %q", a.chat.history[1].content)
	}
}

// --- parseModelCommandIfAny ---

func TestParseModelCommandIfAny_Invalid(t *testing.T) {
	_, err := parseModelCommandIfAny("hello world")
	if err == nil {
		t.Error("expected error for non-model command")
	}
}
