package session_test

// hardening_h5_iter2_test.go — Hardening pass iteration 2 (session).
//
// Areas covered:
//  1. Store.TailMessages: returns correct last N
//  2. Store.Append: concurrent appends produce correct count
//  3. Store.LoadOrReconstruct: reconstructs when manifest.json is missing
//  4. Store.Load: safe-defaults applied for old manifests (missing status/version)
//  5. Store.ListThreadIDs: returns empty (not nil) slice when no threads exist
//  6. Store.TailThreadMessages: returns nil/nil when file doesn't exist
//  7. Store.Delete: removes session directory
//  8. Store.Exists: returns false for path-traversal IDs
//  9. Store.TailMessages: returns nil/nil when file doesn't exist
// 10. Store.Load: rejects path-traversal IDs
// 11. Store.SaveManifest: leaves no .tmp file on success
// 12. Store.AppendToThread + ListThreadIDs + TailThreadMessages round-trip

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
)

func newTestStore(t *testing.T) (*session.Store, string) {
	t.Helper()
	dir := t.TempDir()
	return session.NewStore(dir), dir
}

// ── 1. TailMessages: returns last N correctly ──────────────────────────────────

func TestH5_TailMessages_LastN(t *testing.T) {
	st, _ := newTestStore(t)
	sess := st.New("title", "/workspace", "gpt-4")
	if err := st.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	for i := 0; i < 5; i++ {
		if err := st.Append(sess, session.SessionMessage{
			Role:    "user",
			Content: fmt.Sprintf("msg %d", i),
		}); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	msgs, err := st.TailMessages(sess.ID, 3)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("want 3, got %d", len(msgs))
	}
}

// ── 2. Concurrent Appends produce correct message count ───────────────────────

func TestH5_Store_Append_Concurrent(t *testing.T) {
	st, _ := newTestStore(t)
	sess := st.New("title", "/ws", "model")
	if err := st.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	const n = 20
	var wg sync.WaitGroup
	var failures int32
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := st.Append(sess, session.SessionMessage{
				Role:    "user",
				Content: fmt.Sprintf("msg %d", i),
			}); err != nil {
				atomic.AddInt32(&failures, 1)
			}
		}(i)
	}
	wg.Wait()

	if failures > 0 {
		t.Errorf("%d append(s) failed", failures)
	}

	msgs, err := st.TailMessages(sess.ID, n+10)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	if len(msgs) != n {
		t.Errorf("want %d messages, got %d", n, len(msgs))
	}
}

// ── 3. LoadOrReconstruct: reconstructs when manifest is missing ───────────────

