package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
)

// TestHandleWSMessage_Chat_PersistsUserAndAssistantMessages verifies that
// after a WS chat turn completes, both the user message and the assistant
// reply are persisted to the session store.
//
// Regression test: previously ChatWithAgent only stored messages in-memory
// (sess.appendHistory), so on page reload api.sessions.getMessages returned
// empty and all chat history was lost.
func TestHandleWSMessage_Chat_PersistsUserAndAssistantMessages(t *testing.T) {
	srv, _ := newTestServer(t)

	// Create and save a session manifest so the WS handler can load it.
	sess := srv.store.New("test-persist", "/workspace", "test-model")
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	client := &wsClient{send: make(chan WSMessage, 64), ctx: context.Background()}
	msg := WSMessage{
		Type:      "chat",
		SessionID: sess.ID,
		Content:   "hello from test",
	}
	srv.handleWSMessage(client, msg)

	// Drain until done/error or timeout.
	deadline := time.After(5 * time.Second)
	done := false
	for !done {
		select {
		case m := <-client.send:
			if m.Type == "done" || m.Type == "error" {
				done = true
			}
		case <-deadline:
			t.Log("timeout waiting for done/error")
			done = true
		}
	}
	// Allow the goroutine to finish persisting (brief yield).
	time.Sleep(50 * time.Millisecond)

	// Load messages from the store and verify persistence.
	msgs, err := srv.store.TailMessages(sess.ID, 10)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 persisted messages (user+assistant), got %d", len(msgs))
	}

	// Verify user message.
	userMsg := msgs[0]
	if userMsg.Role != "user" {
		t.Errorf("expected first message role 'user', got %q", userMsg.Role)
	}
	if userMsg.Content != "hello from test" {
		t.Errorf("expected user content 'hello from test', got %q", userMsg.Content)
	}

	// Verify assistant message.
	assistantMsg := msgs[1]
	if assistantMsg.Role != "assistant" {
		t.Errorf("expected second message role 'assistant', got %q", assistantMsg.Role)
	}
	if assistantMsg.Content == "" {
		t.Error("expected non-empty assistant content")
	}

	// Both messages must have IDs and timestamps.
	for i, m := range msgs[:2] {
		if m.ID == "" {
			t.Errorf("msgs[%d]: expected non-empty ID", i)
		}
		if m.Ts.IsZero() {
			t.Errorf("msgs[%d]: expected non-zero timestamp", i)
		}
	}
}

// TestHandleWSMessage_Chat_SpaceSession_PersistsToStore verifies that
// messages from a space-associated session (created with space_id) are
// also persisted, enabling ListSpaceMessages to return history after reload.
func TestHandleWSMessage_Chat_SpaceSession_PersistsToStore(t *testing.T) {
	srv, _ := newTestServer(t)

	// Create a session with space_id (simulating what handleCreateSession does after our fix).
	now := time.Now().UTC()
	spaceSess := &session.Session{
		ID: session.NewID(),
		Manifest: session.Manifest{
			ID:        session.NewID(),
			Status:    "active",
			Version:   1,
			SpaceID:   "test-space-123",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	// Fix the Manifest.ID/SessionID to match the session ID.
	spaceSess.Manifest.ID = spaceSess.ID
	spaceSess.Manifest.SessionID = spaceSess.ID
	if err := srv.store.SaveManifest(spaceSess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	client := &wsClient{send: make(chan WSMessage, 64), ctx: context.Background()}
	msg := WSMessage{
		Type:      "chat",
		SessionID: spaceSess.ID,
		Content:   "space message test",
	}
	srv.handleWSMessage(client, msg)

	deadline := time.After(5 * time.Second)
	done := false
	for !done {
		select {
		case m := <-client.send:
			if m.Type == "done" || m.Type == "error" {
				done = true
			}
		case <-deadline:
			done = true
		}
	}
	time.Sleep(50 * time.Millisecond)

	msgs, err := srv.store.TailMessages(spaceSess.ID, 10)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 persisted messages, got %d (space session messages not persisted)", len(msgs))
	}
}

// TestHandleSendMessage_PersistsUserAndAssistantMessages verifies that the
// REST endpoint POST /api/v1/sessions/{id}/messages also persists both the
// user message and assistant reply to the session store.
//
// Regression test: previously handleSendMessage called ChatForSession and
// returned the response, but never wrote messages to s.store — so
// GET /api/v1/sessions/{id}/messages always returned [] after a REST chat.
func TestHandleSendMessage_PersistsUserAndAssistantMessages(t *testing.T) {
	srv, ts := newTestServer(t)

	// Pre-create and persist a session manifest so the handler can load it.
	sess := srv.store.New("rest-persist", "/workspace", "test-model")
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	// Also register the session with the orchestrator so ChatForSession works.
	if _, err := srv.orch.NewSession(sess.ID); err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	body := `{"content":"hello via rest"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/sessions/"+sess.ID+"/messages", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /messages: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var respBody map[string]string
	json.NewDecoder(resp.Body).Decode(&respBody)
	if respBody["content"] == "" {
		t.Error("expected non-empty content in response")
	}

	// Verify both messages were persisted to the store.
	msgs, loadErr := srv.store.TailMessages(sess.ID, 10)
	if loadErr != nil {
		t.Fatalf("TailMessages: %v", loadErr)
	}
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 persisted messages (user+assistant), got %d — REST path not persisting", len(msgs))
	}

	if msgs[0].Role != "user" {
		t.Errorf("expected msgs[0].Role='user', got %q", msgs[0].Role)
	}
	if msgs[0].Content != "hello via rest" {
		t.Errorf("expected msgs[0].Content='hello via rest', got %q", msgs[0].Content)
	}
	if msgs[1].Role != "assistant" {
		t.Errorf("expected msgs[1].Role='assistant', got %q", msgs[1].Role)
	}
	if msgs[1].Content == "" {
		t.Error("expected non-empty assistant content in msgs[1]")
	}
}
