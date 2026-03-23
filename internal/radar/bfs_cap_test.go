package radar

import (
	"errors"
	"fmt"
	"testing"

	"github.com/cockroachdb/pebble/v2"
)

// TestBFS_MaxVisitedCap_StopsGracefully creates a graph with far more than
// BFSMaxVisited (10,000) reachable nodes and verifies that ComputeImpact
// terminates promptly and returns ErrBFSLimitExceeded.
//
// Graph structure (fan-out to exceed the depth limit of 4):
//
//	Depth 0: seed (1 node)
//	Depth 1: 200 nodes import seed
//	Depth 2: 80 nodes import each depth-1 node → 200*80 = 16,000 > 10,000
func TestBFS_MaxVisitedCap_StopsGracefully(t *testing.T) {
	db := openTestDB(t)
	repoID := "repo-cap"
	sha := "sha-cap"

	const (
		fan1Size = 200
		fan2Size = 80
	)

	// Depth 1: fan1_0 … fan1_199 import "seed.go"
	fan1 := make([]string, fan1Size)
	for i := 0; i < fan1Size; i++ {
		fan1[i] = fmt.Sprintf("cap_fan1_%d.go", i)
	}
	writeImportRecord(t, db, repoID, sha, "seed.go", ImportRecord{ImportedBy: fan1})

	// Depth 2: each fan1 node is imported by fan2Size nodes.
	batch := db.NewBatch()
	for i := 0; i < fan1Size; i++ {
		fan2 := make([]string, fan2Size)
		for j := 0; j < fan2Size; j++ {
			fan2[j] = fmt.Sprintf("cap_fan2_%d_%d.go", i, j)
		}
		rec := ImportRecord{ImportedBy: fan2}
		val := mustMarshal(t, rec)
		key := impKey(repoID, sha, fan1[i])
		if err := batch.Set(key, val, pebble.Sync); err != nil {
			t.Fatalf("batch.Set: %v", err)
		}
	}
	if err := batch.Commit(pebble.Sync); err != nil {
		t.Fatalf("batch.Commit: %v", err)
	}

	// Run BFS starting from seed.go — total reachable is 1 + 200 + 16,000 > 10,000.
	result, err := ComputeImpact(db, repoID, sha, []string{"seed.go"})
	if err != nil {
		t.Fatalf("ComputeImpact returned fatal error: %v", err)
	}

	// Must set Truncated=true and Err=ErrBFSLimitExceeded.
	if !result.Truncated {
		t.Error("expected result.Truncated=true for oversized graph")
	}
	if !errors.Is(result.Err, ErrBFSLimitExceeded) {
		t.Errorf("expected ErrBFSLimitExceeded, got: %v", result.Err)
	}

	// NodesVisited must not exceed the cap.
	if result.NodesVisited > BFSMaxVisited {
		t.Errorf("NodesVisited=%d exceeds cap BFSMaxVisited=%d", result.NodesVisited, BFSMaxVisited)
	}
}
