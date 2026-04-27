package session

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

func openTestDBForToolCalls(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestAppendToThread_PersistsToolCallsJSON(t *testing.T) {
	db := openTestDBForToolCalls(t)
	store := NewSQLiteSessionStore(db)
	sess := store.New("agent-test", "/tmp", "model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	msg := SessionMessage{
		ID:      "msg-with-tools",
		Role:    "assistant",
		Content: "I used tools",
		Agent:   "Atlas",
		Ts:      time.Now().UTC(),
		ToolCalls: []PersistedToolCall{
			{ID: "tc-1", Name: "bash", Args: map[string]any{"cmd": "echo hi"}, Result: "hi"},
		},
	}

	if err := store.AppendToThread(sess.ID, "thread-1", msg); err != nil {
		t.Fatalf("AppendToThread: %v", err)
	}

	var raw sql.NullString
	err := db.Read().QueryRow(
		`SELECT tool_calls_json FROM messages WHERE id = ?`, "msg-with-tools",
	).Scan(&raw)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !raw.Valid || raw.String == "" {
		t.Fatal("expected tool_calls_json to be non-NULL, got NULL/empty")
	}
	var calls []PersistedToolCall
	if err := json.Unmarshal([]byte(raw.String), &calls); err != nil {
		t.Fatalf("unmarshal tool_calls_json: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "bash" {
		t.Errorf("expected tool name 'bash', got %q", calls[0].Name)
	}
	if calls[0].Result != "hi" {
		t.Errorf("expected result 'hi', got %q", calls[0].Result)
	}
}

func TestAppendToThread_NilToolCalls_NoColumn(t *testing.T) {
	db := openTestDBForToolCalls(t)
	store := NewSQLiteSessionStore(db)
	sess := store.New("agent-test2", "/tmp", "model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	msg := SessionMessage{
		ID:      "msg-no-tools",
		Role:    "assistant",
		Content: "No tools here",
		Agent:   "Atlas",
		Ts:      time.Now().UTC(),
	}

	if err := store.AppendToThread(sess.ID, "thread-2", msg); err != nil {
		t.Fatalf("AppendToThread: %v", err)
	}

	var raw sql.NullString
	err := db.Read().QueryRow(
		`SELECT tool_calls_json FROM messages WHERE id = ?`, "msg-no-tools",
	).Scan(&raw)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if raw.Valid && raw.String != "" {
		t.Errorf("expected NULL/empty tool_calls_json for message with no tool calls, got %q", raw.String)
	}
}
