package tui

import (
	"sync/atomic"
	"testing"

	"github.com/scrypster/huginn/internal/notepad"
	"github.com/scrypster/huginn/internal/pricing"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/swarm"
)

// ============================================================
// Simple setter methods — exercising 0% functions
// ============================================================

func TestSetAutoRunAtom(t *testing.T) {
	a := newTestApp()
	atom := &atomic.Bool{}
	atom.Store(false)
	a.SetAutoRunAtom(atom)
	if a.autoRunAtom != atom {
		t.Error("expected autoRunAtom to be set")
	}
}

func TestSetSessionStore(t *testing.T) {
	a := newTestApp()
	store := session.NewStore(t.TempDir())
	a.SetSessionStore(store)
	if a.sessionStore != store {
		t.Error("expected sessionStore to be set")
	}
}

func TestSetActiveSession(t *testing.T) {
	a := newTestApp()
	sess := &session.Session{}
	a.SetActiveSession(sess)
	if a.activeSession != sess {
		t.Error("expected activeSession to be set")
	}
}

func TestSetNotepadManager(t *testing.T) {
	a := newTestApp()
	dir := t.TempDir()
	mgr := notepad.NewManager(dir, dir)
	a.SetNotepadManager(mgr)
	if a.notepadMgr != mgr {
		t.Error("expected notepadMgr to be set")
	}
}

func TestSetPriceTracker(t *testing.T) {
	a := newTestApp()
	tracker := pricing.NewSessionTracker(nil)
	a.SetPriceTracker(tracker)
	if a.priceTracker != tracker {
		t.Error("expected priceTracker to be set")
	}
}

// ============================================================
// Session management — nil guard paths
// ============================================================

func TestSaveSession_NilStore_ReturnsNil(t *testing.T) {
	a := newTestApp()
	a.sessionStore = nil
	cmd := a.saveSession()
	if cmd != nil {
		t.Error("expected nil cmd when sessionStore is nil")
	}
}

func TestSaveSession_NilSession_ReturnsNil(t *testing.T) {
	a := newTestApp()
	store := session.NewStore(t.TempDir())
	a.sessionStore = store
	a.activeSession = nil
	cmd := a.saveSession()
	if cmd != nil {
		t.Error("expected nil cmd when activeSession is nil")
	}
}

func TestRenameSession_NilSession_ReturnsNil(t *testing.T) {
	a := newTestApp()
	a.activeSession = nil
	cmd := a.renameSession("new title")
	if cmd != nil {
		t.Error("expected nil cmd when activeSession is nil")
	}
}

func TestRenameSession_EmptyTitle_ReturnsNil(t *testing.T) {
	a := newTestApp()
	a.activeSession = &session.Session{}
	cmd := a.renameSession("")
	if cmd != nil {
		t.Error("expected nil cmd when newTitle is empty")
	}
}

func TestRenameSession_UpdatesTitle(t *testing.T) {
	a := newTestApp()
	a.activeSession = &session.Session{}
	_ = a.renameSession("my new title")
	if a.activeSession.Manifest.Title != "my new title" {
		t.Errorf("expected title 'my new title', got %q", a.activeSession.Manifest.Title)
	}
}

func TestOpenSessionPicker_NilStore_AddsSystemLine(t *testing.T) {
	a := newTestApp()
	a.sessionStore = nil
	cmd := a.openSessionPicker()
	if cmd != nil {
		t.Error("expected nil cmd when sessionStore is nil")
	}
	if len(a.chat.history) == 0 {
		t.Error("expected system message in history when store is nil")
	}
}

func TestOpenSessionPicker_WithStore_TransitionsState(t *testing.T) {
	a := newTestApp()
	store := session.NewStore(t.TempDir())
	a.sessionStore = store
	a.state = stateChat
	_ = a.openSessionPicker()
	if a.state != stateSessionPicker {
		t.Errorf("expected stateSessionPicker, got %v", a.state)
	}
}

func TestResumeSession_NilStore_ReturnsNilMsg(t *testing.T) {
	a := newTestApp()
	a.sessionStore = nil
	cmd := a.resumeSession("some-id")
	if cmd == nil {
		t.Fatal("expected non-nil cmd from resumeSession")
	}
	// Execute — should return nil because store is nil.
	msg := cmd()
	if msg != nil {
		t.Errorf("expected nil msg when store is nil, got %v", msg)
	}
}

// ============================================================
// handleNotepadCmd — all sub-command branches
// ============================================================

func TestHandleNotepadCmd_NilManager_ShowsDisabled(t *testing.T) {
	a := newTestApp()
	a.notepadMgr = nil
	_ = a.handleNotepadCmd("/notepad list")
	if len(a.chat.history) == 0 {
		t.Fatal("expected system message when notepad manager is nil")
	}
	last := a.chat.history[len(a.chat.history)-1]
	if last.role != "system" {
		t.Errorf("expected system role, got %q", last.role)
	}
}

func TestHandleNotepadCmd_List_EmptyManager(t *testing.T) {
	a := newTestApp()
	dir := t.TempDir()
	mgr := notepad.NewManager(dir, dir)
	a.notepadMgr = mgr
	_ = a.handleNotepadCmd("/notepad list")
	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry after /notepad list")
	}
}

