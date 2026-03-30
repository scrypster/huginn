package session_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
)

// TestTailMessages_ReturnsPersistedToolCalls verifies that tool calls appended
// with an assistant message survive a round-trip through SQLite and are returned
// by TailMessages. This is the regression test for tool calls being lost on page
// refresh.
func TestTailMessages_ReturnsPersistedToolCalls(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	sess := s.New("tool calls test", "", "qwen3:30b")
	if err := s.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	toolCalls := []session.PersistedToolCall{
		{
			ID:     "call_abc123",
			Name:   "muninn_recall",
			Args:   map[string]any{"vault": "default", "context": "user preferences"},
			Result: `{"memories": [{"content": "user likes sci-fi"}]}`,
		},
	}

	msg := session.SessionMessage{
		Role:      "assistant",
		Content:   "I recalled your preferences.",
		Agent:     "Mike",
		Ts:        time.Now().UTC(),
		ToolCalls: toolCalls,
	}

	if err := s.Append(sess, msg); err != nil {
		t.Fatalf("Append: %v", err)
	}

	msgs, err := s.TailMessages(sess.ID, 10)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	got := msgs[0]
	if len(got.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call on message, got %d", len(got.ToolCalls))
	}
	tc := got.ToolCalls[0]
	if tc.ID != "call_abc123" {
		t.Errorf("tool call ID: want call_abc123, got %q", tc.ID)
	}
	if tc.Name != "muninn_recall" {
		t.Errorf("tool call Name: want muninn_recall, got %q", tc.Name)
	}
	if tc.Result != `{"memories": [{"content": "user likes sci-fi"}]}` {
		t.Errorf("tool call Result: unexpected value %q", tc.Result)
	}
}

// TestTailMessages_NoToolCalls_Empty verifies that messages without tool calls
// return an empty slice (not nil) for ToolCalls — so frontend logic is consistent.
func TestTailMessages_NoToolCalls_Empty(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	sess := s.New("no tool calls", "", "qwen3:30b")
	if err := s.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	if err := s.Append(sess, session.SessionMessage{
		Role:    "assistant",
		Content: "Hello there.",
		Agent:   "Mike",
		Ts:      time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	msgs, err := s.TailMessages(sess.ID, 10)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].ToolCalls != nil {
		t.Errorf("expected nil ToolCalls for message with no tool calls, got %v", msgs[0].ToolCalls)
	}
}

// TestTailMessages_MultipleToolCalls verifies that multiple tool calls on a
// single message are all persisted and returned correctly.
func TestTailMessages_MultipleToolCalls(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	sess := s.New("multi tool calls", "", "qwen3:30b")
	if err := s.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	toolCalls := []session.PersistedToolCall{
		{ID: "call_1", Name: "muninn_recall", Args: map[string]any{"vault": "default", "context": "foo"}, Result: "result1"},
		{ID: "call_2", Name: "muninn_remember", Args: map[string]any{"vault": "default", "concept": "bar", "content": "baz"}, Result: "stored"},
	}

	if err := s.Append(sess, session.SessionMessage{
		Role:      "assistant",
		Content:   "I recalled and stored.",
		Agent:     "Mike",
		Ts:        time.Now().UTC(),
		ToolCalls: toolCalls,
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	msgs, err := s.TailMessages(sess.ID, 10)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	if len(msgs[0].ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(msgs[0].ToolCalls))
	}
	if msgs[0].ToolCalls[0].ID != "call_1" {
		t.Errorf("first tool call ID: want call_1, got %q", msgs[0].ToolCalls[0].ID)
	}
	if msgs[0].ToolCalls[1].ID != "call_2" {
		t.Errorf("second tool call ID: want call_2, got %q", msgs[0].ToolCalls[1].ID)
	}
}

// TestTailMessagesBefore_ReturnsToolCalls verifies that tool calls persisted with
// an assistant message are returned by TailMessagesBefore (pagination path).
//
// Regression: TailMessagesBefore queried without tool_calls_json, so paginated
// history always lost tool call data silently.
func TestTailMessagesBefore_ReturnsToolCalls(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	sess := s.New("paging tool calls test", "", "qwen3:30b")
	if err := s.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// Append a plain user message first (seq=1).
	if err := s.Append(sess, session.SessionMessage{
		Role: "user", Content: "hello", Ts: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Append user: %v", err)
	}

	// Append assistant message with tool calls (seq=2).
	toolCalls := []session.PersistedToolCall{
		{ID: "call_paged", Name: "muninn_recall", Args: map[string]any{"vault": "default"}, Result: "memories"},
	}
	if err := s.Append(sess, session.SessionMessage{
		Role: "assistant", Content: "Here you go.", Agent: "Mike",
		Ts:        time.Now().UTC(),
		ToolCalls: toolCalls,
	}); err != nil {
		t.Fatalf("Append assistant: %v", err)
	}

	// Append a third message so we can page to the second via beforeSeq=3.
	if err := s.Append(sess, session.SessionMessage{
		Role: "user", Content: "thanks", Ts: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Append user2: %v", err)
	}

	// TailMessagesBefore(seq<3) should return the first two messages, including tool calls on msg[1].
	msgs, err := s.TailMessagesBefore(sess.ID, 10, 3)
	if err != nil {
		t.Fatalf("TailMessagesBefore: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	assistant := msgs[1]
	if len(assistant.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call on paginated assistant message, got %d — tool calls lost on pagination", len(assistant.ToolCalls))
	}
	if assistant.ToolCalls[0].Name != "muninn_recall" {
		t.Errorf("tool call Name: got %q, want muninn_recall", assistant.ToolCalls[0].Name)
	}
}

// TestToolCallsJSON_RoundTrip verifies the JSON marshaling is stable.
func TestToolCallsJSON_RoundTrip(t *testing.T) {
	t.Parallel()
	tcs := []session.PersistedToolCall{
		{
			ID:     "call_xyz",
			Name:   "web_search",
			Args:   map[string]any{"query": "golang testing"},
			Result: "10 results found",
		},
	}
	data, err := json.Marshal(tcs)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got []session.PersistedToolCall
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got[0].Name != "web_search" {
		t.Errorf("Name: want web_search, got %q", got[0].Name)
	}
}
