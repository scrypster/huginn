package session_test

import "testing"

func TestSaveManifestRoundTripsExternalFields(t *testing.T) {
	s := newTestSQLiteStore(t)

	sess := s.New("claude session", "/tmp/proj", "claude-opus-5")
	sess.Manifest.ExternalKind = "claude-code"
	sess.Manifest.ExternalID = "11111111-2222-3333-4444-555555555555"
	if err := s.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	got, err := s.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Manifest.ExternalKind != "claude-code" {
		t.Errorf("ExternalKind = %q, want %q", got.Manifest.ExternalKind, "claude-code")
	}
	if got.Manifest.ExternalID != sess.Manifest.ExternalID {
		t.Errorf("ExternalID = %q, want %q", got.Manifest.ExternalID, sess.Manifest.ExternalID)
	}
}

func TestLoadByExternalID(t *testing.T) {
	s := newTestSQLiteStore(t)

	sess := s.New("claude session", "/tmp/proj", "claude-opus-5")
	sess.Manifest.ExternalKind = "claude-code"
	sess.Manifest.ExternalID = "abc-123"
	if err := s.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	got, err := s.LoadByExternalID("claude-code", "abc-123")
	if err != nil {
		t.Fatalf("LoadByExternalID: %v", err)
	}
	if got == nil || got.ID != sess.ID {
		t.Fatalf("LoadByExternalID returned %v, want session %s", got, sess.ID)
	}

	missing, err := s.LoadByExternalID("claude-code", "nope")
	if err != nil {
		t.Fatalf("LoadByExternalID(missing): unexpected error %v", err)
	}
	if missing != nil {
		t.Errorf("LoadByExternalID(missing) = %v, want nil", missing)
	}
}

func TestSourceCheckConstraintUntouched(t *testing.T) {
	s := newTestSQLiteStore(t)
	sess := s.New("routine session", "/tmp/proj", "m")
	sess.Manifest.Source = "routine"
	if err := s.SaveManifest(sess); err != nil {
		t.Fatalf("routine source must still be accepted: %v", err)
	}
}