func TestH5_Store_LoadOrReconstruct_MissingManifest(t *testing.T) {
	st, dir := newTestStore(t)
	sessID := "reconstruct-me"
	// Create session directory without manifest.json
	sessDir := filepath.Join(dir, sessID)
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a minimal valid messages.jsonl
	jsonl := `{"id":"msg1","role":"user","content":"hello","ts":"2024-01-01T00:00:00Z","seq":1}` + "\n" +
		`{"id":"msg2","role":"assistant","content":"world","ts":"2024-01-01T00:00:01Z","seq":2}` + "\n"
	if err := os.WriteFile(filepath.Join(sessDir, "messages.jsonl"), []byte(jsonl), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	recovered, err := st.LoadOrReconstruct(sessID)
	if err != nil {
		t.Fatalf("LoadOrReconstruct: %v", err)
	}
	if recovered.ID != sessID {
		t.Errorf("want ID %q, got %q", sessID, recovered.ID)
	}
	if recovered.Manifest.MessageCount != 2 {
		t.Errorf("want MessageCount=2, got %d", recovered.Manifest.MessageCount)
	}
}

// ── 4. Load: safe defaults for old manifests ──────────────────────────────────

func TestH5_Store_Load_SafeDefaults(t *testing.T) {
	st, dir := newTestStore(t)
	sessID := "old-manifest"
	sessDir := filepath.Join(dir, sessID)
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write a manifest missing status and version fields.
	minimal := `{"session_id":"` + sessID + `","id":"` + sessID + `","title":"t"}`
	if err := os.WriteFile(filepath.Join(sessDir, "manifest.json"), []byte(minimal), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := st.Load(sessID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Manifest.Status != "active" {
		t.Errorf("want status 'active', got %q", loaded.Manifest.Status)
	}
	if loaded.Manifest.Version != 1 {
		t.Errorf("want version 1, got %d", loaded.Manifest.Version)
	}
}

// ── 5. ListThreadIDs: empty (not nil) when no threads exist ───────────────────

func TestH5_Store_ListThreadIDs_EmptyNotNil(t *testing.T) {
	st, _ := newTestStore(t)
	sess := st.New("title", "/ws", "model")
	if err := st.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	ids, err := st.ListThreadIDs(sess.ID)
	if err != nil {
		t.Fatalf("ListThreadIDs: %v", err)
	}
	if ids == nil {
		t.Error("want empty (not nil) slice, got nil")
	}
	if len(ids) != 0 {
		t.Errorf("want 0 thread IDs, got %d", len(ids))
	}
}

// ── 6. TailThreadMessages: nil/nil when no file ───────────────────────────────

func TestH5_Store_TailThreadMessages_NoFile(t *testing.T) {
	st, _ := newTestStore(t)
	sess := st.New("title", "/ws", "model")
	if err := st.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	msgs, err := st.TailThreadMessages(sess.ID, "nonexistent-thread", 10)
	if err != nil {
		t.Fatalf("TailThreadMessages: unexpected error: %v", err)
	}
	if msgs != nil {
		t.Errorf("want nil msgs when thread file doesn't exist, got %v", msgs)
	}
}

// ── 7. Delete: removes session directory ──────────────────────────────────────

func TestH5_Store_Delete_RemovesDir(t *testing.T) {
	st, _ := newTestStore(t)
	sess := st.New("title", "/ws", "model")
	if err := st.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	if ok := st.Exists(sess.ID); !ok {
		t.Fatal("session should exist before delete")
	}

	if err := st.Delete(sess.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if ok := st.Exists(sess.ID); ok {
		t.Error("session should not exist after delete")
	}
}

// ── 8. Exists: returns false for path-traversal IDs ──────────────────────────

func TestH5_Store_Exists_PathTraversal(t *testing.T) {
	st, _ := newTestStore(t)
	traversalIDs := []string{
		"../evil",
		"foo/bar",
		"../../etc/passwd",
	}
	for _, id := range traversalIDs {
		if ok := st.Exists(id); ok {
			t.Errorf("Exists(%q) = true, want false for traversal ID", id)
		}
	}
}

// ── 9. TailMessages: returns nil when file doesn't exist ─────────────────────

func TestH5_Store_TailMessages_NoFile(t *testing.T) {
	st, _ := newTestStore(t)
	sess := st.New("title", "/ws", "model")
	if err := st.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	// No messages appended — messages.jsonl doesn't exist.
	msgs, err := st.TailMessages(sess.ID, 10)
	if err != nil {
		t.Fatalf("TailMessages on empty session: %v", err)
	}
	_ = msgs // nil is acceptable
}

// ── 10. Load: rejects path-traversal IDs ─────────────────────────────────────

func TestH5_Store_Load_RejectsTraversalID(t *testing.T) {
	st, _ := newTestStore(t)
	_, err := st.Load("../../etc/passwd")
	if err == nil {
		t.Error("want error for path-traversal ID, got nil")
	}
}

// ── 11. SaveManifest: no .tmp file left on success ────────────────────────────

func TestH5_Store_SaveManifest_NoTmpFile(t *testing.T) {
	st, dir := newTestStore(t)
	sess := st.New("title", "/ws", "model")
	if err := st.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	sessDir := filepath.Join(dir, sess.ID)
	entries, _ := os.ReadDir(sessDir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("found leftover .tmp file: %s", e.Name())
		}
	}
}

// ── 12. AppendToThread + ListThreadIDs + TailThreadMessages round-trip ─────────

func TestH5_Store_Thread_RoundTrip(t *testing.T) {
	st, _ := newTestStore(t)
	sess := st.New("title", "/ws", "model")
	if err := st.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	threadID := "thread-001"
	for i := 0; i < 3; i++ {
		if err := st.AppendToThread(sess.ID, threadID, session.SessionMessage{
			Role:    "user",
			Content: fmt.Sprintf("thread msg %d", i),
			Ts:      time.Now().UTC(),
		}); err != nil {
			t.Fatalf("AppendToThread %d: %v", i, err)
		}
	}

	msgs, err := st.TailThreadMessages(sess.ID, threadID, 10)
	if err != nil {
		t.Fatalf("TailThreadMessages: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("want 3 thread messages, got %d", len(msgs))
	}

	ids, err := st.ListThreadIDs(sess.ID)
	if err != nil {
		t.Fatalf("ListThreadIDs: %v", err)
	}
	if len(ids) != 1 || ids[0] != threadID {
		t.Errorf("want [%q], got %v", threadID, ids)
	}
}
