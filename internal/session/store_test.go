package session

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	sess := store.New("Test session title", "/workspace/root", "qwen3:30b")
	msg := SessionMessage{
		Role:    "user",
		Content: "hello",
		Ts:      time.Now(),
	}
	if err := store.Append(sess, msg); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	loaded, err := store.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Manifest.Title != "Test session title" {
		t.Errorf("expected title %q, got %q", "Test session title", loaded.Manifest.Title)
	}
	msgs, err := store.TailMessages(sess.ID, 10)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Errorf("expected 1 message 'hello', got %v", msgs)
	}
}

func TestStore_RepairsCorruptJSONL(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	sess := store.New("corrupt test", "/ws", "model")
	store.Append(sess, SessionMessage{Role: "user", Content: "good message"})

	// Corrupt the last line
	jsonlPath := filepath.Join(store.sessionDir(sess.ID), "messages.jsonl")
	f, _ := os.OpenFile(jsonlPath, os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString("\n{broken json\n")
	f.Close()

	msgs, err := store.TailMessages(sess.ID, 10)
	if err != nil {
		t.Fatalf("TailMessages after corruption: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 valid message after repair, got %d", len(msgs))
	}
}

func TestSession_PrimaryAgentID_DefaultsToManifestAgent(t *testing.T) {
	sess := &Session{}
	sess.Manifest.Agent = "Alex"
	if sess.PrimaryAgentID() != "Alex" {
		t.Errorf("expected Alex, got %q", sess.PrimaryAgentID())
	}
}

func TestSession_SetPrimaryAgent_UpdatesManifest(t *testing.T) {
	sess := &Session{}
	sess.SetPrimaryAgent("Stacy")
	if sess.Manifest.Agent != "Stacy" {
		t.Errorf("expected Stacy, got %q", sess.Manifest.Agent)
	}
}

func TestSession_PrimaryAgentID_EmptyByDefault(t *testing.T) {
	sess := &Session{}
	if sess.PrimaryAgentID() != "" {
		t.Errorf("expected empty, got %q", sess.PrimaryAgentID())
	}
}

func TestStore_ListReturnsSessionsSortedByUpdated(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	s1 := store.New("first", "/ws", "model")
	store.SaveManifest(s1)
	time.Sleep(10 * time.Millisecond)
	s2 := store.New("second", "/ws", "model")
	store.SaveManifest(s2)

	entries, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].ID != s2.ID {
		t.Errorf("expected newest session first")
	}
}

func TestSession_PrimaryAgent_ConcurrentAccess(t *testing.T) {
	sess := &Session{}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(name string) {
			defer wg.Done()
			sess.SetPrimaryAgent(name)
		}(fmt.Sprintf("agent-%d", i))
		go func() {
			defer wg.Done()
			_ = sess.PrimaryAgentID()
		}()
	}
	wg.Wait()
}
