package radar

import (
	"fmt"
	"os"
	"testing"

	"github.com/cockroachdb/pebble/v2"
)

// ---------------------------------------------------------------------------
// Test 1: LayerConfig integration with drift detection
// ---------------------------------------------------------------------------

// TestLayerConfig_IntegrationWithDrift verifies that LoadLayerConfig correctly
// parses a custom huginn-radar.yaml and returns layer mappings that differ
// from defaultLayers.
func TestLayerConfig_IntegrationWithDrift(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	yamlContent := `layers:
  presentation:
    - "web/src/*"
    - "web/routes/*"
  core:
    - "lib/core/*"
  storage:
    - "lib/db/*"
`
	writeFile(t, dir, radarConfigFile, yamlContent)

	cfg, err := LoadLayerConfig(dir)
	if err != nil {
		t.Fatalf("LoadLayerConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}

	// Verify custom layers are present and differ from defaults.
	if _, ok := cfg.Layers["presentation"]; !ok {
		t.Error("expected 'presentation' layer in config")
	}
	if _, ok := cfg.Layers["core"]; !ok {
		t.Error("expected 'core' layer in config")
	}
	if _, ok := cfg.Layers["storage"]; !ok {
		t.Error("expected 'storage' layer in config")
	}

	// Default layers should NOT contain "presentation" — proving custom layers are used.
	if _, ok := defaultLayers["web/src"]; ok {
		t.Error("defaultLayers should not contain 'web/src'; custom layers should override")
	}

	// Verify pattern count.
	if len(cfg.Layers["presentation"]) != 2 {
		t.Errorf("expected 2 patterns for presentation, got %d", len(cfg.Layers["presentation"]))
	}
}

// ---------------------------------------------------------------------------
// Test 2: Forbidden edges + layer checks combined
// ---------------------------------------------------------------------------

// TestDetectDrift_ForbiddenEdge_WithLayerCheck verifies that a snapshot edge
// matching a forbidden-edge policy rule is reported in ForbiddenEdges, and that
// a cross-layer violation on the same or another edge is simultaneously detected.
func TestDetectDrift_ForbiddenEdge_WithLayerCheck(t *testing.T) {
	db := openTestDB(t)
	repoID := "repo-forbidden"
	sha := "sha-forbidden"
	branch := "main"

	// Set up a policy with a forbidden edge rule: domain → api is forbidden.
	policy := &BaselinePolicy{
		ForbiddenEdges: []ForbiddenEdge{
			{From: "internal/domain/*", To: "internal/api/*"},
		},
	}
	policyData := mustMarshal(t, policy)
	if err := db.Set(baselinePolicyKey(repoID, branch), policyData, pebble.Sync); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	// Create an edge that violates both forbidden-edge rule AND cross-layer rule:
	// domain (rank 10) → api (rank 30)
	writeEdge(t, db, repoID, sha, "internal/domain/user.go", "internal/api/handler.go")

	result, err := DetectDrift(db, repoID, sha, branch, []string{"internal/domain/user.go"})
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}

	// Check forbidden edge is reported.
	if len(result.ForbiddenEdges) == 0 {
		t.Error("expected at least one forbidden edge violation")
	} else {
		found := false
		for _, fe := range result.ForbiddenEdges {
			if fe.From == "internal/domain/user.go" && fe.To == "internal/api/handler.go" {
				found = true
			}
		}
		if !found {
			t.Error("expected forbidden edge domain/user.go → api/handler.go")
		}
	}

	// Check cross-layer violation is also reported (domain rank 10 → api rank 30).
	if len(result.CrossLayerViolations) == 0 {
		t.Error("expected at least one cross-layer violation for domain → api")
	} else {
		found := false
		for _, cv := range result.CrossLayerViolations {
			if cv.From == "internal/domain/user.go" && cv.To == "internal/api/handler.go" {
				found = true
			}
		}
		if !found {
			t.Error("expected cross-layer violation domain/user.go → api/handler.go")
		}
	}
}

// ---------------------------------------------------------------------------
// Test 3: BFSMaxVisited truncation
// ---------------------------------------------------------------------------

// TestComputeImpact_BFSMaxVisited_Truncation creates a graph that exceeds the
// BFS visit limit and verifies that Truncated=true on the result.
func TestComputeImpact_BFSMaxVisited_Truncation(t *testing.T) {
	db := openTestDB(t)
	repoID := "repo-trunc"
	sha := "sha-trunc"

	// Build a wide fan-out graph: root.go is imported by N files,
	// each of which is imported by N more files, to exceed BFSMaxVisited.
	// BFSMaxVisited = 10_000. We create a single seed with enough fan-out
	// to exceed the limit at depth 2.
	//
	// Depth 0: root.go (1 node)
	// Depth 1: fan1_0..fan1_99 (100 nodes)
	// Depth 2: each fan1_X has 110 importers → 100*110 = 11,000 nodes
	// Total reachable > 10,000 → Truncated=true

	fan1 := make([]string, 100)
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("fan1_%d.go", i)
		fan1[i] = name
	}
	writeImportRecord(t, db, repoID, sha, "root.go", ImportRecord{
		ImportedBy: fan1,
	})

	for _, f1 := range fan1 {
		fan2 := make([]string, 110)
		for j := 0; j < 110; j++ {
			fan2[j] = fmt.Sprintf("%s_fan2_%d.go", f1, j)
		}
		writeImportRecord(t, db, repoID, sha, f1, ImportRecord{
			ImportedBy: fan2,
		})
	}

	result, err := ComputeImpact(db, repoID, sha, []string{"root.go"})
	if err != nil {
		t.Fatalf("ComputeImpact error: %v", err)
	}

	if !result.Truncated {
		t.Errorf("expected Truncated=true when graph exceeds BFSMaxVisited (%d), got NodesVisited=%d",
			BFSMaxVisited, result.NodesVisited)
	}
	if result.NodesVisited < BFSMaxVisited {
		t.Errorf("expected at least %d nodes visited, got %d", BFSMaxVisited, result.NodesVisited)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := dir + "/" + name
	if err := writeFileBytes(path, []byte(content)); err != nil {
		t.Fatalf("writeFile %s: %v", name, err)
	}
}

func writeFileBytes(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}