func TestHandleNotepadCmd_Show_MissingName(t *testing.T) {
	a := newTestApp()
	dir := t.TempDir()
	mgr := notepad.NewManager(dir, dir)
	a.notepadMgr = mgr
	_ = a.handleNotepadCmd("/notepad show")
	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry after /notepad show with no name")
	}
	last := a.chat.history[len(a.chat.history)-1]
	if last.role != "error" {
		t.Errorf("expected error role for missing name, got %q", last.role)
	}
}

func TestHandleNotepadCmd_Delete_MissingName(t *testing.T) {
	a := newTestApp()
	dir := t.TempDir()
	mgr := notepad.NewManager(dir, dir)
	a.notepadMgr = mgr
	_ = a.handleNotepadCmd("/notepad delete")
	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry after /notepad delete with no name")
	}
	last := a.chat.history[len(a.chat.history)-1]
	if last.role != "error" {
		t.Errorf("expected error role for missing name, got %q", last.role)
	}
}

func TestHandleNotepadCmd_UnknownSub_ShowsUsage(t *testing.T) {
	a := newTestApp()
	dir := t.TempDir()
	mgr := notepad.NewManager(dir, dir)
	a.notepadMgr = mgr
	_ = a.handleNotepadCmd("/notepad unknowncmd")
	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry for unknown sub-command")
	}
}

// ============================================================
// SwarmViewModel — uncovered methods
// ============================================================

func TestSwarmViewModel_FocusedID_Empty(t *testing.T) {
	sv := NewSwarmViewModel(80, 24)
	id := sv.FocusedID()
	if id != "" {
		t.Errorf("expected empty FocusedID when no focus, got %q", id)
	}
}

func TestSwarmViewModel_FocusedID_AfterSetFocus(t *testing.T) {
	sv := NewSwarmViewModel(80, 24)
	sv.AddAgent("agent1", "Agent One", "#ff0000")
	sv.SetFocus("agent1")
	id := sv.FocusedID()
	if id != "agent1" {
		t.Errorf("expected FocusedID='agent1', got %q", id)
	}
}

func TestSwarmViewModel_CountRunning_NoAgents(t *testing.T) {
	sv := NewSwarmViewModel(80, 24)
	if n := sv.CountRunning(); n != 0 {
		t.Errorf("expected 0 running, got %d", n)
	}
}

func TestSwarmViewModel_CountRunning_WithRunningAgents(t *testing.T) {
	sv := NewSwarmViewModel(80, 24)
	sv.AddAgent("a1", "Alpha", "#ff0000")
	sv.AddAgent("a2", "Beta", "#00ff00")
	sv.AddAgent("a3", "Gamma", "#0000ff")
	sv.SetStatus("a1", swarm.StatusThinking)
	sv.SetStatus("a2", swarm.StatusTooling)
	sv.SetStatus("a3", swarm.StatusDone)
	n := sv.CountRunning()
	if n != 2 {
		t.Errorf("expected 2 running agents, got %d", n)
	}
}

func TestSwarmViewModel_StatusLabel_AllStatuses(t *testing.T) {
	sv := NewSwarmViewModel(80, 24)
	sv.AddAgent("a1", "Alpha", "#ff0000")

	statuses := []swarm.AgentStatus{
		swarm.StatusQueued,
		swarm.StatusThinking,
		swarm.StatusTooling,
		swarm.StatusDone,
		swarm.StatusError,
		swarm.StatusCancelled,
	}

	for _, status := range statuses {
		sv.SetStatus("a1", status)
		for _, av := range sv.agents {
			label := sv.statusLabel(av)
			if label == "" {
				t.Errorf("expected non-empty statusLabel for status %v", status)
			}
		}
	}
}

func TestSwarmViewModel_StatusLabel_ToolingWithToolName(t *testing.T) {
	sv := NewSwarmViewModel(80, 24)
	sv.AddAgent("a1", "Alpha", "#ff0000")
	sv.SetStatus("a1", swarm.StatusTooling)
	sv.SetToolName("a1", "bash_exec")
	for _, av := range sv.agents {
		label := sv.statusLabel(av)
		if label == "" {
			t.Errorf("expected non-empty statusLabel for tooling with name")
		}
	}
}

// ============================================================
// readSwarmEvent — nil channel path
// ============================================================

func TestReadSwarmEvent_NilChannel_ReturnsNil(t *testing.T) {
	cmd := readSwarmEvent(nil)
	if cmd != nil {
		t.Error("expected nil cmd for nil swarm event channel")
	}
}

// ============================================================
// agentwizard Init methods (standalone wizard)
// ============================================================

func TestStandaloneAgentWizard_Init(t *testing.T) {
	m := NewStandaloneAgentWizard()
	cmd := m.Init()
	// Init just returns textinput.Blink — should not panic.
	_ = cmd
}

func TestAgentWizardModel_Init(t *testing.T) {
	m := newAgentWizardModel()
	cmd := m.Init()
	// Init just returns textinput.Blink — should not panic.
	_ = cmd
}
