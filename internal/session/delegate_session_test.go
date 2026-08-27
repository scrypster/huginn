package session

import (
	"strings"
	"testing"
	"time"
)

func TestLoadForDelegate_NoSessionID_DoesNotStub(t *testing.T) {
	store := NewStore(t.TempDir())
	sess, err := LoadForDelegate(store, "", "space-1")
	if err == nil {
		t.Fatal("empty session ID must error")
	}
	if !strings.Contains(err.Error(), "no session ID in context") {
		t.Fatalf("err = %v", err)
	}
	if sess != nil {
		t.Fatal("must not return a stub session")
	}
	list, listErr := store.List()
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(list) != 0 {
		t.Fatalf("empty session ID must not persist a stub, got %+v", list)
	}
}

func TestLoadForDelegate_NoStoreNoSpace_DoesNotStub(t *testing.T) {
	sess, err := LoadForDelegate(nil, "space-thread-parent-Steve", "")
	if err == nil || sess != nil {
		t.Fatalf("no store and no space must not stub, sess=%+v err=%v", sess, err)
	}
}

func TestLoadForDelegate_PersistsWithSpaceID(t *testing.T) {
	store := NewStore(t.TempDir())
	sess, err := LoadForDelegate(store, "space-thread-p-Steve", "desk-dm")
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID != "space-thread-p-Steve" {
		t.Fatalf("id = %q", sess.ID)
	}
	if sess.SpaceID() != "desk-dm" {
		t.Fatalf("space = %q", sess.SpaceID())
	}
	loaded, loadErr := store.Load("space-thread-p-Steve")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if loaded.SpaceID() != "desk-dm" {
		t.Fatalf("persisted space = %q", loaded.SpaceID())
	}
}

func TestLoadForDelegate_ExistingSession_BindsSpace(t *testing.T) {
	store := NewStore(t.TempDir())
	now := time.Now().UTC()
	if err := store.SaveManifest(&Session{
		ID: "hallway",
		Manifest: Manifest{
			ID: "hallway", SessionID: "hallway",
			Status: "active", Version: 1,
			CreatedAt: now, UpdatedAt: now,
		},
	}); err != nil {
		t.Fatal(err)
	}
	sess, err := LoadForDelegate(store, "hallway", "desk-dm")
	if err != nil {
		t.Fatal(err)
	}
	if sess.SpaceID() != "desk-dm" {
		t.Fatalf("bound space = %q", sess.SpaceID())
	}
}
