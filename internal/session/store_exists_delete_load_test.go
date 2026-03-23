// Package session — iteration 3 hardening tests.
// Focus: validateID path-traversal rejection, Store.Exists/Delete/Load edge cases,
// LoadOrReconstruct reconstruction from JSONL, concurrent Append+SaveManifest safety.
package session

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// validateID
// ---------------------------------------------------------------------------

// TestValidateID_EmptyString verifies that an empty ID is rejected.
func TestValidateID_EmptyString(t *testing.T) {
	if err := validateID(""); err == nil {
		t.Error("expected error for empty ID, got nil")
	}
}

// TestValidateID_ForwardSlash verifies that an ID containing "/" is rejected.
func TestValidateID_ForwardSlash(t *testing.T) {
	if err := validateID("session/escape"); err == nil {
		t.Error("expected error for ID containing '/', got nil")
	}
}

// TestValidateID_Backslash verifies that an ID containing "\" is rejected.
func TestValidateID_Backslash(t *testing.T) {
	if err := validateID("session\\escape"); err == nil {
		t.Error("expected error for ID containing '\\', got nil")
	}
}

// TestValidateID_DotDot verifies that an ID containing ".." is rejected.
func TestValidateID_DotDot(t *testing.T) {
	if err := validateID("..evil"); err == nil {
		t.Error("expected error for ID containing '..', got nil")
	}
}

