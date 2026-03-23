package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Store.Exists
// ---------------------------------------------------------------------------

func TestExists_ExistingSession(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	sess := store.New("exists test", "/ws", "model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	if ok := store.Exists(sess.ID); !ok {
		t.Errorf("Exists returned false for a session that was saved")
	}
}

func TestExists_MissingSession(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if ok := store.Exists("totally-fake-id"); ok {
		t.Errorf("Exists returned true for a non-existent session")
	}
}

// ---------------------------------------------------------------------------
// Store.Load — safe-defaults branches
// ---------------------------------------------------------------------------

func TestLoad_SafeDefaults_MissingFields(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create a session directory and write a manifest with empty/zero fields
	// to exercise the safe-defaults branches in Load.
	id := "safe-defaults-sess"
	sessDir := filepath.Join(dir, id)
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Manifest with SessionID="", ID="", Status="", Version=0
	minimal := map[string]any{
		"title": "minimal",
		"model": "test-model",
	}
	data, _ := json.Marshal(minimal)
	if err := os.WriteFile(filepath.Join(sessDir, "manifest.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sess, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load with minimal manifest: %v", err)
	}
	if sess.Manifest.SessionID != id {
		t.Errorf("SessionID safe-default: got %q, want %q", sess.Manifest.SessionID, id)
	}
	if sess.Manifest.ID != id {
		t.Errorf("ID safe-default: got %q, want %q", sess.Manifest.ID, id)
	}
	if sess.Manifest.Status != "active" {
		t.Errorf("Status safe-default: got %q, want 'active'", sess.Manifest.Status)
	}
	if sess.Manifest.Version != 1 {
		t.Errorf("Version safe-default: got %d, want 1", sess.Manifest.Version)
	}
}

func TestLoad_MissingSession_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	_, err := store.Load("nonexistent-id")
	if err == nil {
		t.Error("expected error loading non-existent session, got nil")
	}
}

// ---------------------------------------------------------------------------
// Store.maxMessages — zero branch (MaxMessagesPerSession=0)
// ---------------------------------------------------------------------------

func TestMaxMessages_ZeroUsesDefault(t *testing.T) {
	store := &Store{MaxMessagesPerSession: 0}
	if got := store.maxMessages(); got != DefaultMaxMessagesPerSession {
		t.Errorf("maxMessages with 0: got %d, want %d", got, DefaultMaxMessagesPerSession)
	}
}

func TestMaxMessages_CustomValue(t *testing.T) {
	store := &Store{MaxMessagesPerSession: 42}
	if got := store.maxMessages(); got != 42 {
		t.Errorf("maxMessages with 42: got %d, want 42", got)
	}
}

// ---------------------------------------------------------------------------
// Store.List — with non-existent base dir
// ---------------------------------------------------------------------------

func TestList_NonExistentBaseDir(t *testing.T) {
	store := NewStore("/absolutely/nonexistent/path/xyz123")
	manifests, err := store.List()
	if err != nil {
		t.Errorf("List with non-existent dir should return nil, nil, got err: %v", err)
	}
	if manifests != nil {
		t.Errorf("List with non-existent dir should return nil manifests, got: %v", manifests)
	}
}

