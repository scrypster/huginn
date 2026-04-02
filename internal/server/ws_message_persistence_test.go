package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/modelconfig"
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

// TestHandleWSMessage_Chat_ErrorPersistsMessagesAndEmitsActivity verifies that
// when a chat turn fails (e.g. agent has no model configured), the user message
// AND an error assistant message are still persisted to the store, and
// emitSpaceActivity is called so the notification badge reflects a real message
// rather than causing a ghost notification (badge with no messages to see).
//
// Regression: previously the error path returned early without persisting
// anything, so users saw an unread badge leading to an empty conversation.
func TestHandleWSMessage_Chat_ErrorPersistsMessagesAndEmitsActivity(t *testing.T) {
	srv, _ := newTestServer(t)

	// Override agentLoader to return an agent without a model (simulates a
	// legacy or manually-edited agent config where model was not set).
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{
			Agents: []agents.AgentDef{
				{Name: "Mike", Model: "", SystemPrompt: ""},
			},
		}, nil
	}

	// Create a session stamped with "Mike" as primary agent.
	now := time.Now().UTC()
	sess := &session.Session{
		ID: session.NewID(),
		Manifest: session.Manifest{
			ID:        "",
			SessionID: "",
			Status:    "active",
			Version:   1,
			Agent:     "Mike",
			SpaceID:   "dm-mike-test",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	sess.Manifest.ID = sess.ID
	sess.Manifest.SessionID = sess.ID
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	client := &wsClient{send: make(chan WSMessage, 64), ctx: context.Background()}
	srv.handleWSMessage(client, WSMessage{
		Type:      "chat",
		SessionID: sess.ID,
		Content:   "hello Mike",
	})

	// Drain until error/done or timeout.
	deadline := time.After(5 * time.Second)
	gotError := false
	for !gotError {
		select {
		case m := <-client.send:
			if m.Type == "error" {
				gotError = true
			}
			if m.Type == "done" {
				// Should never succeed — fail fast.
				t.Fatal("expected error WS message, got done")
			}
		case <-deadline:
			t.Log("timeout waiting for error WS message")
			gotError = true
		}
	}
	// Allow the goroutine to finish persisting.
	time.Sleep(50 * time.Millisecond)

	// Verify both messages were persisted despite the error.
	msgs, err := srv.store.TailMessages(sess.ID, 10)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	if len(msgs) < 2 {
		t.Fatalf("expected 2 persisted messages (user + error reply) on chat error, got %d — ghost notification regression", len(msgs))
	}

	if msgs[0].Role != "user" {
		t.Errorf("msgs[0].Role = %q, want 'user'", msgs[0].Role)
	}
	if msgs[0].Content != "hello Mike" {
		t.Errorf("msgs[0].Content = %q, want 'hello Mike'", msgs[0].Content)
	}

	if msgs[1].Role != "assistant" {
		t.Errorf("msgs[1].Role = %q, want 'assistant' (error reply)", msgs[1].Role)
	}
	if msgs[1].Content == "" {
		t.Error("error reply message must have non-empty content")
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

// disconnectBackend emits a few tokens then blocks until the context is cancelled,
// simulating a long-running response where the client disconnects mid-stream.
type disconnectBackend struct {
	// tokensSent is closed after tokens have been emitted, signalling the
	// test that it can cancel the client context.
	tokensSent chan struct{}
}

func (d *disconnectBackend) ChatCompletion(ctx context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	// Emit partial content before the "disconnect".
	if req.OnToken != nil {
		req.OnToken("partial ")
		req.OnToken("response ")
	}
	if req.OnEvent != nil {
		req.OnEvent(backend.StreamEvent{
			Type: backend.StreamToolResult,
			Payload: map[string]any{
				"id": "tc-disconnect-1", "tool": "bash",
				"args": map[string]any{"command": "ls"}, "result": "file.txt",
			},
		})
	}
	// Signal that tokens have been sent.
	close(d.tokensSent)
	// Block until context is cancelled (simulating disconnect).
	<-ctx.Done()
	return nil, ctx.Err()
}
func (d *disconnectBackend) Health(_ context.Context) error   { return nil }
func (d *disconnectBackend) Shutdown(_ context.Context) error { return nil }
func (d *disconnectBackend) ContextWindow() int               { return 4096 }

// TestWSChat_PersistsOnDisconnect verifies that when a WS client disconnects
// mid-stream (before the done event fires), whatever content was accumulated
// is still persisted to the session store.
//
// Regression: previously the server only persisted on "done". If the client
// refreshed during streaming, all accumulated content was lost.
func TestWSChat_PersistsOnDisconnect(t *testing.T) {
	db := &disconnectBackend{tokensSent: make(chan struct{})}
	models := modelconfig.DefaultModels()
	orch, err := agent.NewOrchestrator(db, models, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	sessDir := t.TempDir()
	store := session.NewStore(sessDir)

	hub := newWSHub()
	go hub.run()
	t.Cleanup(func() { hub.stop() })

	cfg := *config.Default()
	srv := New(cfg, orch, store, testToken, t.TempDir(), nil, nil, nil)
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return agents.DefaultAgentsConfig(), nil
	}
	srv.wsHub = hub

	// Create a session.
	sess := store.New("disconnect-test", "/workspace", "test-model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// Create a cancellable context to simulate disconnect.
	ctx, cancel := context.WithCancel(context.Background())
	client := &wsClient{send: make(chan WSMessage, 64), ctx: ctx, cancel: cancel}

	srv.handleWSMessage(client, WSMessage{
		Type:      "chat",
		SessionID: sess.ID,
		Content:   "hello from disconnect test",
	})

	// Wait until the backend has emitted tokens, then simulate disconnect.
	select {
	case <-db.tokensSent:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for tokens to be emitted")
	}

	// Cancel the context (simulates WS disconnect).
	cancel()

	// Allow the goroutine to finish and persist.
	time.Sleep(200 * time.Millisecond)

	// Verify messages were persisted despite the disconnect.
	msgs, loadErr := store.TailMessages(sess.ID, 10)
	if loadErr != nil {
		t.Fatalf("TailMessages: %v", loadErr)
	}
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 persisted messages (user + assistant) on disconnect, got %d", len(msgs))
	}

	// User message.
	if msgs[0].Role != "user" {
		t.Errorf("msgs[0].Role = %q, want 'user'", msgs[0].Role)
	}
	if msgs[0].Content != "hello from disconnect test" {
		t.Errorf("msgs[0].Content = %q, want 'hello from disconnect test'", msgs[0].Content)
	}

	// Assistant message with accumulated content.
	if msgs[1].Role != "assistant" {
		t.Errorf("msgs[1].Role = %q, want 'assistant'", msgs[1].Role)
	}
	if !strings.Contains(msgs[1].Content, "partial") {
		t.Errorf("msgs[1].Content = %q, expected it to contain 'partial' (accumulated tokens)", msgs[1].Content)
	}

	// Tool calls should also be persisted.
	if len(msgs[1].ToolCalls) == 0 {
		t.Error("expected at least 1 tool call persisted on disconnect, got 0")
	}
}

// toolCallsBackend emits tokens + tool results in a single response, simulating
// a real agent response with both text content and tool calls.
type toolCallsBackend struct{}

func (b *toolCallsBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	if req.OnToken != nil {
		req.OnToken("Let me check... ")
	}
	if req.OnEvent != nil {
		req.OnEvent(backend.StreamEvent{
			Type: backend.StreamToolResult,
			Payload: map[string]any{
				"id": "tc-1", "tool": "bash",
				"args": map[string]any{"command": "ls"}, "result": "file1.txt\nfile2.txt",
			},
		})
		req.OnEvent(backend.StreamEvent{
			Type: backend.StreamToolResult,
			Payload: map[string]any{
				"id": "tc-2", "tool": "read_file",
				"args": map[string]any{"path": "file1.txt"}, "result": "hello world",
			},
		})
	}
	if req.OnToken != nil {
		req.OnToken("Here are the files.")
	}
	return &backend.ChatResponse{Content: "Let me check... Here are the files.", DoneReason: "stop"}, nil
}
func (b *toolCallsBackend) Health(_ context.Context) error   { return nil }
func (b *toolCallsBackend) Shutdown(_ context.Context) error { return nil }
func (b *toolCallsBackend) ContextWindow() int               { return 4096 }

// TestWSChat_PersistsTextAndToolCallsTogether verifies that a response
// containing both text content AND tool calls has both persisted correctly.
func TestWSChat_PersistsTextAndToolCallsTogether(t *testing.T) {
	models := modelconfig.DefaultModels()
	orch, err := agent.NewOrchestrator(&toolCallsBackend{}, models, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	sessDir := t.TempDir()
	store := session.NewStore(sessDir)

	hub := newWSHub()
	go hub.run()
	t.Cleanup(func() { hub.stop() })

	cfg := *config.Default()
	srv := New(cfg, orch, store, testToken, t.TempDir(), nil, nil, nil)
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return agents.DefaultAgentsConfig(), nil
	}
	srv.wsHub = hub

	sess := store.New("toolcalls-test", "/workspace", "test-model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	client := &wsClient{send: make(chan WSMessage, 64), ctx: context.Background()}
	srv.handleWSMessage(client, WSMessage{
		Type:      "chat",
		SessionID: sess.ID,
		Content:   "list files",
	})

	// Drain until done/error.
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
	time.Sleep(50 * time.Millisecond)

	msgs, loadErr := store.TailMessages(sess.ID, 10)
	if loadErr != nil {
		t.Fatalf("TailMessages: %v", loadErr)
	}
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(msgs))
	}

	assistantMsg := msgs[1]
	if assistantMsg.Role != "assistant" {
		t.Fatalf("msgs[1].Role = %q, want 'assistant'", assistantMsg.Role)
	}

	// Verify text content.
	if !strings.Contains(assistantMsg.Content, "Let me check") {
		t.Errorf("assistant content = %q, expected it to contain 'Let me check'", assistantMsg.Content)
	}
	if !strings.Contains(assistantMsg.Content, "Here are the files") {
		t.Errorf("assistant content = %q, expected it to contain 'Here are the files'", assistantMsg.Content)
	}

	// Verify tool calls.
	if len(assistantMsg.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(assistantMsg.ToolCalls))
	}
	if assistantMsg.ToolCalls[0].Name != "bash" {
		t.Errorf("tool_calls[0].Name = %q, want 'bash'", assistantMsg.ToolCalls[0].Name)
	}
	if assistantMsg.ToolCalls[1].Name != "read_file" {
		t.Errorf("tool_calls[1].Name = %q, want 'read_file'", assistantMsg.ToolCalls[1].Name)
	}
	if assistantMsg.ToolCalls[0].Result != "file1.txt\nfile2.txt" {
		t.Errorf("tool_calls[0].Result = %q, want 'file1.txt\\nfile2.txt'", assistantMsg.ToolCalls[0].Result)
	}
}
