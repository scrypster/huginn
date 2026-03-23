package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newPersistTestStore creates a Store backed by a temp directory for persist tests.
// It creates the sessions subdirectory so AppendMessage / Create can write files.
func newPersistTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessDir, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	return NewStore(sessDir)
}

func newTestPersistSession(id string) *PersistentSession {
	return &PersistentSession{
		ID:        id,
		Title:     "Test session",
		Model:     "test-model",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Status:    "active",
		Version:   1,
	}
}

func TestPersistStore_CreateAndLoadManifest(t *testing.T) {
	store := newPersistTestStore(t)
	ps := newTestPersistSession("test-session-1")

	if err := store.Create(ps); err != nil {
		t.Fatalf("Create: %v", err)
	}

	loaded, err := store.LoadManifest("test-session-1")
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if loaded.Title != ps.Title {
		t.Errorf("title mismatch: got %q, want %q", loaded.Title, ps.Title)
	}
	if loaded.ID != ps.ID {
		t.Errorf("ID mismatch: got %q, want %q", loaded.ID, ps.ID)
	}
	if loaded.Status != "active" {
		t.Errorf("status mismatch: got %q, want %q", loaded.Status, "active")
	}
}

func TestPersistStore_AppendAndReadMessages(t *testing.T) {
	store := newPersistTestStore(t)
	ps := newTestPersistSession("sess-msgs")
	store.Create(ps)

	msgs := []*PersistedMessage{
		{ID: "m1", Seq: 1, Role: "user", Content: "hello"},
		{ID: "m2", Seq: 2, Role: "assistant", Content: "hi there"},
		{ID: "m3", Seq: 3, Role: "user", Content: "how are you"},
	}
	for _, m := range msgs {
		if err := store.AppendMessage("sess-msgs", m); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	read, err := store.ReadMessages("sess-msgs")
	if err != nil {
		t.Fatalf("ReadMessages: %v", err)
	}
	if len(read) != 3 {
		t.Errorf("expected 3 messages, got %d", len(read))
	}
	if read[1].Content != "hi there" {
		t.Errorf("content mismatch: %q", read[1].Content)
	}
}

func TestPersistStore_ReadMessages_NoFile(t *testing.T) {
	store := newPersistTestStore(t)
	store.Create(newTestPersistSession("sess-nomsg2"))

	msgs, err := store.ReadMessages("sess-nomsg2")
	if err != nil {
		t.Fatalf("ReadMessages on missing jsonl: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestPersistStore_ReadLastN(t *testing.T) {
	store := newPersistTestStore(t)
	store.Create(newTestPersistSession("sess-tail"))

	for i := 0; i < 20; i++ {
		store.AppendMessage("sess-tail", &PersistedMessage{
			ID:      fmt.Sprintf("m%d", i),
			Seq:     int64(i),
			Role:    "user",
			Content: fmt.Sprintf("message %d", i),
		})
	}

	last5, err := store.ReadLastN("sess-tail", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(last5) != 5 {
		t.Errorf("expected 5, got %d", len(last5))
	}
	// Last message should be message 19
	if last5[4].Content != "message 19" {
		t.Errorf("last message: %q", last5[4].Content)
	}
	// First of last 5 should be message 15
	if last5[0].Content != "message 15" {
		t.Errorf("first of last 5: %q", last5[0].Content)
	}
}

func TestPersistStore_ReadLastN_FewerThanN(t *testing.T) {
	store := newPersistTestStore(t)
	store.Create(newTestPersistSession("sess-few"))

	for i := 0; i < 3; i++ {
		store.AppendMessage("sess-few", &PersistedMessage{
			ID:      fmt.Sprintf("m%d", i),
			Seq:     int64(i),
			Role:    "user",
			Content: fmt.Sprintf("msg %d", i),
		})
	}

	all, err := store.ReadLastN("sess-few", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 (all), got %d", len(all))
	}
}

func TestPersistStore_RepairJSONL_CorruptLastLine(t *testing.T) {
	store := newPersistTestStore(t)
	store.Create(newTestPersistSession("sess-repair"))

	// Write 2 valid lines + 1 corrupt line directly.
	jsonlPath := filepath.Join(store.baseDir, "sess-repair", "messages.jsonl")
	valid1, _ := json.Marshal(&PersistedMessage{ID: "m1", Seq: 1, Role: "user", Content: "ok"})
	valid2, _ := json.Marshal(&PersistedMessage{ID: "m2", Seq: 2, Role: "assistant", Content: "ok too"})
	content := string(valid1) + "\n" + string(valid2) + "\n{corrupt json!!!\n"
	os.WriteFile(jsonlPath, []byte(content), 0600)

	n, err := store.RepairJSONL("sess-repair")
	if err != nil {
		t.Fatalf("RepairJSONL: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 valid lines, got %d", n)
	}

	msgs, err := store.ReadMessages("sess-repair")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages after repair, got %d", len(msgs))
	}
}

func TestPersistStore_RepairJSONL_AllCorrupt(t *testing.T) {
	store := newPersistTestStore(t)
	store.Create(newTestPersistSession("sess-allcorrupt"))
	jsonlPath := filepath.Join(store.baseDir, "sess-allcorrupt", "messages.jsonl")
	os.WriteFile(jsonlPath, []byte("{bad}\n{also bad}\n"), 0600)

	n, err := store.RepairJSONL("sess-allcorrupt")
	if err != nil {
		t.Fatalf("RepairJSONL all corrupt: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 valid lines for all-corrupt, got %d", n)
	}
}

func TestPersistStore_RepairJSONL_Missing(t *testing.T) {
	store := newPersistTestStore(t)
	store.Create(newTestPersistSession("sess-nomsg"))

	n, err := store.RepairJSONL("sess-nomsg")
	if err != nil {
		t.Errorf("RepairJSONL on missing JSONL: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 for missing file, got %d", n)
	}
}

func TestPersistStore_RepairJSONL_AllValid(t *testing.T) {
	store := newPersistTestStore(t)
	store.Create(newTestPersistSession("sess-allvalid"))

	for i := 0; i < 5; i++ {
		store.AppendMessage("sess-allvalid", &PersistedMessage{
			ID:      fmt.Sprintf("m%d", i),
			Seq:     int64(i),
			Role:    "user",
			Content: fmt.Sprintf("msg %d", i),
		})
	}

	n, err := store.RepairJSONL("sess-allvalid")
	if err != nil {
		t.Fatalf("RepairJSONL all valid: %v", err)
	}
	if n != 5 {
		t.Errorf("expected 5 valid lines, got %d", n)
	}

	msgs, _ := store.ReadMessages("sess-allvalid")
	if len(msgs) != 5 {
		t.Errorf("messages intact after no-op repair: got %d", len(msgs))
	}
}

func TestPersistStore_List_SortedByUpdatedAt(t *testing.T) {
	store := newPersistTestStore(t)

	s1 := newTestPersistSession("sess-a")
	s1.UpdatedAt = time.Now().Add(-2 * time.Hour)
	store.Create(s1)

	s2 := newTestPersistSession("sess-b")
	s2.UpdatedAt = time.Now().Add(-1 * time.Hour)
	store.Create(s2)

	s3 := newTestPersistSession("sess-c")
	s3.UpdatedAt = time.Now()
	store.Create(s3)

	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Errorf("expected 3, got %d", len(list))
	}
	// List() returns []Manifest sorted by UpdatedAt desc.
	// PersistentSession.ID is marshalled as "session_id" which maps to Manifest.SessionID.
	if list[0].SessionID != "sess-c" {
		t.Errorf("newest first: expected sess-c, got %s", list[0].SessionID)
	}
	if list[2].SessionID != "sess-a" {
		t.Errorf("oldest last: expected sess-a, got %s", list[2].SessionID)
	}
}

func TestPersistStore_List_Empty(t *testing.T) {
	store := newPersistTestStore(t)
	list, err := store.List()
	if err != nil {
		t.Fatalf("List empty: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}
}

func TestPersistStore_Delete(t *testing.T) {
	store := newPersistTestStore(t)
	store.Create(newTestPersistSession("sess-del"))

	if err := store.Delete("sess-del"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.LoadManifest("sess-del"); err == nil {
		t.Error("expected error loading deleted session")
	}
}

func TestPersistStore_UpdateManifest(t *testing.T) {
	store := newPersistTestStore(t)
	store.Create(newTestPersistSession("sess-update"))

	if err := store.UpdateManifest("sess-update", func(ps *PersistentSession) {
		ps.Title = "Updated Title"
		ps.MsgCount = 42
	}); err != nil {
		t.Fatalf("UpdateManifest: %v", err)
	}

	loaded, err := store.LoadManifest("sess-update")
	if err != nil {
		t.Fatalf("LoadManifest after update: %v", err)
	}
	if loaded.Title != "Updated Title" {
		t.Errorf("title not updated: %q", loaded.Title)
	}
	if loaded.MsgCount != 42 {
		t.Errorf("msg count not updated: %d", loaded.MsgCount)
	}
}

func TestNewMessageID_Unique(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := NewMessageID()
		if ids[id] {
			t.Errorf("duplicate ID generated: %s", id)
		}
		ids[id] = true
	}
}

func TestNewID_Unique(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := NewID()
		if ids[id] {
			t.Errorf("duplicate ID generated: %s", id)
		}
		ids[id] = true
	}
}

func TestPersistStore_ConcurrentAppend(t *testing.T) {
	t.Parallel()
	store := newPersistTestStore(t)
	store.Create(newTestPersistSession("sess-concurrent"))

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(n int) {
			for j := 0; j < 10; j++ {
				store.AppendMessage("sess-concurrent", &PersistedMessage{
					ID:      fmt.Sprintf("m-%d-%d", n, j),
					Seq:     int64(n*10 + j),
					Role:    "user",
					Content: "concurrent write",
				})
			}
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}

	msgs, err := store.ReadMessages("sess-concurrent")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 100 {
		t.Errorf("expected 100 messages, got %d", len(msgs))
	}
}

func TestPersistStore_LoadManifest_Missing(t *testing.T) {
	store := newPersistTestStore(t)
	_, err := store.LoadManifest("does-not-exist")
	if err == nil {
		t.Error("expected error for missing session, got nil")
	}
}

func TestPersistStore_PersistedMessageFields(t *testing.T) {
	store := newPersistTestStore(t)
	store.Create(newTestPersistSession("sess-fields"))

	msg := &PersistedMessage{
		ID:           "msg-001",
		Ts:           "2026-01-01T00:00:00Z",
		Seq:          1,
		Role:         "assistant",
		Content:      "response text",
		Agent:        "coder",
		ToolName:     "bash",
		ToolCallID:   "call-abc",
		Type:         "tool_result",
		PromptTokens: 100,
		CompTokens:   50,
		CostUSD:      0.00123,
		Model:        "qwen3:30b",
	}
	if err := store.AppendMessage("sess-fields", msg); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	msgs, err := store.ReadMessages("sess-fields")
	if err != nil {
		t.Fatalf("ReadMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	got := msgs[0]
	if got.Agent != "coder" {
		t.Errorf("agent: %q", got.Agent)
	}
	if got.ToolName != "bash" {
		t.Errorf("tool_name: %q", got.ToolName)
	}
	if got.CostUSD != 0.00123 {
		t.Errorf("cost_usd: %f", got.CostUSD)
	}
	if got.PromptTokens != 100 {
		t.Errorf("prompt_tokens: %d", got.PromptTokens)
	}
}