// TestValidateID_Valid verifies that a normal ULID-style ID passes validation.
func TestValidateID_Valid(t *testing.T) {
	if err := validateID("01JQPKD3ZNABCDEF01234567AB"); err != nil {
		t.Errorf("expected valid ID to pass, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Store.Exists
// ---------------------------------------------------------------------------

// TestStore_Exists_ReturnsFalseForMissing verifies that Exists returns false
// when no session directory has been created.
func TestStore_Exists_ReturnsFalseForMissing(t *testing.T) {
	store := NewStore(t.TempDir())
	if ok := store.Exists("01NONEXISTENTSESSIONIDXXX"); ok {
		t.Error("expected Exists to return false for missing session")
	}
}

// TestStore_Exists_ReturnsTrueAfterSave verifies that Exists returns true after
// SaveManifest creates the directory on disk.
func TestStore_Exists_ReturnsTrueAfterSave(t *testing.T) {
	store := NewStore(t.TempDir())
	sess := store.New("test", "/ws", "model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	if ok := store.Exists(sess.ID); !ok {
		t.Errorf("expected Exists to return true after SaveManifest, got false")
	}
}

// TestStore_Exists_InvalidIDReturnsFalse verifies that Exists returns false
// (not a panic or error) for invalid IDs.
func TestStore_Exists_InvalidIDReturnsFalse(t *testing.T) {
	store := NewStore(t.TempDir())
	if ok := store.Exists("../escape"); ok {
		t.Error("expected Exists to return false for invalid ID")
	}
}

// ---------------------------------------------------------------------------
// Store.Delete
// ---------------------------------------------------------------------------

// TestStore_Delete_RemovesDirectory verifies that Delete removes the session
// directory from disk and Exists subsequently returns false.
func TestStore_Delete_RemovesDirectory(t *testing.T) {
	store := NewStore(t.TempDir())
	sess := store.New("to-delete", "/ws", "model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	if ok := store.Exists(sess.ID); !ok {
		t.Fatal("expected session to exist before Delete")
	}

	if err := store.Delete(sess.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ok := store.Exists(sess.ID); ok {
		t.Error("expected session to not exist after Delete")
	}
}

// TestStore_Delete_NonExistentSessionSucceeds verifies that deleting a
// non-existent session returns nil (idempotent).
func TestStore_Delete_NonExistentSessionSucceeds(t *testing.T) {
	store := NewStore(t.TempDir())
	// os.RemoveAll on a non-existent path returns nil.
	if err := store.Delete("01NONEXISTENTSESSIONIDXXXX"); err != nil {
		t.Errorf("expected nil error deleting non-existent session, got: %v", err)
	}
}

// TestStore_Delete_InvalidIDReturnsError verifies that Delete rejects an
// invalid (path-traversal) session ID.
func TestStore_Delete_InvalidIDReturnsError(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Delete("../escape"); err == nil {
		t.Error("expected error deleting session with invalid ID, got nil")
	}
}

// ---------------------------------------------------------------------------
// Store.Load edge cases
// ---------------------------------------------------------------------------

// TestStore_Load_InvalidIDReturnsError verifies that Load rejects path-traversal IDs.
func TestStore_Load_InvalidIDReturnsError(t *testing.T) {
	store := NewStore(t.TempDir())
	_, err := store.Load("../evil")
	if err == nil {
		t.Error("expected error for invalid session ID in Load, got nil")
	}
}

// TestStore_Load_MissingManifestReturnsError verifies that Load returns an
// error when the manifest file does not exist.
func TestStore_Load_MissingManifestReturnsError(t *testing.T) {
	store := NewStore(t.TempDir())
	_, err := store.Load("01NONEXISTENTSESSIONIDXXXX")
	if err == nil {
		t.Error("expected error for missing manifest, got nil")
	}
}

// TestStore_Load_CorruptManifestReturnsError verifies that Load returns an
// error when the manifest JSON is malformed.
func TestStore_Load_CorruptManifestReturnsError(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	sess := store.New("corrupt", "/ws", "model")

	// Create the directory and write garbage as the manifest.
	sessDir := filepath.Join(dir, sess.ID)
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, "manifest.json"), []byte("{broken json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := store.Load(sess.ID)
	if err == nil {
		t.Error("expected error for corrupt manifest JSON, got nil")
	}
}

// TestStore_Load_AppliesSafeDefaults verifies that Load backfills safe
// defaults (SessionID, Status, Version) for old manifests missing those fields.
func TestStore_Load_AppliesSafeDefaults(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	sess := store.New("legacy", "/ws", "model")

	// Write a minimal manifest (no session_id / status / version fields).
	sessDir := filepath.Join(dir, sess.ID)
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatal(err)
	}
	minimal := `{"title":"legacy"}`
	if err := os.WriteFile(filepath.Join(sessDir, "manifest.json"), []byte(minimal), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Manifest.SessionID != sess.ID {
		t.Errorf("expected SessionID=%q, got %q", sess.ID, loaded.Manifest.SessionID)
	}
	if loaded.Manifest.Status != "active" {
		t.Errorf("expected Status=active, got %q", loaded.Manifest.Status)
	}
	if loaded.Manifest.Version != 1 {
		t.Errorf("expected Version=1, got %d", loaded.Manifest.Version)
	}
}

// ---------------------------------------------------------------------------
// Store.LoadOrReconstruct
// ---------------------------------------------------------------------------

// TestStore_LoadOrReconstruct_ReconstructsFromJSONL verifies that when the
// manifest is missing but messages.jsonl exists, LoadOrReconstruct returns a
// recovered session with the correct message count.
func TestStore_LoadOrReconstruct_ReconstructsFromJSONL(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	sess := store.New("reconstruct-me", "/ws", "model")

	// Write a few messages without saving the manifest.
	for i := 0; i < 3; i++ {
		if err := store.Append(sess, SessionMessage{Role: "user", Content: "msg"}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	// Do NOT call SaveManifest — so manifest.json does not exist.

	recovered, err := store.LoadOrReconstruct(sess.ID)
	if err != nil {
		t.Fatalf("LoadOrReconstruct: %v", err)
	}
	if !strings.Contains(recovered.Manifest.Title, "recovered") {
		t.Errorf("expected '(recovered)' in title, got %q", recovered.Manifest.Title)
	}
	if recovered.Manifest.MessageCount != 3 {
		t.Errorf("expected MessageCount=3, got %d", recovered.Manifest.MessageCount)
	}
}

// TestStore_LoadOrReconstruct_UsesManifestWhenPresent verifies that when a
// valid manifest exists, it is loaded normally (no reconstruction).
func TestStore_LoadOrReconstruct_UsesManifestWhenPresent(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	sess := store.New("normal-load", "/ws", "model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	loaded, err := store.LoadOrReconstruct(sess.ID)
	if err != nil {
		t.Fatalf("LoadOrReconstruct: %v", err)
	}
	if loaded.Manifest.Title != "normal-load" {
		t.Errorf("expected title 'normal-load', got %q", loaded.Manifest.Title)
	}
	if strings.Contains(loaded.Manifest.Title, "recovered") {
		t.Error("should not contain '(recovered)' when manifest is present")
	}
}

// ---------------------------------------------------------------------------
// Store: concurrent Append + SaveManifest
// ---------------------------------------------------------------------------

// TestStore_ConcurrentAppendAndSaveManifest verifies that concurrent Append
// and SaveManifest calls on the same session do not cause data races or panics.
func TestStore_ConcurrentAppendAndSaveManifest(t *testing.T) {
	store := NewStore(t.TempDir())
	sess := store.New("concurrent", "/ws", "model")

	// Pre-create directory.
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("initial SaveManifest: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = store.Append(sess, SessionMessage{Role: "user", Content: "msg"})
		}()
		go func() {
			defer wg.Done()
			_ = store.SaveManifest(sess)
		}()
	}
	wg.Wait()

	// Verify the JSONL is readable after concurrent writes.
	msgs, err := store.TailMessages(sess.ID, 1000)
	if err != nil {
		t.Fatalf("TailMessages after concurrent writes: %v", err)
	}
	if len(msgs) == 0 {
		t.Error("expected some messages after concurrent Append calls")
	}
}

// ---------------------------------------------------------------------------
// Store.New — workspace name derivation
// ---------------------------------------------------------------------------

// TestStore_New_WorkspaceNameFromPath verifies that WorkspaceName is derived
// from the base name of the workspace root path.
func TestStore_New_WorkspaceNameFromPath(t *testing.T) {
	store := NewStore(t.TempDir())
	sess := store.New("ws-test", "/home/user/myproject", "model")
	if sess.Manifest.WorkspaceName != "myproject" {
		t.Errorf("expected WorkspaceName=myproject, got %q", sess.Manifest.WorkspaceName)
	}
}

// TestStore_New_WorkspaceNameFallsBackToDot verifies that a dot (.) workspace
// root uses the original path value as WorkspaceName.
func TestStore_New_WorkspaceNameFallsBackToDot(t *testing.T) {
	store := NewStore(t.TempDir())
	sess := store.New("ws-dot", ".", "model")
	// filepath.Base(".") == "." which triggers the fallback to workspaceRoot (".")
	if sess.Manifest.WorkspaceName != "." {
		t.Errorf("expected WorkspaceName='.', got %q", sess.Manifest.WorkspaceName)
	}
}

// ---------------------------------------------------------------------------
// Store.TailMessages: invalid ID guard
// ---------------------------------------------------------------------------

// TestTailMessages_InvalidIDReturnsError verifies that TailMessages rejects
// path-traversal session IDs.
func TestTailMessages_InvalidIDReturnsError(t *testing.T) {
	store := NewStore(t.TempDir())
	_, err := store.TailMessages("../escape", 10)
	if err == nil {
		t.Error("expected error for invalid session ID in TailMessages, got nil")
	}
}

// ---------------------------------------------------------------------------
// Store: message seq monotonicity
// ---------------------------------------------------------------------------

// TestAppend_SeqIsMonotonicallyIncreasing verifies that consecutive Append
// calls assign strictly increasing Seq values.
func TestAppend_SeqIsMonotonicallyIncreasing(t *testing.T) {
	store := NewStore(t.TempDir())
	sess := store.New("seq-test", "/ws", "model")
	const count = 5
	for i := 0; i < count; i++ {
		if err := store.Append(sess, SessionMessage{Role: "user", Content: "msg", Ts: time.Now()}); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	msgs, err := store.TailMessages(sess.ID, count)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	for i := 1; i < len(msgs); i++ {
		if msgs[i].Seq <= msgs[i-1].Seq {
			t.Errorf("expected strictly increasing Seq at index %d: %d <= %d",
				i, msgs[i].Seq, msgs[i-1].Seq)
		}
	}
}

// ---------------------------------------------------------------------------
// Store.List: empty base directory
// ---------------------------------------------------------------------------

// TestStore_List_EmptyBaseDir verifies that List returns nil, nil when the
// base directory does not exist.
func TestStore_List_EmptyBaseDir(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "nonexistent"))
	manifests, err := store.List()
	if err != nil {
		t.Fatalf("expected nil error for missing base dir, got: %v", err)
	}
	if manifests != nil {
		t.Errorf("expected nil manifests for missing base dir, got %v", manifests)
	}
}
