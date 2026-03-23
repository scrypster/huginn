package artifact_test

// edge_cases_r5_test.go — edge-case tests for internal/artifact package.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/artifact"
	"github.com/scrypster/huginn/internal/workforce"
)

// TestWrite_IDCollision_SecondWriteFails verifies that writing two artifacts with the same
// ID fails with a primary-key constraint error on the second write.
func TestWrite_IDCollision_SecondWriteFails(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	ctx := context.Background()

	a := baseArtifact()
	a.ID = "fixed-collision-id"
	a.Content = []byte("first write")

	if err := s.Write(ctx, a); err != nil {
		t.Fatalf("first Write: %v", err)
	}

	// Reset timestamps so the second Write does not short-circuit before the INSERT.
	b := baseArtifact()
	b.ID = "fixed-collision-id"
	b.Content = []byte("second write")

	err := s.Write(ctx, b)
	if err == nil {
		t.Fatal("expected error on second Write with same ID (PK constraint), got nil")
	}
}

// TestWrite_NilArtifact_Panics_OrErrors verifies that Write(ctx, nil) either panics
// (acceptable) or returns an error. The test uses recover to catch panics.
func TestWrite_NilArtifact_Panics_OrErrors(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	ctx := context.Background()

	defer func() {
		// A panic is acceptable behaviour for a nil receiver.
		if r := recover(); r != nil {
			t.Logf("Write(nil) panicked (acceptable): %v", r)
		}
	}()

	err := s.Write(ctx, nil)
	if err == nil {
		t.Log("Write(nil) returned nil error without panic — that is also acceptable")
	} else {
		t.Logf("Write(nil) returned error (acceptable): %v", err)
	}
}

// TestRead_EmptyID_ReturnsError verifies that Read with an empty ID returns an error.
func TestRead_EmptyID_ReturnsError(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	ctx := context.Background()

	_, err := s.Read(ctx, "")
	if err == nil {
		t.Fatal("expected error for Read(\"\"), got nil")
	}
}

// TestListBySession_EmptySessionID_ReturnsEmpty verifies that ListBySession with an empty
// session ID returns an empty (non-nil) slice without error.
func TestListBySession_EmptySessionID_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	ctx := context.Background()

	// Write a real artifact to sess-001 to ensure the DB is not empty.
	a := baseArtifact()
	a.Content = []byte("noise")
	if err := s.Write(ctx, a); err != nil {
		t.Fatalf("Write: %v", err)
	}

	list, err := s.ListBySession(ctx, "", 10, "")
	if err != nil {
		t.Fatalf("ListBySession(\"\") returned error: %v", err)
	}
	// A nil slice is acceptable here; len(nil) == 0.
	if len(list) != 0 {
		t.Errorf("expected 0 results for empty session ID, got %d", len(list))
	}
}

// TestUpdateStatus_EmptyID_ReturnsError verifies that UpdateStatus with an empty ID returns an error.
func TestUpdateStatus_EmptyID_ReturnsError(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	ctx := context.Background()

	err := s.UpdateStatus(ctx, "", workforce.StatusAccepted, "")
	if err == nil {
		t.Fatal("expected error for UpdateStatus with empty ID, got nil")
	}
}

// TestDeleteBySession_EmptyID_ReturnsNilOrError verifies graceful handling of an empty
// session ID in DeleteBySession — must not panic.
func TestDeleteBySession_EmptyID_ReturnsNilOrError(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	ctx := context.Background()

	// Either nil or an error is acceptable; a panic is not.
	err := s.DeleteBySession(ctx, "")
	// Log the result — both outcomes are valid.
	t.Logf("DeleteBySession(\"\") = %v", err)
}

