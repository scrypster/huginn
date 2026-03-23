package radar

import (
	"fmt"
	"testing"

	"github.com/cockroachdb/pebble/v2"
)

// ---------------------------------------------------------------------------
// loadBaseline / loadPolicy error handling
// ---------------------------------------------------------------------------

func TestLoadBaseline_ErrNotFound_ReturnsSilent(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	bg, err := loadBaseline(db, "repo1", "main")
	if bg != nil {
		t.Errorf("expected nil baseline, got %+v", bg)
	}
	if err != pebble.ErrNotFound {
		t.Errorf("expected pebble.ErrNotFound, got %v", err)
	}
}

func TestLoadPolicy_ErrNotFound_ReturnsSilent(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	bp, err := loadPolicy(db, "repo1", "main")
	if bp != nil {
		t.Errorf("expected nil policy, got %+v", bp)
	}
	if err != pebble.ErrNotFound {
		t.Errorf("expected pebble.ErrNotFound, got %v", err)
	}
}

func TestLoadBaseline_CorruptData_ReturnsError(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	// Write corrupt data at the baseline key
	key := baselineGraphKey("repo1", "main")
	if err := db.Set(key, []byte("not json"), pebble.Sync); err != nil {
		t.Fatalf("Set: %v", err)
	}

	bg, err := loadBaseline(db, "repo1", "main")
	if err == nil {
		t.Fatal("expected error for corrupt data")
	}
	if bg != nil {
		t.Errorf("expected nil on error, got %+v", bg)
	}
}

func TestLoadPolicy_CorruptData_ReturnsError(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	key := baselinePolicyKey("repo1", "main")
	if err := db.Set(key, []byte("{broken"), pebble.Sync); err != nil {
		t.Fatalf("Set: %v", err)
	}

	bp, err := loadPolicy(db, "repo1", "main")
	if err == nil {
		t.Fatal("expected error for corrupt data")
	}
	if bp != nil {
		t.Errorf("expected nil on error, got %+v", bp)
	}
}

// ---------------------------------------------------------------------------
// Pathological graph topologies for BFS
// ---------------------------------------------------------------------------

func TestComputeImpact_StarTopology(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	repoID := "star-test"
	sha := "sha-star"
	center := "hub.go"
	leaves := make([]string, 100)
	for i := range leaves {
		leaves[i] = fmt.Sprintf("leaf_%d.go", i)
	}

	// Each leaf imports the center, so center's ImportedBy includes all leaves
	centerRec := ImportRecord{
		Imports:    []string{},
		ImportedBy: leaves,
	}
	writeImportRecord(t, db, repoID, sha, center, centerRec)

	// Each leaf has no importers
	for _, leaf := range leaves {
		leafRec := ImportRecord{Imports: []string{center}, ImportedBy: []string{}}
		writeImportRecord(t, db, repoID, sha, leaf, leafRec)
	}

	result, err := ComputeImpact(db, repoID, sha, []string{center})
	if err != nil {
		t.Fatalf("ComputeImpact: %v", err)
	}

	// Should reach center + all 100 leaves = 101
	if result.NodesVisited != 101 {
		t.Errorf("star topology: visited %d, want 101", result.NodesVisited)
	}
}

func TestComputeImpact_LongChain(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	repoID := "chain-test"
	sha := "sha-chain"
	const chainLen = 100
	nodes := make([]string, chainLen)
	for i := range nodes {
		nodes[i] = fmt.Sprintf("node_%d.go", i)
	}

	// node_0 is imported by node_1, node_1 by node_2, etc.
	for i := 0; i < chainLen; i++ {
		var importedBy []string
		if i+1 < chainLen {
			importedBy = []string{nodes[i+1]}
		}
		var imports []string
		if i > 0 {
			imports = []string{nodes[i-1]}
		}
		rec := ImportRecord{Imports: imports, ImportedBy: importedBy}
		writeImportRecord(t, db, repoID, sha, nodes[i], rec)
	}

	result, err := ComputeImpact(db, repoID, sha, []string{nodes[0]})
	if err != nil {
		t.Fatalf("ComputeImpact: %v", err)
	}

	// BFS should be bounded by BFSMaxDepth (4) + seed node = 5
	expectedMax := BFSMaxDepth + 1
	if result.NodesVisited > expectedMax {
		t.Errorf("chain: visited %d nodes, expected at most %d (BFSMaxDepth+1)", result.NodesVisited, expectedMax)
	}
	if result.NodesVisited < 2 {
		t.Error("chain: should visit at least seed + 1 neighbor")
	}
}

func TestComputeImpact_CompleteGraph(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	repoID := "complete-test"
	sha := "sha-complete"
	const size = 5
	nodes := make([]string, size)
	for i := range nodes {
		nodes[i] = fmt.Sprintf("node_%d.go", i)
	}

	// Every node imports every other node (complete graph)
	for i := 0; i < size; i++ {
		var imports, importedBy []string
		for j := 0; j < size; j++ {
			if i != j {
				imports = append(imports, nodes[j])
				importedBy = append(importedBy, nodes[j])
			}
		}
		rec := ImportRecord{Imports: imports, ImportedBy: importedBy}
		writeImportRecord(t, db, repoID, sha, nodes[i], rec)
	}

	result, err := ComputeImpact(db, repoID, sha, []string{nodes[0]})
	if err != nil {
		t.Fatalf("ComputeImpact: %v", err)
	}

	// All 5 nodes should be reached
	if result.NodesVisited != size {
		t.Errorf("complete graph: visited %d, want %d", result.NodesVisited, size)
	}
}

// ---------------------------------------------------------------------------
// DetectDrift with ErrNotFound baseline/policy
// ---------------------------------------------------------------------------

func TestDetectDrift_NoBaselineNoPolicy(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	repoID := "drift-empty"
	sha := "sha1"
	branch := "main"

	// No baseline or policy in DB, no edges either
	result, err := DetectDrift(db, repoID, sha, branch, []string{"file.go"})
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// ---------------------------------------------------------------------------
// Helpers (reuse openTestDB from radar_test.go)
// ---------------------------------------------------------------------------

func TestDetectDrift_WithEdgesAndNoBaseline(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	repoID := "drift-edges"
	sha := "sha2"
	branch := "main"

	writeEdge(t, db, repoID, sha, "cmd/main.go", "internal/svc.go")

	result, err := DetectDrift(db, repoID, sha, branch, []string{"cmd/main.go"})
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	// With no baseline, all edges are "new"
	if len(result.NewEdges) != 1 {
		t.Errorf("expected 1 new edge, got %d", len(result.NewEdges))
	}
}


