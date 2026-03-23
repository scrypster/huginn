package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ============================================================
// loaderModel.Update
// ============================================================

func TestLoaderModel_Update_WindowSize(t *testing.T) {
	m := newLoaderModel("/tmp")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	lm := updated.(loaderModel)
	if lm.width != 120 {
		t.Errorf("expected width=120, got %d", lm.width)
	}
}

func TestLoaderModel_Update_ProgressMsg(t *testing.T) {
	m := newLoaderModel("/tmp")
	updated, _ := m.Update(progressMsg{done: 5, total: 100, path: "/some/file.go"})
	lm := updated.(loaderModel)
	if lm.done != 5 {
		t.Errorf("expected done=5, got %d", lm.done)
	}
	if lm.total != 100 {
		t.Errorf("expected total=100, got %d", lm.total)
	}
	if lm.current != "/some/file.go" {
		t.Errorf("expected current='/some/file.go', got %q", lm.current)
	}
}

func TestLoaderModel_Update_IndexDoneMsg(t *testing.T) {
	m := newLoaderModel("/tmp")
	updated, cmd := m.Update(indexDoneMsg{idx: nil, err: nil})
	lm := updated.(loaderModel)
	if lm.result == nil {
		t.Error("expected result to be set after indexDoneMsg")
	}
	if cmd == nil {
		t.Error("expected tea.Quit cmd after indexDoneMsg")
	}
}

func TestLoaderModel_Update_IndexDoneMsg_WithError(t *testing.T) {
	m := newLoaderModel("/tmp")
	updated, _ := m.Update(indexDoneMsg{idx: nil, err: errIndexFailed})
	lm := updated.(loaderModel)
	if lm.result == nil {
		t.Error("expected result to be set even on error")
	}
	if lm.result.Err != errIndexFailed {
		t.Errorf("expected error to be stored, got %v", lm.result.Err)
	}
}

// errIndexFailed is a test sentinel error.
var errIndexFailed = &testError{"index failed"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

// ============================================================
// loaderModel.View
// ============================================================

func TestLoaderModel_View_ZeroWidth_Empty(t *testing.T) {
	m := newLoaderModel("/tmp")
	m.width = 0
	result := m.View()
	if result != "" {
		t.Errorf("expected empty View for zero width, got %q", result)
	}
}

func TestLoaderModel_View_WithWidth_NonEmpty(t *testing.T) {
	m := newLoaderModel("/tmp")
	m.width = 80
	result := m.View()
	if result == "" {
		t.Error("expected non-empty View with width=80")
	}
}

func TestLoaderModel_View_WithProgress_ShowsStats(t *testing.T) {
	m := newLoaderModel("/tmp")
	m.width = 80
	m.done = 42
	m.total = 100
	result := m.View()
	if result == "" {
		t.Error("expected non-empty View with progress")
	}
}

func TestLoaderModel_View_IndeterminateMode(t *testing.T) {
	m := newLoaderModel("/tmp")
	m.width = 80
	m.done = 10
	m.total = 0 // indeterminate: total unknown
	result := m.View()
	if result == "" {
		t.Error("expected non-empty View in indeterminate mode")
	}
}

func TestLoaderModel_View_WithCurrentFile(t *testing.T) {
	m := newLoaderModel("/tmp")
	m.width = 80
	m.current = "/tmp/some/file.go"
	result := m.View()
	if result == "" {
		t.Error("expected non-empty View with current file")
	}
}

func TestLoaderModel_View_CurrentFileAtRoot(t *testing.T) {
	m := newLoaderModel("/tmp")
	m.width = 80
	m.current = "file.go" // no directory component
	result := m.View()
	if result == "" {
		t.Error("expected non-empty View with root-level current file")
	}
}
