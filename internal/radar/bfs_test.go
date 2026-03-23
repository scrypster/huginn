package radar

import (
	"testing"
)

// TestComputeImpact_Empty verifies that empty changed files returns 0 nodes.
func TestComputeImpact_Empty(t *testing.T) {
	db := openTestDB(t)
	result, err := ComputeImpact(db, "repo1", "sha1", nil)
	if err != nil {
		t.Fatalf("expected no error for empty changed files: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.NodesVisited != 0 {
		t.Errorf("expected 0 nodes visited, got %d", result.NodesVisited)
	}
	if result.Truncated {
		t.Error("expected Truncated=false for empty input")
	}
}

// TestComputeImpact_DirectDependency verifies A imported by B → B is reachable.
func TestComputeImpact_DirectDependency(t *testing.T) {
	db := openTestDB(t)
	repoID := "repo1"
	sha := "sha1"

	writeImportRecord(t, db, repoID, sha, "a.go", ImportRecord{
		ImportedBy: []string{"b.go"},
	})

	result, err := ComputeImpact(db, repoID, sha, []string{"a.go"})
	if err != nil {
		t.Fatalf("ComputeImpact error: %v", err)
	}

	if result.NodesVisited < 2 {
		t.Errorf("expected at least 2 nodes visited, got %d", result.NodesVisited)
	}

	bFound := false
	for _, node := range result.Impacted {
		if node.Path == "b.go" {
			bFound = true
			if node.Distance != 1 {
				t.Errorf("b.go should have distance 1, got %d", node.Distance)
			}
		}
	}
	if !bFound {
		t.Error("expected b.go in impacted nodes")
	}
}

// TestComputeImpact_TransitiveChain tests A→B→C: modifying A reaches C.
func TestComputeImpact_TransitiveChain(t *testing.T) {
	db := openTestDB(t)
	repoID := "repo1"
	sha := "sha1"

	// c.go imported by b.go; b.go imported by a.go
	writeImportRecord(t, db, repoID, sha, "c.go", ImportRecord{
		ImportedBy: []string{"b.go"},
	})
	writeImportRecord(t, db, repoID, sha, "b.go", ImportRecord{
		ImportedBy: []string{"a.go"},
	})
	writeImportRecord(t, db, repoID, sha, "a.go", ImportRecord{
		ImportedBy: []string{},
	})

	result, err := ComputeImpact(db, repoID, sha, []string{"c.go"})
	if err != nil {
		t.Fatalf("ComputeImpact error: %v", err)
	}

	if result.NodesVisited < 3 {
		t.Errorf("expected at least 3 nodes (c, b, a), got %d", result.NodesVisited)
	}

	distByPath := make(map[string]int)
	for _, n := range result.Impacted {
		distByPath[n.Path] = n.Distance
	}

	if distByPath["c.go"] != 0 {
		t.Errorf("c.go should have distance 0, got %d", distByPath["c.go"])
	}
	if distByPath["b.go"] != 1 {
		t.Errorf("b.go should have distance 1, got %d", distByPath["b.go"])
	}
	if distByPath["a.go"] != 2 {
		t.Errorf("a.go should have distance 2, got %d", distByPath["a.go"])
	}
}

// TestComputeImpact_MultipleSeeds verifies two seeds each pull in their importers.
func TestComputeImpact_MultipleSeeds(t *testing.T) {
	db := openTestDB(t)
	repoID := "repo1"
	sha := "sha1"

	writeImportRecord(t, db, repoID, sha, "a.go", ImportRecord{
		ImportedBy: []string{"c.go"},
	})
	writeImportRecord(t, db, repoID, sha, "b.go", ImportRecord{
		ImportedBy: []string{"d.go"},
	})

	result, err := ComputeImpact(db, repoID, sha, []string{"a.go", "b.go"})
	if err != nil {
		t.Fatalf("ComputeImpact error: %v", err)
	}

	paths := make(map[string]bool)
	for _, n := range result.Impacted {
		paths[n.Path] = true
	}

	for _, expected := range []string{"a.go", "b.go", "c.go", "d.go"} {
		if !paths[expected] {
			t.Errorf("expected %s in results", expected)
		}
	}
}

// TestComputeImpact_MissingRecord seed with no DB record: treated as ErrNotFound, no error.
func TestComputeImpact_MissingRecord(t *testing.T) {
	db := openTestDB(t)
	result, err := ComputeImpact(db, "repo1", "sha1", []string{"missing.go"})
	if err != nil {
		t.Fatalf("expected no error for missing record: %v", err)
	}
	// Only seed visited (at distance 0)
	if result.NodesVisited != 1 {
		t.Errorf("expected 1 node (seed), got %d", result.NodesVisited)
	}
}

// TestComputeImpact_FanInPopulated verifies FanIn reflects ImportedBy count.
func TestComputeImpact_FanInPopulated(t *testing.T) {
	db := openTestDB(t)
	repoID := "repo1"
	sha := "sha1"

	writeImportRecord(t, db, repoID, sha, "hot.go", ImportRecord{
		ImportedBy: []string{"x.go", "y.go", "z.go"},
	})

	result, err := ComputeImpact(db, repoID, sha, []string{"hot.go"})
	if err != nil {
		t.Fatalf("ComputeImpact error: %v", err)
	}

	for _, n := range result.Impacted {
		if n.Path == "hot.go" {
			if n.FanIn != 3 {
				t.Errorf("expected FanIn=3 for hot.go, got %d", n.FanIn)
			}
		}
	}
}

// TestComputeImpact_MaxDepthNodes verifies BFS does not exceed BFSMaxDepth.
func TestComputeImpact_MaxDepthNodes(t *testing.T) {
	db := openTestDB(t)
	repoID := "repo1"
	sha := "sha1"

	// Build chain: f0→f1→...→f6 (f0 imported by f1, etc.)
	files := []string{"f0.go", "f1.go", "f2.go", "f3.go", "f4.go", "f5.go", "f6.go"}
	for i := 0; i < len(files)-1; i++ {
		writeImportRecord(t, db, repoID, sha, files[i], ImportRecord{
			ImportedBy: []string{files[i+1]},
		})
	}
	writeImportRecord(t, db, repoID, sha, files[len(files)-1], ImportRecord{
		ImportedBy: []string{},
	})

	result, err := ComputeImpact(db, repoID, sha, []string{"f0.go"})
	if err != nil {
		t.Fatalf("ComputeImpact error: %v", err)
	}

	for _, n := range result.Impacted {
		if n.Distance > BFSMaxDepth {
			t.Errorf("node %s has distance %d > BFSMaxDepth %d", n.Path, n.Distance, BFSMaxDepth)
		}
	}
}
