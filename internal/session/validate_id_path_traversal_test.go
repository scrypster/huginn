package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateID_RejectsPathTraversal ensures that IDs containing path-separator
// or dot-dot sequences are rejected, preventing directory traversal attacks.
func TestValidateID_RejectsPathTraversal(t *testing.T) {
	malicious := []string{
		"../etc/passwd",
		"../../secret",
		"..",
		"foo/bar",
		"foo\\bar",
		"../",
		"/etc/passwd",
	}
	for _, id := range malicious {
		if err := validateID(id); err == nil {
			t.Errorf("validateID(%q) expected error, got nil", id)
		}
	}
}

// TestValidateID_AllowsValidIDs ensures that normal ULID-like IDs are accepted.
func TestValidateID_AllowsValidIDs(t *testing.T) {
	valid := []string{
		"01JXYZ1234567890ABCDEFGHIJ",
		"session-abc-123",
		"mySession",
	}
	for _, id := range valid {
		if err := validateID(id); err != nil {
			t.Errorf("validateID(%q) unexpected error: %v", id, err)
		}
	}
}

// TestStore_Load_PathTraversal verifies that Load rejects traversal IDs and does
// NOT allow reading files outside the session base directory.
func TestStore_Load_PathTraversal(t *testing.T) {
	baseDir := t.TempDir()
	store := NewStore(baseDir)

	// Place a sentinel file one level above baseDir.
	sentinelDir := filepath.Dir(baseDir)
	sentinelPath := filepath.Join(sentinelDir, "secret.txt")
	_ = os.WriteFile(sentinelPath, []byte("secret"), 0644)
	defer os.Remove(sentinelPath)

	_, err := store.Load("../secret.txt")
	if err == nil {
		t.Fatal("expected error for traversal ID, got nil")
	}
	if !strings.Contains(err.Error(), "invalid characters") && !strings.Contains(err.Error(), "id") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestStore_Delete_PathTraversal verifies that Delete rejects traversal IDs and
// does NOT remove files outside the session base directory.
func TestStore_Delete_PathTraversal(t *testing.T) {
	baseDir := t.TempDir()
	store := NewStore(baseDir)

	// Place a file outside baseDir that must not be deleted.
	outerDir := t.TempDir()
	target := filepath.Join(outerDir, "important.txt")
	_ = os.WriteFile(target, []byte("do not delete"), 0644)

	err := store.Delete("../important.txt")
	if err == nil {
		t.Fatal("expected error for traversal ID, got nil")
	}

	// Confirm the outer file was not deleted.
	if _, statErr := os.Stat(target); os.IsNotExist(statErr) {
		t.Error("traversal Delete removed a file outside the session store")
	}
}

// TestStore_TailMessages_PathTraversal verifies that TailMessages rejects traversal IDs.
func TestStore_TailMessages_PathTraversal(t *testing.T) {
	baseDir := t.TempDir()
	store := NewStore(baseDir)

	_, err := store.TailMessages("../passwd", 10)
	if err == nil {
		t.Fatal("expected error for traversal ID, got nil")
	}
}

// TestStore_AppendToThread_PathTraversal verifies that AppendToThread rejects
// traversal sessionID and threadID values.
func TestStore_AppendToThread_PathTraversal(t *testing.T) {
	baseDir := t.TempDir()
	store := NewStore(baseDir)

	// Traversal in sessionID
	err := store.AppendToThread("../escape", "validthread", SessionMessage{Role: "user", Content: "x"})
	if err == nil {
		t.Error("expected error for traversal sessionID, got nil")
	}

	// Traversal in threadID
	err = store.AppendToThread("validsession", "../escape", SessionMessage{Role: "user", Content: "x"})
	if err == nil {
		t.Error("expected error for traversal threadID, got nil")
	}
}

// TestStore_TailThreadMessages_PathTraversal verifies that TailThreadMessages
// rejects traversal IDs.
func TestStore_TailThreadMessages_PathTraversal(t *testing.T) {
	baseDir := t.TempDir()
	store := NewStore(baseDir)

	_, err := store.TailThreadMessages("../escape", "thread1", 10)
	if err == nil {
		t.Error("expected error for traversal sessionID in TailThreadMessages")
	}

	_, err = store.TailThreadMessages("session1", "../escape", 10)
	if err == nil {
		t.Error("expected error for traversal threadID in TailThreadMessages")
	}
}
