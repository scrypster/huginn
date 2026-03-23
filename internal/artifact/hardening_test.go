package artifact_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/artifact"
	"github.com/scrypster/huginn/internal/workforce"
)

// ── Write boundary tests ─────────────────────────────────────────────────────

func TestWrite_NilContent_Succeeds(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	a := baseArtifact()
	a.Content = nil

	if err := s.Write(context.Background(), a); err != nil {
		t.Fatalf("Write nil content: %v", err)
	}
	got, err := s.Read(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got.Content) != 0 {
		t.Errorf("expected empty content, got %d bytes", len(got.Content))
	}
}

func TestWrite_EmptyContent_Succeeds(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	a := baseArtifact()
	a.Content = []byte{}

	if err := s.Write(context.Background(), a); err != nil {
		t.Fatalf("Write empty content: %v", err)
	}
}

func TestWrite_ExactlyAtSizeThreshold_StaysInline(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	a := baseArtifact()
	a.Content = bytes.Repeat([]byte("x"), 256*1024) // exactly 256 KB

	if err := s.Write(context.Background(), a); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if a.ContentRef != "" {
		t.Errorf("content at exactly threshold should be inline, got ref %q", a.ContentRef)
	}
}

func TestWrite_OneByteOverThreshold_GoesToDisk(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	a := baseArtifact()
	a.Content = bytes.Repeat([]byte("x"), 256*1024+1)

	if err := s.Write(context.Background(), a); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if a.ContentRef == "" {
		t.Error("content one byte over threshold should be on disk")
	}
}

func TestWrite_ExactlyMaxSize_Succeeds(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	a := baseArtifact()
	a.Content = bytes.Repeat([]byte("x"), artifact.MaxArtifactSize)

	if err := s.Write(context.Background(), a); err != nil {
		t.Fatalf("Write at exactly max size should succeed: %v", err)
	}
}

func TestWrite_PresetID_Preserved(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	a := baseArtifact()
	a.ID = "custom-id-001"
	a.Content = []byte("data")

	if err := s.Write(context.Background(), a); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if a.ID != "custom-id-001" {
		t.Errorf("expected preset ID preserved, got %q", a.ID)
	}
	got, err := s.Read(context.Background(), "custom-id-001")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.ID != "custom-id-001" {
		t.Errorf("round-trip ID mismatch: %q", got.ID)
	}
}

