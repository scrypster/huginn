package session_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/session"
)

func TestManifestRoutineFields(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(dir)
	sess := store.New("standup run", "/tmp", "claude-3")
	sess.Manifest.Source = "routine"
	sess.Manifest.RoutineID = "01ROUTINEID"
	sess.Manifest.RunID = "01RUNID"

	if err := store.SaveManifest(sess); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Manifest.Source != "routine" {
		t.Errorf("want source=routine, got %q", loaded.Manifest.Source)
	}
	if loaded.Manifest.RoutineID != "01ROUTINEID" {
		t.Errorf("want routine_id=01ROUTINEID, got %q", loaded.Manifest.RoutineID)
	}
	if loaded.Manifest.RunID != "01RUNID" {
		t.Errorf("want run_id=01RUNID, got %q", loaded.Manifest.RunID)
	}
}
