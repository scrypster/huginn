package connections

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func makeConn(id string, provider Provider) Connection {
	return Connection{
		ID:           id,
		Provider:     provider,
		AccountLabel: "Test Account",
		AccountID:    "acct-" + id,
		Scopes:       []string{"read", "write"},
		CreatedAt:    time.Now().UTC().Truncate(time.Second),
		ExpiresAt:    time.Now().UTC().Add(time.Hour).Truncate(time.Second),
	}
}

func TestStoreAddAndList(t *testing.T) {
	s := newTestStore(t)

	c1 := makeConn("conn-1", ProviderGoogle)
	c2 := makeConn("conn-2", ProviderSlack)

	if err := s.Add(c1); err != nil {
		t.Fatalf("Add c1: %v", err)
	}
	if err := s.Add(c2); err != nil {
		t.Fatalf("Add c2: %v", err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(list))
	}

	// Duplicate ID should fail
	if err := s.Add(c1); err == nil {
		t.Fatal("expected error adding duplicate ID")
	}
}

func TestStoreRemove(t *testing.T) {
	s := newTestStore(t)

	c := makeConn("conn-1", ProviderGitHub)
	if err := s.Add(c); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := s.Remove(c.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	list, _ := s.List()
	if len(list) != 0 {
		t.Fatalf("expected 0 connections after remove, got %d", len(list))
	}

	// Removing non-existent should error
	if err := s.Remove("nonexistent"); err == nil {
		t.Fatal("expected error removing nonexistent connection")
	}
}

func TestStoreGet(t *testing.T) {
	s := newTestStore(t)

	c := makeConn("conn-1", ProviderJira)
	if err := s.Add(c); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, ok := s.Get("conn-1")
	if !ok {
		t.Fatal("Get: expected to find conn-1")
	}
	if got.ID != c.ID || got.Provider != c.Provider {
		t.Errorf("Get returned wrong connection: %+v", got)
	}

	_, ok = s.Get("nonexistent")
	if ok {
		t.Fatal("Get: expected false for nonexistent id")
	}
}

func TestStoreListByProvider(t *testing.T) {
	s := newTestStore(t)

	if err := s.Add(makeConn("g1", ProviderGoogle)); err != nil {
		t.Fatalf("Add g1: %v", err)
	}
	if err := s.Add(makeConn("g2", ProviderGoogle)); err != nil {
		t.Fatalf("Add g2: %v", err)
	}
	if err := s.Add(makeConn("s1", ProviderSlack)); err != nil {
		t.Fatalf("Add s1: %v", err)
	}

	googleConns, err := s.ListByProvider(ProviderGoogle)
	if err != nil {
		t.Fatalf("ListByProvider: %v", err)
	}
	if len(googleConns) != 2 {
		t.Errorf("expected 2 Google connections, got %d", len(googleConns))
	}

	slackConns, err := s.ListByProvider(ProviderSlack)
	if err != nil {
		t.Fatalf("ListByProvider Slack: %v", err)
	}
	if len(slackConns) != 1 {
		t.Errorf("expected 1 Slack connection, got %d", len(slackConns))
	}

	jiraConns, err := s.ListByProvider(ProviderJira)
	if err != nil {
		t.Fatalf("ListByProvider Jira: %v", err)
	}
	if len(jiraConns) != 0 {
		t.Errorf("expected 0 Jira connections, got %d", len(jiraConns))
	}
}

func TestStoreUpdateExpiry(t *testing.T) {
	s := newTestStore(t)

	c := makeConn("conn-1", ProviderBitbucket)
	if err := s.Add(c); err != nil {
		t.Fatalf("Add: %v", err)
	}

	newExpiry := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	if err := s.UpdateExpiry("conn-1", newExpiry); err != nil {
		t.Fatalf("UpdateExpiry: %v", err)
	}

	got, ok := s.Get("conn-1")
	if !ok {
		t.Fatal("Get after UpdateExpiry: not found")
	}
	if !got.ExpiresAt.Equal(newExpiry) {
		t.Errorf("ExpiresAt: got %v, want %v", got.ExpiresAt, newExpiry)
	}

	// Non-existent ID should error
	if err := s.UpdateExpiry("nonexistent", newExpiry); err == nil {
		t.Fatal("expected error updating expiry for nonexistent connection")
	}
}

func TestStoreAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "connections.json")

	s, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	if err := s.Add(makeConn("conn-1", ProviderGoogle)); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Verify the final file exists and no temp files remain
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	var tmpFiles []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			tmpFiles = append(tmpFiles, e.Name())
		}
	}
	if len(tmpFiles) > 0 {
		t.Errorf("expected no leftover temp files, found: %v", tmpFiles)
	}

	// Verify store file exists and is valid JSON by reloading it
	s2, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore reload: %v", err)
	}
	list, _ := s2.List()
	if len(list) != 1 {
		t.Errorf("expected 1 connection after reload, got %d", len(list))
	}
}

// TestStoreLoadInvalidJSON tests loading invalid JSON from file
func TestStoreLoadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "bad.json")

	// Write invalid JSON
	if err := os.WriteFile(storePath, []byte("{invalid json}"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := NewStore(storePath)
	if err == nil {
		t.Fatal("expected error loading invalid JSON")
	}
}

// TestStoreLoadEmpty tests that empty/missing store file results in empty list
func TestStoreLoadEmpty(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "nonexistent.json")

	s, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list for new store, got %d", len(list))
	}
}

// TestStoreLoadReadError tests handling of file read errors
func TestStoreLoadReadError(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "connections.json")

	// Create store file with valid content
	if err := os.WriteFile(storePath, []byte("[]"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Create a store and add data
	s, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Add(makeConn("test", ProviderGoogle)); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Now make the file unreadable (on Unix-like systems)
	if err := os.Chmod(storePath, 0000); err != nil {
		t.Skipf("cannot change file permissions on this system: %v", err)
	}
	defer os.Chmod(storePath, 0644) // restore for cleanup

	_, err = NewStore(storePath)
	if err == nil {
		t.Skipf("cannot reliably test permission errors on this system")
	}
}

// TestStoreConcurrentAccess verifies thread-safe concurrent operations
func TestStoreConcurrentAccess(t *testing.T) {
	s := newTestStore(t)

	// Initial connection
	if err := s.Add(makeConn("base", ProviderGoogle)); err != nil {
		t.Fatalf("Add: %v", err)
	}

	var wg sync.WaitGroup
	const goroutines = 10

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// Concurrent reads
			list, _ := s.List()
			if len(list) < 1 {
				t.Errorf("goroutine %d: list too small", n)
			}
		}(i)
	}

	wg.Wait()
}
