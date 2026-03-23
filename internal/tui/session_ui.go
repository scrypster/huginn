package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/scrypster/huginn/internal/session"
)

// sessionResumedMsg is sent when a session has been loaded from disk and is ready
// to be restored into the TUI.
type sessionResumedMsg struct {
	sess     *session.Session
	messages []session.SessionMessage
}

// resumeSession loads a session and its messages from disk, returning a tea.Cmd
// that delivers a sessionResumedMsg for handling in Update().
// It also hydrates the orchestrator's in-memory history so the LLM has context
// from the resumed session.
func (a *App) resumeSession(id string) tea.Cmd {
	return func() tea.Msg {
		if a.sessionStore == nil {
			return nil
		}
		sess, err := a.sessionStore.Load(id)
		if err != nil {
			return nil
		}
		msgs, err := a.sessionStore.TailMessages(id, 100)
		if err != nil {
			return nil
		}
		// Hydrate the orchestrator's in-memory history so the LLM has context
		// from the resumed session. Failures are non-fatal — the display history
		// is still rebuilt from msgs below.
		if a.orch != nil {
			_ = a.orch.HydrateSession(context.Background(), id)
		}
		return sessionResumedMsg{sess: sess, messages: msgs}
	}
}

// saveSession persists the active session manifest to disk (best-effort, in a goroutine).
func (a *App) saveSession() tea.Cmd {
	if a.sessionStore == nil || a.activeSession == nil {
		return nil
	}
	go func() {
		_ = a.sessionStore.SaveManifest(a.activeSession)
	}()
	return nil
}

// renameSession updates the active session title and persists it.
func (a *App) renameSession(newTitle string) tea.Cmd {
	if a.activeSession == nil || newTitle == "" {
		return nil
	}
	a.activeSession.Manifest.Title = newTitle
	if a.sessionStore != nil {
		go func() {
			_ = a.sessionStore.SaveManifest(a.activeSession)
		}()
	}
	return nil
}

// openSessionPicker opens the session picker overlay, loading sessions from the store.
func (a *App) openSessionPicker() tea.Cmd {
	if a.sessionStore == nil {
		a.addLine("system", "No session store configured")
		a.refreshViewport()
		return nil
	}
	manifests, err := a.sessionStore.List()
	if err != nil {
		a.addLine("system", fmt.Sprintf("list sessions: %v", err))
		a.refreshViewport()
		return nil
	}
	a.sessionPicker = newSessionPickerModel(manifests, a.workspaceRoot)
	a.sessionPicker.visible = true
	a.sessionPicker.width = a.width
	a.sessionPicker.height = a.height
	a.state = stateSessionPicker
	a.recalcViewportHeight()
	return nil
}
