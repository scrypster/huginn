package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestPersistStore_RepairJSONL_MidWriteCrash simulates a crash mid-write
// where the last JSONL line is truncated (not valid JSON).
func TestPersistStore_RepairJSONL_MidWriteCrash(t *testing.T) {
	t.Parallel()
	store := newPersistTestStore(t)
	if err := store.Create(&PersistentSession{
		ID:        "crash-sim",
		Title:     "T",
		Status:    "active",
		Version:   1,
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Write 2 valid messages.
	if err := store.AppendMessage("crash-sim", &PersistedMessage{ID: "m1", Seq: 1, Role: "user", Content: "hello"}); err != nil {
		t.Fatalf("AppendMessage m1: %v", err)
	}
	if err := store.AppendMessage("crash-sim", &PersistedMessage{ID: "m2", Seq: 2, Role: "assistant", Content: "hi"}); err != nil {
		t.Fatalf("AppendMessage m2: %v", err)
	}

	// Simulate truncated last write (partial JSON, no newline).
	jsonlPath := filepath.Join(store.baseDir, "crash-sim", "messages.jsonl")
	f, err := os.OpenFile(jsonlPath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("open jsonl for appending truncated data: %v", err)
	}
	_, _ = f.WriteString(`{"id":"m3","seq":3,"role":"user","content":"truncat`) // truncated — no closing brace
	f.Close()

	n, err := store.RepairJSONL("crash-sim")
	if err != nil {
		t.Fatalf("RepairJSONL: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 valid lines after repair, got %d", n)
	}

	msgs, err := store.ReadMessages("crash-sim")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages post-repair, got %d", len(msgs))
	}
}

// TestPersistStore_ConcurrentUpdateManifest verifies the mutex protects
// concurrent manifest updates from corrupting the file.
func TestPersistStore_ConcurrentUpdateManifest(t *testing.T) {
	t.Parallel()
	store := newPersistTestStore(t)
	if err := store.Create(&PersistentSession{
		ID:        "concurrent-mani",
		Title:     "T",
		Status:    "active",
		Version:   1,
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = store.UpdateManifest("concurrent-mani", func(ps *PersistentSession) {
				ps.MsgCount = n
			})
		}(i)
	}
	wg.Wait()

	// Verify the manifest is still readable and uncorrupted.
	loaded, err := store.LoadManifest("concurrent-mani")
	if err != nil {
		t.Fatalf("manifest corrupted after concurrent updates: %v", err)
	}
	if loaded.ID != "concurrent-mani" {
		t.Errorf("manifest ID corrupted: %q", loaded.ID)
	}
}

// TestPersistStore_LargeMessage verifies that messages larger than 64 KB are
// stored and retrieved without truncation (exercises the manual byte-walker
// in readPersistedJSONL rather than bufio.Scanner with its default limit).
func TestPersistStore_LargeMessage(t *testing.T) {
	t.Parallel()
	store := newPersistTestStore(t)
	if err := store.Create(&PersistentSession{
		ID:        "large-msg",
		Title:     "T",
		Status:    "active",
		Version:   1,
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Build a 1 MB content string of repeated 'x' characters.
	large := strings.Repeat("x", 1024*1024)

	if err := store.AppendMessage("large-msg", &PersistedMessage{
		ID:      "big",
		Seq:     1,
		Role:    "user",
		Content: large,
	}); err != nil {
		t.Fatalf("AppendMessage large: %v", err)
	}

	msgs, err := store.ReadMessages("large-msg")
	if err != nil {
		t.Fatalf("ReadMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}
	if len(msgs[0].Content) != len(large) {
		t.Errorf("content truncated: got %d bytes, want %d", len(msgs[0].Content), len(large))
	}
}

// TestNewID_TimeSortable verifies that IDs generated in sequence sort lexicographically
// in ascending order (ULID property).
func TestNewID_TimeSortable(t *testing.T) {
	t.Parallel()
	ids := make([]string, 10)
	for i := range ids {
		ids[i] = NewID()
		time.Sleep(time.Millisecond) // ensure different millisecond timestamps
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] <= ids[i-1] {
			t.Errorf("ID[%d]=%q not greater than ID[%d]=%q", i, ids[i], i-1, ids[i-1])
		}
	}
}

// TestPersistStore_RepairJSONL_EmptyLines verifies that a file consisting
// entirely of blank lines is handled without error and reports zero valid lines.
func TestPersistStore_RepairJSONL_EmptyLines(t *testing.T) {
	t.Parallel()
	store := newPersistTestStore(t)
	if err := store.Create(&PersistentSession{
		ID:        "empty-lines",
		Title:     "T",
		Status:    "active",
		Version:   1,
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	jsonlPath := filepath.Join(store.baseDir, "empty-lines", "messages.jsonl")
	if err := os.WriteFile(jsonlPath, []byte("\n\n\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	n, err := store.RepairJSONL("empty-lines")
	if err != nil {
		t.Fatalf("RepairJSONL: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 valid lines for blank-only file, got %d", n)
	}
}

// TestPersistStore_UpdateManifest_Concurrent_IDPreserved runs 50 goroutines
// each updating different fields and verifies the session ID is never lost.
func TestPersistStore_UpdateManifest_Concurrent_IDPreserved(t *testing.T) {
	t.Parallel()
	store := newPersistTestStore(t)
	const sessID = "id-preserve-test"
	if err := store.Create(&PersistentSession{
		ID:        sessID,
		Title:     "original",
		Status:    "active",
		Version:   1,
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = store.UpdateManifest(sessID, func(ps *PersistentSession) {
				ps.Title = fmt.Sprintf("title-%d", n)
				ps.MsgCount = n
			})
		}(i)
	}
	wg.Wait()

	loaded, err := store.LoadManifest(sessID)
	if err != nil {
		t.Fatalf("LoadManifest after concurrent updates: %v", err)
	}
	if loaded.ID != sessID {
		t.Errorf("session ID corrupted: got %q, want %q", loaded.ID, sessID)
	}
}
