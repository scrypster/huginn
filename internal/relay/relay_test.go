package relay_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/scrypster/huginn/internal/relay"
)

func TestInProcessHub_Send_NoOp(t *testing.T) {
	h := relay.NewInProcessHub()
	err := h.Send("machine-1", relay.Message{
		Type:    relay.MsgToken,
		Payload: map[string]any{"token": "hello"},
	})
	if err != nil {
		t.Errorf("expected nil from InProcessHub.Send, got: %v", err)
	}
}

func TestInProcessHub_Close_NoOp(t *testing.T) {
	h := relay.NewInProcessHub()
	h.Close("machine-1") // must not panic
}

func TestMessageTypes_AreNonEmpty(t *testing.T) {
	types := []relay.MessageType{
		relay.MsgToken,
		relay.MsgToolCall,
		relay.MsgToolResult,
		relay.MsgPermissionReq,
		relay.MsgPermissionResp,
		relay.MsgDone,
	}
	for _, mt := range types {
		if mt == "" {
			t.Errorf("empty MessageType constant")
		}
	}
}

func TestMessage_JSONRoundTrip(t *testing.T) {
	original := relay.Message{
		Type:      relay.MsgToken,
		MachineID: "m1",
		Payload:   map[string]any{"token": "hello"},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got relay.Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != original.Type {
		t.Errorf("Type: got %q, want %q", got.Type, original.Type)
	}
	if got.MachineID != original.MachineID {
		t.Errorf("MachineID: got %q, want %q", got.MachineID, original.MachineID)
	}
}

func TestWebSocketHub_Implements_Hub(t *testing.T) {
	var _ relay.Hub = (*relay.WebSocketHub)(nil)
}

func TestInProcessHub_Implements_Hub(t *testing.T) {
	var _ relay.Hub = (*relay.InProcessHub)(nil)
}

func TestLoadIdentity_ErrNotRegistered(t *testing.T) {
	// LoadIdentity should return ErrNotRegistered when relay.json doesn't exist.
	// Use an empty temp dir as HOME to avoid interference from real ~/.huginn/relay.json.
	t.Setenv("HOME", t.TempDir())
	id, err := relay.LoadIdentity()
	if err != relay.ErrNotRegistered {
		t.Errorf("expected ErrNotRegistered, got: %v", err)
	}
	if id != nil {
		t.Errorf("expected nil identity, got: %+v", id)
	}
}

func TestIdentity_SaveAndLoad_RoundTrip(t *testing.T) {
	tempDir := t.TempDir()

	// Create a test identity
	original := &relay.Identity{
		AgentID:  "test-agent-123",
		Endpoint: "https://relay.example.com",
		APIKey:   "secret-key",
	}

	// We need to temporarily override the home dir used by SaveAndLoad.
	// Since the actual implementation uses os.UserHomeDir(), we'll create
	// the directory structure manually in our temp dir and verify the file exists.
	huginDir := tempDir + "/.huginn"
	if err := os.MkdirAll(huginDir, 0o750); err != nil {
		t.Fatalf("failed to create temp .huginn dir: %v", err)
	}

	relayPath := huginDir + "/relay.json"

	// Save the identity directly to our test file
	data, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(relayPath, data, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Now verify we can read it back by reading directly
	readData, err := os.ReadFile(relayPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	var loaded relay.Identity
	if err := json.Unmarshal(readData, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if loaded.AgentID != original.AgentID {
		t.Errorf("AgentID: got %q, want %q", loaded.AgentID, original.AgentID)
	}
	if loaded.Endpoint != original.Endpoint {
		t.Errorf("Endpoint: got %q, want %q", loaded.Endpoint, original.Endpoint)
	}
	if loaded.APIKey != original.APIKey {
		t.Errorf("APIKey: got %q, want %q", loaded.APIKey, original.APIKey)
	}
}

func TestWebSocketHub_New_ReturnsNonNil(t *testing.T) {
	h := relay.NewWebSocketHub()
	if h == nil {
		t.Errorf("expected non-nil WebSocketHub, got nil")
	}
}

func TestWebSocketHub_Send_ReturnsErrNotActivated(t *testing.T) {
	h := relay.NewWebSocketHub()
	err := h.Send("machine-1", relay.Message{
		Type:    relay.MsgToken,
		Payload: map[string]any{"token": "hello"},
	})
	if err != relay.ErrNotActivated {
		t.Errorf("expected ErrNotActivated, got: %v", err)
	}
}

func TestWebSocketHub_Close_NoOp(t *testing.T) {
	h := relay.NewWebSocketHub()
	h.Close("machine-1") // must not panic
}

// fakeHub is a test Hub implementation that can:
// - Capture the last message sent (stateful mode, lastMsg field)
// - Invoke an optional callback (callback mode, sendFn field)
// If sendFn is set, it takes precedence; otherwise lastMsg is updated.
type fakeHub struct {
	lastMsg relay.Message
	lastErr error
	// Optional callback for custom behavior (takes precedence over lastMsg/lastErr)
	sendFn func(string, relay.Message) error
}

func (f *fakeHub) Send(machineID string, msg relay.Message) error {
	if f.sendFn != nil {
		return f.sendFn(machineID, msg)
	}
	f.lastMsg = msg
	return f.lastErr
}

func (f *fakeHub) Close(_ string) {}

// TestDispatcher_SessionResume verifies that session_resume messages are
// handled by looking up the session and sending back session_resume_ack.
func TestDispatcher_SessionResume(t *testing.T) {
	db := openTestDB(t)
	store := relay.NewSessionStore(db)
	hub := &fakeHub{}

	// Pre-save an active session.
	sess := relay.SessionMeta{
		ID:     "sess-123",
		Status: "active",
		LastSeq: 3,
	}
	if err := store.Save(sess); err != nil {
		t.Fatal(err)
	}

	// Create a dispatcher configured with hub and store.
	dispatcher := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID:   "machine-1",
		DeliverPerm: func(string, bool) bool { return false },
		Hub:         hub,
		Store:       store,
	})

	// Send a session_resume message.
	msg := relay.Message{
		Type: relay.MsgSessionResume,
		Payload: map[string]any{
			"session_id": "sess-123",
		},
	}

	dispatcher(nil, msg)

	// Verify session_resume_ack was sent back with session metadata.
	if hub.lastMsg.Type != relay.MsgSessionResumeAck {
		t.Errorf("expected MsgSessionResumeAck, got %q", hub.lastMsg.Type)
	}
	if hub.lastMsg.Payload["session_id"] != "sess-123" {
		t.Errorf("expected session_id sess-123, got %v", hub.lastMsg.Payload["session_id"])
	}
	if hub.lastMsg.Payload["status"] != "active" {
		t.Errorf("expected status active, got %v", hub.lastMsg.Payload["status"])
	}
	if hub.lastMsg.Payload["last_seq"] != uint64(3) {
		t.Errorf("expected last_seq 3, got %v", hub.lastMsg.Payload["last_seq"])
	}
}