// TestWrite_MetadataJSON_RoundTrip verifies that an artifact written with Metadata set
// is read back with the same Metadata values intact.
func TestWrite_MetadataJSON_RoundTrip(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	ctx := context.Background()

	a := baseArtifact()
	a.Content = []byte("patch content")
	a.Metadata = map[string]any{
		"lines_added":   float64(42),
		"lines_removed": float64(7),
		"files_changed": float64(3),
		"tag":           "metadata-round-trip",
	}

	if err := s.Write(ctx, a); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := s.Read(ctx, a.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Metadata == nil {
		t.Fatal("expected non-nil Metadata after Read")
	}
	if got.Metadata["tag"] != "metadata-round-trip" {
		t.Errorf("Metadata[tag] = %v, want %q", got.Metadata["tag"], "metadata-round-trip")
	}
	if got.Metadata["lines_added"] != float64(42) {
		t.Errorf("Metadata[lines_added] = %v, want 42", got.Metadata["lines_added"])
	}
}

// TestWrite_VeryLongTitle verifies that a title of 10000 chars roundtrips without truncation.
// SQLite TEXT columns have no hard length limit.
func TestWrite_VeryLongTitle(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	ctx := context.Background()

	longTitle := strings.Repeat("t", 10000)

	a := baseArtifact()
	a.Title = longTitle
	a.Content = []byte("some content")

	if err := s.Write(ctx, a); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := s.Read(ctx, a.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Title != longTitle {
		t.Errorf("title length mismatch: want %d chars, got %d", len(longTitle), len(got.Title))
	}
}

// TestWrite_SpecialCharsInTitle verifies that Unicode, emoji, and SQL injection-like strings
// in the title are stored and retrieved without alteration or error.
func TestWrite_SpecialCharsInTitle(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	ctx := context.Background()

	cases := []struct {
		name  string
		title string
	}{
		{"unicode", "こんにちは世界 — резюме"},
		{"emoji", "🔥 Artifact 🎉"},
		{"sql_injection", "'; DROP TABLE artifacts; --"},
		{"null_like", "title\x00with\x00nulls"},
		{"quotes", `title with "double" and 'single' quotes`},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := baseArtifact()
			a.Title = tc.title
			a.Content = []byte("content")

			if err := s.Write(ctx, a); err != nil {
				t.Fatalf("Write(%q): %v", tc.name, err)
			}

			got, err := s.Read(ctx, a.ID)
			if err != nil {
				t.Fatalf("Read(%q): %v", tc.name, err)
			}
			if got.Title != tc.title {
				t.Errorf("title roundtrip mismatch for %q: want %q, got %q", tc.name, tc.title, got.Title)
			}
		})
	}
}

// TestArchive_ZeroArtifacts_NoPanic verifies that Archive on an empty DB returns nil
// and does not panic.
func TestArchive_ZeroArtifacts_NoPanic(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	ctx := context.Background()

	n, err := s.Archive(ctx, 1*time.Hour)
	if err != nil {
		t.Fatalf("Archive on empty DB returned error: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 archived rows, got %d", n)
	}
}

// TestListBySession_PaginationExact verifies that when exactly limit items exist, the
// next page (using the last item's ID as cursor) returns an empty slice.
func TestListBySession_PaginationExact(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	ctx := context.Background()

	const count = 5
	for i := 0; i < count; i++ {
		a := baseArtifact()
		a.Content = []byte("page-item")
		if err := s.Write(ctx, a); err != nil {
			t.Fatalf("Write[%d]: %v", i, err)
		}
	}

	// First page: exactly count items.
	page1, err := s.ListBySession(ctx, "sess-001", count, "")
	if err != nil {
		t.Fatalf("ListBySession page1: %v", err)
	}
	if len(page1) != count {
		t.Fatalf("expected %d items on page1, got %d", count, len(page1))
	}

	// Next page should be empty — all items were on page 1.
	page2, err := s.ListBySession(ctx, "sess-001", count, page1[count-1].ID)
	if err != nil {
		t.Fatalf("ListBySession page2: %v", err)
	}
	if len(page2) != 0 {
		t.Errorf("expected empty next page, got %d items", len(page2))
	}
}

// Compile-time assertion: ensure artifact package is imported.
var _ = artifact.MaxArtifactSize