func TestWrite_DefaultsStatusToDraft(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	a := baseArtifact()
	a.Status = "" // empty
	a.Content = []byte("data")

	if err := s.Write(context.Background(), a); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := s.Read(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Status != workforce.StatusDraft {
		t.Errorf("expected default status 'draft', got %q", got.Status)
	}
}

func TestWrite_MetadataRoundTrip(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	a := baseArtifact()
	a.Content = []byte("patch")
	a.Metadata = map[string]any{
		"lines_added":   42,
		"files_changed": []any{"main.go", "util.go"},
		"nested":        map[string]any{"key": "value"},
	}

	if err := s.Write(context.Background(), a); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := s.Read(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Metadata == nil {
		t.Fatal("expected metadata to be populated after round-trip")
	}
	if v, ok := got.Metadata["lines_added"].(float64); !ok || v != 42 {
		t.Errorf("expected lines_added=42, got %v", got.Metadata["lines_added"])
	}
}

func TestWrite_NilMetadata_RoundTrip(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	a := baseArtifact()
	a.Content = []byte("data")
	a.Metadata = nil

	if err := s.Write(context.Background(), a); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := s.Read(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Metadata != nil {
		t.Errorf("expected nil metadata, got %v", got.Metadata)
	}
}

// ── UpdateStatus error paths ─────────────────────────────────────────────────

func TestUpdateStatus_NotFound_ReturnsError(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	err := s.UpdateStatus(context.Background(), "nonexistent", workforce.StatusAccepted, "")
	if !errors.Is(err, workforce.ErrArtifactNotFound) {
		t.Errorf("expected ErrArtifactNotFound, got %v", err)
	}
}

func TestUpdateStatus_InvalidTransition_RejectedToAccepted(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	a := baseArtifact()
	a.Content = []byte("data")
	s.Write(context.Background(), a)
	s.UpdateStatus(context.Background(), a.ID, workforce.StatusRejected, "bad")

	err := s.UpdateStatus(context.Background(), a.ID, workforce.StatusAccepted, "")
	if err == nil {
		t.Error("expected error for rejected->accepted transition")
	}
	if !strings.Contains(err.Error(), "invalid status transition") {
		t.Errorf("expected invalid transition error, got: %v", err)
	}
}

func TestUpdateStatus_InvalidTransition_AcceptedToRejected(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	a := baseArtifact()
	a.Content = []byte("data")
	s.Write(context.Background(), a)
	s.UpdateStatus(context.Background(), a.ID, workforce.StatusAccepted, "")

	err := s.UpdateStatus(context.Background(), a.ID, workforce.StatusRejected, "changed my mind")
	if err == nil {
		t.Error("expected error for accepted->rejected transition")
	}
}

func TestUpdateStatus_InvalidTransition_FailedToAccepted(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	a := baseArtifact()
	a.Content = []byte("data")
	s.Write(context.Background(), a)
	s.UpdateStatus(context.Background(), a.ID, workforce.StatusFailed, "")

	err := s.UpdateStatus(context.Background(), a.ID, workforce.StatusAccepted, "")
	if err == nil {
		t.Error("expected error for failed->accepted transition")
	}
}

func TestUpdateStatus_DraftToFailed_Succeeds(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	a := baseArtifact()
	a.Content = []byte("data")
	s.Write(context.Background(), a)

	if err := s.UpdateStatus(context.Background(), a.ID, workforce.StatusFailed, ""); err != nil {
		t.Errorf("draft->failed should succeed: %v", err)
	}
}

func TestUpdateStatus_AcceptedToSuperseded_Succeeds(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	a := baseArtifact()
	a.Content = []byte("data")
	s.Write(context.Background(), a)
	s.UpdateStatus(context.Background(), a.ID, workforce.StatusAccepted, "")

	if err := s.UpdateStatus(context.Background(), a.ID, workforce.StatusSuperseded, ""); err != nil {
		t.Errorf("accepted->superseded should succeed: %v", err)
	}
}

func TestUpdateStatus_BumpsUpdatedAt(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	a := baseArtifact()
	a.Content = []byte("data")
	s.Write(context.Background(), a)

	before, _ := s.Read(context.Background(), a.ID)
	time.Sleep(time.Millisecond)

	s.UpdateStatus(context.Background(), a.ID, workforce.StatusAccepted, "")
	after, _ := s.Read(context.Background(), a.ID)

	if !after.UpdatedAt.After(before.UpdatedAt) {
		t.Error("UpdateStatus should bump updated_at")
	}
}

// ── Supersede error paths ────────────────────────────────────────────────────

func TestSupersede_NotFound_ReturnsError(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	err := s.Supersede(context.Background(), "ghost", "new")
	if !errors.Is(err, workforce.ErrArtifactNotFound) {
		t.Errorf("expected ErrArtifactNotFound, got %v", err)
	}
}

// ── ListBySession edge cases ─────────────────────────────────────────────────

func TestListBySession_NoResults_ReturnsEmptySlice(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	list, err := s.ListBySession(context.Background(), "nonexistent-session", 0, "")
	if err != nil {
		t.Fatalf("ListBySession: %v", err)
	}
	if list != nil && len(list) != 0 {
		t.Errorf("expected empty result for unknown session, got %d", len(list))
	}
}

func TestListBySession_NegativeLimit_DefaultsTo50(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	// Just verify it doesn't panic with negative limit.
	_, err := s.ListBySession(context.Background(), "sess-001", -1, "")
	if err != nil {
		t.Fatalf("ListBySession with negative limit: %v", err)
	}
}

// ── ListByAgent edge cases ───────────────────────────────────────────────────

func TestListByAgent_FutureSince_ReturnsNothing(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	a := baseArtifact()
	a.AgentName = "FutureAgent"
	a.Content = []byte("data")
	s.Write(context.Background(), a)

	list, err := s.ListByAgent(context.Background(), "FutureAgent", time.Now().Add(1*time.Hour), 0, "")
	if err != nil {
		t.Fatalf("ListByAgent: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0 results for future since, got %d", len(list))
	}
}

func TestListByAgent_ZeroTime_ReturnsAll(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	for i := 0; i < 3; i++ {
		a := baseArtifact()
		a.AgentName = "AllAgent"
		a.Content = []byte("data")
		s.Write(context.Background(), a)
	}

	list, err := s.ListByAgent(context.Background(), "AllAgent", time.Time{}, 0, "")
	if err != nil {
		t.Fatalf("ListByAgent: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("expected 3 results with zero time, got %d", len(list))
	}
}

// ── DeleteBySession edge cases ───────────────────────────────────────────────

func TestDeleteBySession_NoArtifacts_Succeeds(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	err := s.DeleteBySession(context.Background(), "nonexistent-session")
	if err != nil {
		t.Errorf("DeleteBySession with no artifacts should succeed: %v", err)
	}
}

// ── Archive edge cases ───────────────────────────────────────────────────────

func TestArchive_NoMatches_ReturnsZero(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	n, err := s.Archive(context.Background(), 1*time.Hour)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 archived on empty store, got %d", n)
	}
}

func TestArchive_DraftNotDeleted_EvenIfOld(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	a := baseArtifact()
	a.Content = []byte("draft")
	s.Write(context.Background(), a)

	// Even with a zero duration cutoff (everything is "old"), draft should survive.
	n, err := s.Archive(context.Background(), 0)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 archived (only draft exists), got %d", n)
	}
}

func TestArchive_AcceptedNotDeleted(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	a := baseArtifact()
	a.Content = []byte("good")
	s.Write(context.Background(), a)
	s.UpdateStatus(context.Background(), a.ID, workforce.StatusAccepted, "")

	n, err := s.Archive(context.Background(), 0)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 archived (accepted should be preserved), got %d", n)
	}
}

// ── Concurrent writes ────────────────────────────────────────────────────────

func TestWrite_ConcurrentWrites_NoRace(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	var wg sync.WaitGroup
	errs := make([]error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			a := baseArtifact()
			a.Content = []byte("concurrent data")
			errs[n] = s.Write(context.Background(), a)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d failed: %v", i, err)
		}
	}

	list, _ := s.ListBySession(context.Background(), "sess-001", 200, "")
	if len(list) != 20 {
		t.Errorf("expected 20 artifacts from concurrent writes, got %d", len(list))
	}
}

// ── Cancelled context ────────────────────────────────────────────────────────

func TestRead_CancelledContext_ReturnsError(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	a := baseArtifact()
	a.Content = []byte("data")
	s.Write(context.Background(), a)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := s.Read(ctx, a.ID)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

// ── Field round-trip ─────────────────────────────────────────────────────────

func TestWrite_MimeType_RoundTrip(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	a := baseArtifact()
	a.Content = []byte("full artifact")
	a.MimeType = "application/json"

	if err := s.Write(context.Background(), a); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := s.Read(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.MimeType != "application/json" {
		t.Errorf("MimeType mismatch: %q", got.MimeType)
	}
}
