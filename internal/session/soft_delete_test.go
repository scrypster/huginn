package session

import (
	"testing"
)

// TestStore_ArchiveSession_HidesFromDefaultList verifies that once a session is
// archived it no longer appears in the default ListFiltered result but does
// appear when IncludeArchived is true.
func TestStore_ArchiveSession_HidesFromDefaultList(t *testing.T) {
	store := NewStore(t.TempDir())

	sess := store.New("test session", "/workspace", "claude")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// Before archiving: session appears in default list.
	manifests, err := store.ListFiltered(SessionFilter{IncludeArchived: false})
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	found := false
	for _, m := range manifests {
		if m.ID == sess.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("expected session in default list before archiving")
	}

	// Archive it.
	if err := store.ArchiveSession(sess.ID); err != nil {
		t.Fatalf("ArchiveSession: %v", err)
	}

	// After archiving: session must NOT appear in default list.
	manifests, err = store.ListFiltered(SessionFilter{IncludeArchived: false})
	if err != nil {
		t.Fatalf("ListFiltered after archive: %v", err)
	}
	for _, m := range manifests {
		if m.ID == sess.ID {
			t.Errorf("archived session %q still appears in default list", sess.ID)
		}
	}

	// With IncludeArchived: true it must appear.
	manifests, err = store.ListFiltered(SessionFilter{IncludeArchived: true})
	if err != nil {
		t.Fatalf("ListFiltered(include_archived): %v", err)
	}
	found = false
	for _, m := range manifests {
		if m.ID == sess.ID {
			found = true
			if m.Status != "archived" {
				t.Errorf("expected status=archived, got %q", m.Status)
			}
		}
	}
	if !found {
		t.Error("archived session not found when IncludeArchived=true")
	}
}

// TestStore_ArchiveSession_StatusIsArchived confirms that ArchiveSession sets
// the manifest status to "archived" and the session can still be loaded.
func TestStore_ArchiveSession_StatusIsArchived(t *testing.T) {
	store := NewStore(t.TempDir())

	sess := store.New("to archive", "/workspace", "claude")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	if err := store.ArchiveSession(sess.ID); err != nil {
		t.Fatalf("ArchiveSession: %v", err)
	}

	// Load should still work.
	loaded, err := store.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load after archive: %v", err)
	}
	if loaded.Manifest.Status != "archived" {
		t.Errorf("expected status=archived after ArchiveSession, got %q", loaded.Manifest.Status)
	}
}

// TestStore_ListFiltered_ZeroValueExcludesArchived confirms that the zero value
// of SessionFilter (IncludeArchived: false) is the safe default.
func TestStore_ListFiltered_ZeroValueExcludesArchived(t *testing.T) {
	store := NewStore(t.TempDir())

	active := store.New("active session", "/workspace", "claude")
	if err := store.SaveManifest(active); err != nil {
		t.Fatalf("SaveManifest active: %v", err)
	}

	archived := store.New("archived session", "/workspace", "claude")
	if err := store.SaveManifest(archived); err != nil {
		t.Fatalf("SaveManifest archived: %v", err)
	}
	if err := store.ArchiveSession(archived.ID); err != nil {
		t.Fatalf("ArchiveSession: %v", err)
	}

	// Zero-value filter must exclude the archived session.
	var filter SessionFilter // zero value: IncludeArchived = false
	manifests, err := store.ListFiltered(filter)
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	for _, m := range manifests {
		if m.ID == archived.ID {
			t.Errorf("archived session %q appeared in zero-value-filter list", archived.ID)
		}
	}
	found := false
	for _, m := range manifests {
		if m.ID == active.ID {
			found = true
		}
	}
	if !found {
		t.Error("active session should appear in zero-value-filter list")
	}
}

// TestStore_Delete_PermanentlyRemoves confirms that Delete() still removes the
// session entirely (for backward compatibility / ?permanent=true path).
func TestStore_Delete_PermanentlyRemoves(t *testing.T) {
	store := NewStore(t.TempDir())

	sess := store.New("to delete", "/workspace", "claude")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	if err := store.Delete(sess.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if store.Exists(sess.ID) {
		t.Error("session should not exist after Delete()")
	}
}
