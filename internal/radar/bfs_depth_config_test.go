package radar

import (
	"encoding/json"
	"testing"

	"github.com/cockroachdb/pebble/v2"
)

// buildLinearChainDB creates a Pebble DB in a temp dir and populates it with a
// simple linear import chain: A → B → C → D → E (each node imports the next).
//
// Returns the db and a cleanup function.
func buildLinearChainDB(t *testing.T) (*pebble.DB, func()) {
	t.Helper()
	dir := t.TempDir()
	db, err := pebble.Open(dir, &pebble.Options{})
	if err != nil {
		t.Fatalf("pebble.Open: %v", err)
	}
	// Chain: A is imported by B, B by C, C by D, D by E.
	// So getImportedBy("A") = ["B"], getImportedBy("B") = ["C"], etc.
	chain := []struct{ node, importedBy string }{
		{"A", "B"},
		{"B", "C"},
		{"C", "D"},
		{"D", "E"},
	}
	batch := db.NewBatch()
	for _, link := range chain {
		rec := ImportRecord{ImportedBy: []string{link.importedBy}}
		val, _ := json.Marshal(rec)
		key := impKey("repo1", "sha1", link.node)
		if err := batch.Set(key, val, pebble.Sync); err != nil {
			t.Fatalf("batch.Set: %v", err)
		}
	}
	if err := batch.Commit(pebble.Sync); err != nil {
		t.Fatalf("batch.Commit: %v", err)
	}
	return db, func() { db.Close() }
}

// TestComputeImpact_DefaultDepth verifies the original ComputeImpact wrapper
// still works and honours BFSDefaultMaxDepth (4).
func TestComputeImpact_DefaultDepth(t *testing.T) {
	db, cleanup := buildLinearChainDB(t)
	defer cleanup()

	res, err := ComputeImpact(db, "repo1", "sha1", []string{"A"})
	if err != nil {
		t.Fatalf("ComputeImpact: %v", err)
	}

	// With depth 4 from seed A: A(0) B(1) C(2) D(3) E(4) — but E is only
	// reached at distance 4 which equals BFSDefaultMaxDepth, so E should NOT
	// be expanded further. E itself is enqueued but its distance equals depth
	// limit so it's not followed. Let's verify A..E are all present.
	paths := make(map[string]int)
	for _, n := range res.Impacted {
		paths[n.Path] = n.Distance
	}
	for _, expected := range []string{"A", "B", "C", "D", "E"} {
		if _, ok := paths[expected]; !ok {
			t.Errorf("expected node %q in impact result", expected)
		}
	}
}

// TestComputeImpactWithConfig_ShallowDepth verifies that setting MaxDepth=1
// limits traversal to one hop from the seed.
func TestComputeImpactWithConfig_ShallowDepth(t *testing.T) {
	db, cleanup := buildLinearChainDB(t)
	defer cleanup()

	res, err := ComputeImpactWithConfig(db, "repo1", "sha1", []string{"A"}, BFSConfig{MaxDepth: 1})
	if err != nil {
		t.Fatalf("ComputeImpactWithConfig: %v", err)
	}

	paths := make(map[string]bool)
	for _, n := range res.Impacted {
		paths[n.Path] = true
	}

	// At depth 1: seed A(0) and its direct importer B(1).
	if !paths["A"] {
		t.Error("A (seed) should be present")
	}
	if !paths["B"] {
		t.Error("B (distance 1) should be present")
	}
	// C, D, E should NOT be present (distance > 1).
	for _, absent := range []string{"C", "D", "E"} {
		if paths[absent] {
			t.Errorf("%q should not be present at MaxDepth=1", absent)
		}
	}
}

// TestComputeImpactWithConfig_ZeroUsesDefault verifies that MaxDepth=0 falls
// back to BFSDefaultMaxDepth.
func TestComputeImpactWithConfig_ZeroUsesDefault(t *testing.T) {
	db, cleanup := buildLinearChainDB(t)
	defer cleanup()

	resDefault, err := ComputeImpact(db, "repo1", "sha1", []string{"A"})
	if err != nil {
		t.Fatalf("ComputeImpact default: %v", err)
	}
	resZero, err := ComputeImpactWithConfig(db, "repo1", "sha1", []string{"A"}, BFSConfig{MaxDepth: 0})
	if err != nil {
		t.Fatalf("ComputeImpactWithConfig MaxDepth=0: %v", err)
	}

	if resDefault.NodesVisited != resZero.NodesVisited {
		t.Errorf("MaxDepth=0 should equal default: default=%d zero=%d",
			resDefault.NodesVisited, resZero.NodesVisited)
	}
}

// TestComputeImpactWithConfig_MaxVisitedCap verifies that MaxVisited limits
// the total nodes visited and sets Truncated=true.
func TestComputeImpactWithConfig_MaxVisitedCap(t *testing.T) {
	db, cleanup := buildLinearChainDB(t)
	defer cleanup()

	// Chain has A,B,C,D,E = 5 nodes. Cap at 3.
	res, err := ComputeImpactWithConfig(db, "repo1", "sha1", []string{"A"}, BFSConfig{MaxVisited: 3})
	if err != nil {
		t.Fatalf("ComputeImpactWithConfig: %v", err)
	}
	if res.NodesVisited > 3 {
		t.Errorf("expected ≤3 nodes visited, got %d", res.NodesVisited)
	}
	if !res.Truncated {
		t.Error("expected Truncated=true when MaxVisited cap is hit")
	}
}

// TestBFSConfig_DefaultValues verifies the constants match expectations.
func TestBFSConfig_DefaultValues(t *testing.T) {
	if BFSDefaultMaxDepth != 4 {
		t.Errorf("BFSDefaultMaxDepth: expected 4, got %d", BFSDefaultMaxDepth)
	}
	if BFSMaxDepth != BFSDefaultMaxDepth {
		t.Errorf("BFSMaxDepth should equal BFSDefaultMaxDepth")
	}
	if BFSMaxVisited != 2000 {
		t.Errorf("BFSMaxVisited: expected 2000, got %d", BFSMaxVisited)
	}
}
