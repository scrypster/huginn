package agent

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
)

// TestMultiSession_IndependentHistory verifies that two sessions maintain
// independent conversation histories that do not cross-contaminate.
func TestMultiSession_IndependentHistory(t *testing.T) {
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: "reply-session1", DoneReason: "stop"},
			{Content: "reply-session2", DoneReason: "stop"},
		},
	}
	models := &modelconfig.Models{
		Reasoner: "test-model",
	}
	o := mustNewOrchestrator(t, mb, models, nil, nil, nil, nil)

	// Default session: send a message via Chat
	if err := o.Chat(context.Background(), "hello from session1", nil, nil); err != nil {
		t.Fatalf("Chat on default session: %v", err)
	}

	// Create a second session
	s2, err := o.NewSession("")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if s2.ID == "" {
		t.Fatal("expected non-empty session ID")
	}

	// Directly populate second session's history
	o.mu.Lock()
	s2.history = append(s2.history,
		backend.Message{Role: "user", Content: "hello from session2"},
		backend.Message{Role: "assistant", Content: "reply-session2"},
	)
	o.mu.Unlock()

	// Verify default session history contains session1 messages
	o.mu.Lock()
	defaultHist := o.defaultSession().history
	o.mu.Unlock()

	foundS1 := false
	foundS2 := false
	for _, msg := range defaultHist {
		if msg.Content == "hello from session1" {
			foundS1 = true
		}
		if msg.Content == "hello from session2" {
			foundS2 = true
		}
	}
	if !foundS1 {
		t.Error("expected default session to contain session1 message")
	}
	if foundS2 {
		t.Error("default session should NOT contain session2 message")
	}

	// Verify second session history contains only session2 messages
	s2Got, ok := o.GetSession(s2.ID)
	if !ok {
		t.Fatal("GetSession returned false for s2")
	}
	foundS1InS2 := false
	foundS2InS2 := false
	for _, msg := range s2Got.history {
		if msg.Content == "hello from session1" {
			foundS1InS2 = true
		}
		if msg.Content == "hello from session2" {
			foundS2InS2 = true
		}
	}
	if foundS1InS2 {
		t.Error("session2 should NOT contain session1 message")
	}
	if !foundS2InS2 {
		t.Error("expected session2 to contain session2 message")
	}
}

// TestMultiSession_DefaultSessionCompatibility verifies that the existing
// single-session API (Plan, Chat, SessionID) works identically after the
// multi-session refactor.
func TestMultiSession_DefaultSessionCompatibility(t *testing.T) {
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: "planned output", DoneReason: "stop"},
		},
	}
	models := &modelconfig.Models{
		Reasoner: "test-model",
	}
	o := mustNewOrchestrator(t, mb, models, nil, nil, nil, nil)

	// SessionID should be non-empty and consistent
	sid := o.SessionID()
	if sid == "" {
		t.Error("expected non-empty SessionID")
	}
	if o.SessionID() != sid {
		t.Error("SessionID should return the same value on repeated calls")
	}

	// Chat should work via existing API
	err := o.Chat(context.Background(), "build a feature", nil, nil)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if o.CurrentState() != StateIdle {
		t.Errorf("expected StateIdle after chat, got %d", o.CurrentState())
	}

	// SessionID should still be the same
	if o.SessionID() != sid {
		t.Error("SessionID changed after Chat call")
	}
}

// TestNewSession_ReturnsDifferentID verifies that NewSession creates sessions
// with unique IDs, distinct from each other and from the default session.
func TestNewSession_ReturnsDifferentID(t *testing.T) {
	mb := newMockBackend("")
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)

	s1 := mustNewSession(t, o, "")
	s2 := mustNewSession(t, o, "")

	if s1.ID == s2.ID {
		t.Errorf("expected different IDs, both got %q", s1.ID)
	}
	if s1.ID == o.SessionID() {
		t.Errorf("s1.ID should differ from default session ID")
	}
	if s2.ID == o.SessionID() {
		t.Errorf("s2.ID should differ from default session ID")
	}

	// Both should be retrievable
	if _, ok := o.GetSession(s1.ID); !ok {
		t.Error("GetSession should find s1")
	}
	if _, ok := o.GetSession(s2.ID); !ok {
		t.Error("GetSession should find s2")
	}
}

// TestNewSession_WithExplicitID verifies that NewSession accepts an explicit ID.
func TestNewSession_WithExplicitID(t *testing.T) {
	mb := newMockBackend("")
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)

	s := mustNewSession(t, o, "custom-id-123")
	if s.ID != "custom-id-123" {
		t.Errorf("expected ID 'custom-id-123', got %q", s.ID)
	}

	got, ok := o.GetSession("custom-id-123")
	if !ok {
		t.Fatal("GetSession returned false for explicit ID")
	}
	if got.state != StateIdle {
		t.Errorf("expected StateIdle, got %d", got.state)
	}
}

// TestGetSession_NotFound verifies that GetSession returns false for unknown IDs.
func TestGetSession_NotFound(t *testing.T) {
	mb := newMockBackend("")
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)

	_, ok := o.GetSession("nonexistent")
	if ok {
		t.Error("expected GetSession to return false for unknown ID")
	}
}

// TestSession_NewSessionInitialState verifies that a new session starts in idle state
// with empty history.
func TestSession_NewSessionInitialState(t *testing.T) {
	sess := newSession("test-id")
	if sess.ID != "test-id" {
		t.Errorf("expected ID 'test-id', got %q", sess.ID)
	}
	if sess.state != StateIdle {
		t.Errorf("expected StateIdle, got %d", sess.state)
	}
	if len(sess.history) != 0 {
		t.Errorf("expected empty history, got %d entries", len(sess.history))
	}
}
