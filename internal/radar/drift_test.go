package radar

import (
	"testing"
)

// ---------------------------------------------------------------------------
// matchGlobOrPrefix — cases not in radar_test.go
// ---------------------------------------------------------------------------

func TestMatchGlobOrPrefix_PrefixWithSlash(t *testing.T) {
	// "internal/auth" should match "internal/auth/login.go" (dir prefix with slash)
	if !matchGlobOrPrefix("internal/auth/login.go", "internal/auth") {
		t.Error("prefix match with directory should return true")
	}
}

func TestMatchGlobOrPrefix_PrefixExactFile(t *testing.T) {
	// Exact file match
	if !matchGlobOrPrefix("cmd/main.go", "cmd/main.go") {
		t.Error("exact match should return true")
	}
}

func TestMatchGlobOrPrefix_WildcardSuffix(t *testing.T) {
	if !matchGlobOrPrefix("internal/auth/login.go", "internal/auth/*") {
		t.Error("wildcard suffix should match files in directory")
	}
}

// ---------------------------------------------------------------------------
// isLayerViolation — additional cases
// ---------------------------------------------------------------------------

func TestIsLayerViolation_DomainToAPI_IsViolation(t *testing.T) {
	v := isLayerViolation("internal/domain/entity.go", "internal/api/endpoint.go")
	if v == nil {
		t.Error("expected violation: domain (rank 10) importing api (rank 30)")
	}
	if v != nil && v.From != "internal/domain/entity.go" {
		t.Errorf("wrong From: %q", v.From)
	}
}

func TestIsLayerViolation_APIToService_NotViolation(t *testing.T) {
	// api (rank 30) importing service (rank 20) is allowed (higher → lower)
	v := isLayerViolation("internal/api/handler.go", "internal/service/user.go")
	if v != nil {
		t.Errorf("unexpected violation for api→service, got: %+v", v)
	}
}

func TestIsLayerViolation_InfraTarget_AlwaysAllowed(t *testing.T) {
	// Even lower-rank → infra is allowed (infra rank=5, always allowed as target)
	v := isLayerViolation("internal/domain/entity.go", "internal/infra/db.go")
	if v != nil {
		t.Errorf("unexpected violation for domain→infra (infra always ok): %+v", v)
	}
}

func TestIsLayerViolation_UnknownPaths_Nil(t *testing.T) {
	v := isLayerViolation("random/foo.go", "random/bar.go")
	if v != nil {
		t.Errorf("expected nil for unknown layer paths, got: %+v", v)
	}
}

// ---------------------------------------------------------------------------
// detectCycles — additional coverage
// ---------------------------------------------------------------------------

func TestDetectCycles_DisconnectedNodes_NoCycle(t *testing.T) {
	// Two isolated chains, no cycles
	adj := map[string][]string{
		"A": {"B"},
		"C": {"D"},
	}
	cycles := detectCycles(adj, 8)
	if len(cycles) != 0 {
		t.Errorf("expected 0 cycles in disconnected DAG, got %d", len(cycles))
	}
}

func TestDetectCycles_LengthFilterApplied(t *testing.T) {
	// 5-node cycle: A→B→C→D→E→A
	adj := map[string][]string{
		"A": {"B"},
		"B": {"C"},
		"C": {"D"},
		"D": {"E"},
		"E": {"A"},
	}
	// maxLen=3 should filter out the 5-node cycle
	cycles := detectCycles(adj, 3)
	for _, c := range cycles {
		if len(c.Nodes) > 3 {
			t.Errorf("expected cycle length ≤ 3, got %d", len(c.Nodes))
		}
	}
}

// ---------------------------------------------------------------------------
// reachableSubgraph — cases not in radar_test.go
// ---------------------------------------------------------------------------

func TestReachableSubgraph_NilSeeds_ReturnsEmpty(t *testing.T) {
	adj := map[string][]string{
		"A": {"B"},
	}
	sub := reachableSubgraph(adj, nil, 4)
	if len(sub) != 0 {
		t.Errorf("expected empty subgraph for nil seeds, got %d nodes", len(sub))
	}
}

func TestReachableSubgraph_OnlyEdgesInVisitedIncluded(t *testing.T) {
	// A→B and A→C; B→D; start from A with maxDepth=1
	adj := map[string][]string{
		"A": {"B", "C"},
		"B": {"D"},
		"C": {},
	}
	sub := reachableSubgraph(adj, []string{"A"}, 1)
	// A, B, C are reachable (depth 0 and 1); D is NOT (depth 2 > maxDepth=1)
	if neighbors, ok := sub["B"]; ok {
		for _, n := range neighbors {
			if n == "D" {
				t.Error("D should not appear as neighbor of B in subgraph (depth exceeded)")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// canonicalCycleKey — rotation invariance
// ---------------------------------------------------------------------------

func TestCanonicalCycleKey_AllRotationsEqual(t *testing.T) {
	nodes1 := []string{"A", "B", "C"}
	nodes2 := []string{"B", "C", "A"}
	nodes3 := []string{"C", "A", "B"}

	key1 := canonicalCycleKey(nodes1)
	key2 := canonicalCycleKey(nodes2)
	key3 := canonicalCycleKey(nodes3)

	if key1 != key2 || key2 != key3 {
		t.Errorf("rotations should produce same key: %q, %q, %q", key1, key2, key3)
	}
}

// ---------------------------------------------------------------------------
// DetectDrift — new edge detected when baseline absent
// ---------------------------------------------------------------------------

func TestDetectDrift_EdgeWithNoBaseline_IsNew(t *testing.T) {
	db := openTestDB(t)
	repoID := "repo1"
	sha := "sha1"

	writeEdge(t, db, repoID, sha, "cmd/main.go", "internal/service/user.go")

	result, err := DetectDrift(db, repoID, sha, "main", []string{"cmd/main.go"})
	if err != nil {
		t.Fatalf("DetectDrift error: %v", err)
	}

	found := false
	for _, e := range result.NewEdges {
		if e.From == "cmd/main.go" && e.To == "internal/service/user.go" {
			found = true
		}
	}
	if !found {
		t.Error("expected cmd/main.go → internal/service/user.go as new edge")
	}
}

// TestDetectDrift_CrossLayerEdge verifies domain→api produces a CrossLayerViolation.
func TestDetectDrift_CrossLayerEdge_Reported(t *testing.T) {
	db := openTestDB(t)
	repoID := "repo1"
	sha := "sha1"

	writeEdge(t, db, repoID, sha, "internal/domain/entity.go", "internal/api/endpoint.go")

	result, err := DetectDrift(db, repoID, sha, "main", []string{"internal/domain/entity.go"})
	if err != nil {
		t.Fatalf("DetectDrift error: %v", err)
	}

	if len(result.CrossLayerViolations) == 0 {
		t.Error("expected at least one cross-layer violation for domain→api edge")
	}
}

// TestDetectDrift_EmptyChangedFiles still processes all current edges.
func TestDetectDrift_EmptyChangedFiles_StillScansEdges(t *testing.T) {
	db := openTestDB(t)
	repoID := "repo1"
	sha := "sha1"

	writeEdge(t, db, repoID, sha, "cmd/main.go", "internal/service/user.go")

	result, err := DetectDrift(db, repoID, sha, "main", []string{})
	if err != nil {
		t.Fatalf("DetectDrift error: %v", err)
	}
	// Edge should still appear as new (no baseline)
	if len(result.NewEdges) == 0 {
		t.Error("expected new edges even with empty changedFiles")
	}
}