func TestList_WithNonDirEntries(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create a valid session
	sess := store.New("valid", "/ws", "model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// Create a plain file in the base dir (should be ignored — !e.IsDir())
	if err := os.WriteFile(filepath.Join(dir, "not-a-dir.txt"), []byte("junk"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Create a directory without manifest.json (ReadFile will fail, should be skipped)
	emptyDir := filepath.Join(dir, "empty-session-dir")
	if err := os.MkdirAll(emptyDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	manifests, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// Should have exactly one manifest — the valid session
	if len(manifests) != 1 {
		t.Errorf("expected 1 manifest, got %d: %v", len(manifests), manifests)
	}
}

// ---------------------------------------------------------------------------
// Store.LoadOrReconstruct — both manifest and JSONL unreadable
// ---------------------------------------------------------------------------

func TestLoadOrReconstruct_BothUnreadable(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create session directory with a corrupt manifest and no JSONL file
	id := "doubly-corrupt"
	sessDir := filepath.Join(dir, id)
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Write corrupt manifest
	if err := os.WriteFile(filepath.Join(sessDir, "manifest.json"), []byte("corrupt"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// No messages.jsonl — TailMessages should return nil, nil (treated as 0 messages)
	// Actually this should succeed via reconstruction with 0 messages.
	sess, err := store.LoadOrReconstruct(id)
	if err != nil {
		t.Fatalf("LoadOrReconstruct with missing JSONL (no messages) should succeed: %v", err)
	}
	if sess.ID != id {
		t.Errorf("reconstructed ID mismatch: got %q, want %q", sess.ID, id)
	}
}

func TestLoadOrReconstruct_SuccessfulLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	sess := store.New("good session", "/ws", "model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	loaded, err := store.LoadOrReconstruct(sess.ID)
	if err != nil {
		t.Fatalf("LoadOrReconstruct: %v", err)
	}
	if loaded.Manifest.Title != "good session" {
		t.Errorf("expected title 'good session', got %q", loaded.Manifest.Title)
	}
}

// ---------------------------------------------------------------------------
// Store.TailMessages — edge cases
// ---------------------------------------------------------------------------

func TestTailMessages_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	sess := store.New("empty", "/ws", "model")

	// Create the session dir and an empty messages.jsonl
	sessDir := filepath.Join(dir, sess.ID)
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, "messages.jsonl"), []byte(""), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	msgs, err := store.TailMessages(sess.ID, 10)
	if err != nil {
		t.Fatalf("TailMessages with empty file: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestTailMessages_LargeN_ReturnsAll(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	sess := store.New("tail-all", "/ws", "model")

	for i := 0; i < 5; i++ {
		if err := store.Append(sess, SessionMessage{Role: "user", Content: "msg"}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	msgs, err := store.TailMessages(sess.ID, 1000)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	if len(msgs) != 5 {
		t.Errorf("expected 5 messages, got %d", len(msgs))
	}
}

func TestTailMessages_WithSkippedCorruptLines(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	sess := store.New("corrupt-lines", "/ws", "model")

	// Write a few valid messages
	for i := 0; i < 3; i++ {
		if err := store.Append(sess, SessionMessage{Role: "user", Content: "valid"}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	// Append a corrupt line directly (repairJSONL will truncate it)
	jsonlPath := filepath.Join(dir, sess.ID, "messages.jsonl")
	f, _ := os.OpenFile(jsonlPath, os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString("\nNOT_JSON_AT_ALL\n")
	f.Close()

	msgs, err := store.TailMessages(sess.ID, 100)
	if err != nil {
		t.Fatalf("TailMessages after corrupt line: %v", err)
	}
	// repairJSONL should have removed the corrupt line
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages after repair, got %d", len(msgs))
	}
}

// ---------------------------------------------------------------------------
// Store.AppendToThread — error paths and normal flow
// ---------------------------------------------------------------------------

func TestAppendToThread_Success(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	err := store.AppendToThread("sess1", "thread1", SessionMessage{
		Role:    "user",
		Content: "thread message",
	})
	if err != nil {
		t.Fatalf("AppendToThread: %v", err)
	}

	msgs, err := store.TailThreadMessages("sess1", "thread1", 10)
	if err != nil {
		t.Fatalf("TailThreadMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 thread message, got %d", len(msgs))
	}
	if msgs[0].Content != "thread message" {
		t.Errorf("unexpected content: %q", msgs[0].Content)
	}
}

func TestAppendToThread_TimestampAssigned(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	before := time.Now().Add(-time.Second)
	err := store.AppendToThread("sess-ts", "thread-ts", SessionMessage{
		Role:    "assistant",
		Content: "timestamped",
		// Ts is zero — should be assigned by AppendToThread
	})
	if err != nil {
		t.Fatalf("AppendToThread: %v", err)
	}

	msgs, err := store.TailThreadMessages("sess-ts", "thread-ts", 1)
	if err != nil {
		t.Fatalf("TailThreadMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Ts.Before(before) {
		t.Errorf("Ts not assigned: got %v, want >= %v", msgs[0].Ts, before)
	}
}

func TestAppendToThread_MultipleMessages(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	for i := 0; i < 5; i++ {
		if err := store.AppendToThread("sess-multi", "t1", SessionMessage{
			Role:    "user",
			Content: "msg",
		}); err != nil {
			t.Fatalf("AppendToThread %d: %v", i, err)
		}
	}

	msgs, err := store.TailThreadMessages("sess-multi", "t1", 10)
	if err != nil {
		t.Fatalf("TailThreadMessages: %v", err)
	}
	if len(msgs) != 5 {
		t.Errorf("expected 5 messages, got %d", len(msgs))
	}
}

// ---------------------------------------------------------------------------
// Store.ListThreadIDs — edge cases
// ---------------------------------------------------------------------------

func TestListThreadIDs_NonExistentSession(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	ids, err := store.ListThreadIDs("no-such-session")
	if err != nil {
		t.Fatalf("ListThreadIDs for non-existent session: %v", err)
	}
	if ids == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(ids) != 0 {
		t.Errorf("expected 0 thread IDs, got %d", len(ids))
	}
}

func TestListThreadIDs_NoThreadFiles(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	sess := store.New("no-threads", "/ws", "model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	ids, err := store.ListThreadIDs(sess.ID)
	if err != nil {
		t.Fatalf("ListThreadIDs: %v", err)
	}
	if ids == nil {
		t.Error("expected empty non-nil slice, got nil")
	}
	if len(ids) != 0 {
		t.Errorf("expected 0 thread IDs, got %d: %v", len(ids), ids)
	}
}

func TestListThreadIDs_WithThreadFiles(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	sessID := "multi-thread-sess"
	threads := []string{"thread-aaa", "thread-bbb"}
	for _, tid := range threads {
		if err := store.AppendToThread(sessID, tid, SessionMessage{Role: "user", Content: "hi"}); err != nil {
			t.Fatalf("AppendToThread: %v", err)
		}
	}

	ids, err := store.ListThreadIDs(sessID)
	if err != nil {
		t.Fatalf("ListThreadIDs: %v", err)
	}
	if len(ids) != len(threads) {
		t.Errorf("expected %d thread IDs, got %d: %v", len(threads), len(ids), ids)
	}
}

// ---------------------------------------------------------------------------
// Store.TailThreadMessages — non-existent file returns nil, nil
// ---------------------------------------------------------------------------

func TestTailThreadMessages_NonExistentFile(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	msgs, err := store.TailThreadMessages("no-sess", "no-thread", 10)
	if err != nil {
		t.Fatalf("TailThreadMessages for non-existent file: %v", err)
	}
	if msgs != nil {
		t.Errorf("expected nil for non-existent thread, got %v", msgs)
	}
}

func TestTailThreadMessages_TailNMessages(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	for i := 0; i < 10; i++ {
		if err := store.AppendToThread("sess-tail-thread", "t1", SessionMessage{
			Role:    "user",
			Content: "msg",
		}); err != nil {
			t.Fatalf("AppendToThread %d: %v", i, err)
		}
	}

	msgs, err := store.TailThreadMessages("sess-tail-thread", "t1", 3)
	if err != nil {
		t.Fatalf("TailThreadMessages: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages (tail 3 of 10), got %d", len(msgs))
	}
}

// ---------------------------------------------------------------------------
// Store.SaveManifest — mkdir creates the session dir on first save
// ---------------------------------------------------------------------------

func TestSaveManifest_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	sess := store.New("dir-create-test", "/ws", "model")
	// Session directory does NOT exist yet
	sessDir := filepath.Join(dir, sess.ID)
	if _, err := os.Stat(sessDir); err == nil {
		t.Fatal("precondition: session dir should not exist yet")
	}

	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	if _, err := os.Stat(sessDir); err != nil {
		t.Errorf("SaveManifest should create session dir: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Store.New — workspace name edge cases
// ---------------------------------------------------------------------------

func TestNew_WorkspaceRootDot(t *testing.T) {
	store := NewStore(t.TempDir())
	sess := store.New("dot test", ".", "model")
	// When workspaceRoot is ".", filepath.Base returns "." so wname = workspaceRoot = "."
	if sess.Manifest.WorkspaceName != "." {
		t.Errorf("expected workspace name '.', got %q", sess.Manifest.WorkspaceName)
	}
}

func TestNew_WorkspaceRootEmpty(t *testing.T) {
	store := NewStore(t.TempDir())
	sess := store.New("empty ws", "", "model")
	// When workspaceRoot is "", filepath.Base("") = "." which matches the condition
	if sess.Manifest.WorkspaceRoot != "" {
		t.Errorf("unexpected workspace root: %q", sess.Manifest.WorkspaceRoot)
	}
}

func TestNew_NormalWorkspaceRoot(t *testing.T) {
	store := NewStore(t.TempDir())
	sess := store.New("normal ws", "/home/user/project", "model")
	if sess.Manifest.WorkspaceName != "project" {
		t.Errorf("expected workspace name 'project', got %q", sess.Manifest.WorkspaceName)
	}
}

// ---------------------------------------------------------------------------
// ExportJSON — empty slice (different from nil)
// ---------------------------------------------------------------------------

func TestExportJSON_EmptySlice(t *testing.T) {
	out, err := ExportJSON([]SessionMessage{})
	if err != nil {
		t.Fatalf("ExportJSON empty slice: %v", err)
	}
	if out != "[]" {
		t.Errorf("expected '[]' for empty slice, got %q", out)
	}
}

func TestExportJSON_MultipleMessages(t *testing.T) {
	msgs := []SessionMessage{
		{Role: "user", Content: "hello", Ts: time.Now()},
		{Role: "assistant", Content: "world", Ts: time.Now()},
	}
	out, err := ExportJSON(msgs)
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	// Result should be a JSON array with 2 elements
	var decoded []SessionMessage
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("decode ExportJSON output: %v", err)
	}
	if len(decoded) != 2 {
		t.Errorf("expected 2 decoded messages, got %d", len(decoded))
	}
}

// ---------------------------------------------------------------------------
// repairJSONL — corrupt content that truncates
// ---------------------------------------------------------------------------

func TestRepairJSONL_CorruptLineAtEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	// Write a valid JSON line followed by invalid data (no newline after corrupt)
	valid, _ := json.Marshal(SessionMessage{Role: "user", Content: "ok"})
	content := string(valid) + "\n" + "{not-valid-json"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := repairJSONL(path); err != nil {
		t.Fatalf("repairJSONL: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after repair: %v", err)
	}

	// The corrupt line should have been truncated
	if len(data) >= len(content) {
		t.Errorf("expected file to be truncated; len before=%d len after=%d", len(content), len(data))
	}
}

func TestRepairJSONL_AllValidNoTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "valid.jsonl")

	valid, _ := json.Marshal(SessionMessage{Role: "user", Content: "ok"})
	content := string(valid) + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := repairJSONL(path); err != nil {
		t.Fatalf("repairJSONL: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after repair: %v", err)
	}

	if string(data) != content {
		t.Errorf("file should not be modified when all lines are valid")
	}
}

func TestRepairJSONL_NonExistentFile(t *testing.T) {
	// repairJSONL on a non-existent file should return nil (not an error)
	err := repairJSONL("/nonexistent/path/that/does/not/exist.jsonl")
	if err != nil {
		t.Errorf("repairJSONL on non-existent file should return nil, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Persist: writePersistentManifest and Create
// ---------------------------------------------------------------------------

func TestCreate_CreatesDirectoryAndManifest(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "sessions"))
	// sessions dir does not exist yet — Create should MkdirAll it

	ps := &PersistentSession{
		ID:        "new-ps-sess",
		Title:     "New PS",
		Model:     "model",
		Status:    "active",
		Version:   1,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := store.Create(ps); err != nil {
		t.Fatalf("Create: %v", err)
	}

	loaded, err := store.LoadManifest("new-ps-sess")
	if err != nil {
		t.Fatalf("LoadManifest after Create: %v", err)
	}
	if loaded.Title != "New PS" {
		t.Errorf("expected title 'New PS', got %q", loaded.Title)
	}
}

// ---------------------------------------------------------------------------
// Persist: AppendMessage — open error (directory not existing)
// ---------------------------------------------------------------------------

func TestAppendMessage_DirectoryMissing(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Note: AppendMessage does NOT create the session dir — caller must do that.
	// Appending without creating the session should fail.
	msg := &PersistedMessage{ID: "m1", Role: "user", Content: "hello"}
	err := store.AppendMessage("no-such-session-dir", msg)
	if err == nil {
		t.Error("expected error when appending to non-existent session dir, got nil")
	}
}

// ---------------------------------------------------------------------------
// Persist: readPersistedJSONL — with \r\n line endings
// ---------------------------------------------------------------------------

func TestReadPersistedJSONL_CRLFLineEndings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "crlf.jsonl")

	msg := &PersistedMessage{ID: "m1", Seq: 1, Role: "user", Content: "crlf test"}
	data, _ := json.Marshal(msg)
	// Write with \r\n endings
	content := string(data) + "\r\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	msgs, err := readPersistedJSONL(path)
	if err != nil {
		t.Fatalf("readPersistedJSONL with CRLF: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != "crlf test" {
		t.Errorf("unexpected content: %q", msgs[0].Content)
	}
}

func TestReadPersistedJSONL_NonExistentFile(t *testing.T) {
	msgs, err := readPersistedJSONL("/no/such/file.jsonl")
	if err != nil {
		t.Fatalf("readPersistedJSONL non-existent: %v", err)
	}
	if msgs != nil {
		t.Errorf("expected nil for non-existent file, got %v", msgs)
	}
}

func TestReadPersistedJSONL_CorruptLines_Skipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mixed.jsonl")

	m1, _ := json.Marshal(&PersistedMessage{ID: "m1", Role: "user", Content: "good"})
	m2, _ := json.Marshal(&PersistedMessage{ID: "m2", Role: "assistant", Content: "also good"})
	content := string(m1) + "\n" + "BAD JSON\n" + string(m2) + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	msgs, err := readPersistedJSONL(path)
	if err != nil {
		t.Fatalf("readPersistedJSONL with corrupt lines: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 valid messages (corrupt line skipped), got %d", len(msgs))
	}
}

// ---------------------------------------------------------------------------
// Persist: UpdateManifest — error when manifest missing
// ---------------------------------------------------------------------------

func TestUpdateManifest_MissingManifest_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	err := store.UpdateManifest("does-not-exist", func(ps *PersistentSession) {
		ps.Title = "updated"
	})
	if err == nil {
		t.Error("expected error from UpdateManifest when manifest is missing, got nil")
	}
}

// ---------------------------------------------------------------------------
// Persist: ReadLastN — exact n equals count
// ---------------------------------------------------------------------------

func TestReadLastN_ExactCount(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	ps := &PersistentSession{
		ID: "exact-n", Title: "T", Status: "active", Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := store.Create(ps); err != nil {
		t.Fatalf("Create: %v", err)
	}

	for i := 0; i < 5; i++ {
		if err := store.AppendMessage("exact-n", &PersistedMessage{
			ID: "m", Role: "user", Content: "x",
		}); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	msgs, err := store.ReadLastN("exact-n", 5) // exactly 5
	if err != nil {
		t.Fatalf("ReadLastN: %v", err)
	}
	if len(msgs) != 5 {
		t.Errorf("expected 5 messages, got %d", len(msgs))
	}
}

// ---------------------------------------------------------------------------
// Store.Delete
// ---------------------------------------------------------------------------

func TestDelete_NonExistentSession(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	// Deleting a non-existent session should not error (os.RemoveAll is tolerant)
	if err := store.Delete("ghost-session"); err != nil {
		t.Errorf("Delete non-existent should not error, got: %v", err)
	}
}

func TestDelete_ExistingSession(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	sess := store.New("to-be-deleted", "/ws", "model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	if ok := store.Exists(sess.ID); !ok {
		t.Fatal("precondition: session should exist before delete")
	}

	if err := store.Delete(sess.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if ok := store.Exists(sess.ID); ok {
		t.Error("session should not exist after delete")
	}
}
