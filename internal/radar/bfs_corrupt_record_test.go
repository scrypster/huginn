package radar

import (
	"testing"

	"github.com/cockroachdb/pebble/v2"
)

// TestComputeImpact_CorruptRecord_Truncated verifies that a corrupt import record
// causes BFS to return partial results with Truncated=true rather than a hard error.
//
// Bug: on json.Unmarshal failure in getImportRecord(), getImportedBy() returns
// an error that ComputeImpact propagates as a hard failure (nil, error). Callers
// lose all BFS results for the session.
//
// Fix: treat JSON decode errors in getImportedBy() like pebble.ErrNotFound —
// skip the corrupt node with Truncated=true rather than aborting the entire BFS.
func TestComputeImpact_CorruptRecord_Truncated(t *testing.T) {
	db := openTestDB(t)

	repoID := "repo1"
	sha := "abc123"

	// Write a valid import record for "a.go" → imported by "b.go".
	validRec := ImportRecord{ImportedBy: []string{"b.go"}}
	validData, err := jsonMarshal(validRec)
	if err != nil {
		t.Fatalf("marshal valid record: %v", err)
	}
	if err := db.Set(impKey(repoID, sha, "a.go"), validData, pebble.Sync); err != nil {
		t.Fatalf("set valid record: %v", err)
	}

	// Write a CORRUPT import record for "b.go".
	corruptData := []byte("{not valid json!!!}")
	if err := db.Set(impKey(repoID, sha, "b.go"), corruptData, pebble.Sync); err != nil {
		t.Fatalf("set corrupt record: %v", err)
	}

	// BFS from "a.go": should reach "b.go" (valid), then try "b.go"'s importers
	// (corrupt record). Must NOT return error — must return partial with Truncated.
	result, err := ComputeImpact(db, repoID, sha, []string{"a.go"})
	if err != nil {
		t.Fatalf("ComputeImpact returned error on corrupt record: %v\n"+
			"Expected: partial results with Truncated=true, not a hard failure", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result, got nil")
	}
	// "a.go" and "b.go" should both appear in results.
	if result.NodesVisited < 2 {
		t.Errorf("expected at least 2 nodes visited (a.go + b.go), got %d", result.NodesVisited)
	}
	// Truncated must be true because a corrupt record was encountered.
	if !result.Truncated {
		t.Errorf("expected Truncated=true when corrupt record encountered, got false")
	}
	t.Logf("result: visited=%d truncated=%v", result.NodesVisited, result.Truncated)
}

// TestComputeImpact_AllCorruptRecords_NoPanic verifies BFS with only corrupt
// records returns an empty non-panicking result.
func TestComputeImpact_AllCorruptRecords_NoPanic(t *testing.T) {
	db := openTestDB(t)

	repoID := "repo1"
	sha := "abc"

	// Write corrupt record for the seed file.
	if err := db.Set(impKey(repoID, sha, "main.go"), []byte("!!!"), pebble.Sync); err != nil {
		t.Fatalf("set: %v", err)
	}

	result, err := ComputeImpact(db, repoID, sha, []string{"main.go"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// main.go itself should be in visited (seed always added before fetching imports).
	if result.NodesVisited == 0 {
		t.Errorf("expected at least seed node visited, got 0")
	}
}
