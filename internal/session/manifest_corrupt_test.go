package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLoad_CorruptManifest_GracefulError verifies that a truncated or invalid
// manifest.json causes Load() to return a descriptive error rather than panic,
// and that the error message is actionable.
//
// Bug: Load() calls json.Unmarshal on manifest.json without any fallback.
// A corrupt file returns an opaque unmarshal error, losing all session metadata.
// The caller cannot distinguish "file missing" from "file corrupt".
//
// Fix: wrap the unmarshal error with context about which session failed and why,
// so callers (TUI picker) can skip corrupt sessions gracefully rather than
// crashing or showing misleading errors.
func TestLoad_CorruptManifest_GracefulError(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create a session and save it.
	sess := store.New("corruption test", "/ws", "model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// Corrupt the manifest by truncating it mid-JSON.
	manifestPath := filepath.Join(dir, sess.ID, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	truncated := data[:len(data)/2] // cut in half → invalid JSON
	if err := os.WriteFile(manifestPath, truncated, 0644); err != nil {
		t.Fatalf("write truncated manifest: %v", err)
	}

	// Load must return an error (not panic) with a useful message.
	_, err = store.Load(sess.ID)
	if err == nil {
		t.Fatal("expected error loading corrupt manifest, got nil")
	}
	t.Logf("got expected error: %v", err) // informational
}

// TestLoad_CorruptManifest_ListSkips verifies that List() silently skips sessions
// with corrupt manifests rather than returning an error for the whole list.
func TestLoad_CorruptManifest_ListSkips(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create two sessions.
	good := store.New("good session", "/ws", "model")
	store.SaveManifest(good)
	time.Sleep(5 * time.Millisecond) // ensure ordering

	bad := store.New("bad session", "/ws", "model")
	store.SaveManifest(bad)

	// Corrupt bad session's manifest.
	manifestPath := filepath.Join(dir, bad.ID, "manifest.json")
	if err := os.WriteFile(manifestPath, []byte("{bad json"), 0644); err != nil {
		t.Fatalf("corrupt manifest: %v", err)
	}

	// List must succeed and include at least the good session.
	manifests, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	found := false
	for _, m := range manifests {
		if m.ID == good.ID {
			found = true
		}
		if m.ID == bad.ID {
			t.Errorf("bad session should have been skipped, but was included: %+v", m)
		}
	}
	if !found {
		t.Errorf("good session %q not found in list: %v", good.ID, manifests)
	}
}

// TestLoad_CorruptManifest_ReconstructFromJSONL verifies that when a manifest
// is unreadable, Load can optionally reconstruct a partial manifest from the
// JSONL file (message count, last message ID).
//
// This is a best-effort test: Load returns an error, but the system should not
// lose all session data if the JSONL file is intact.
func TestLoad_CorruptManifest_ReconstructFromJSONL(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Write some messages.
	sess := store.New("reconstruct test", "/ws", "model")
	for i := 0; i < 3; i++ {
		store.Append(sess, SessionMessage{Role: "user", Content: "msg"})
	}
	store.SaveManifest(sess)

	// Now corrupt the manifest.
	manifestPath := filepath.Join(dir, sess.ID, "manifest.json")
	os.WriteFile(manifestPath, []byte("corrupted"), 0644)

	// LoadOrReconstruct should return a partial session rather than an error.
	reconstructed, err := store.LoadOrReconstruct(sess.ID)
	if err != nil {
		t.Fatalf("LoadOrReconstruct: %v", err)
	}
	if reconstructed.Manifest.MessageCount != 3 {
		t.Errorf("expected MessageCount=3, got %d", reconstructed.Manifest.MessageCount)
	}
	if reconstructed.ID != sess.ID {
		t.Errorf("expected ID %q, got %q", sess.ID, reconstructed.ID)
	}
}
