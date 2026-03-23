//go:build integration

package memory_test

import (
	"context"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/memory"
)

// TestPerAgentMemory_EndToEnd verifies:
// 1. A learning written to an agent vault is recalled from that vault
// 2. Two different agents have isolated vaults (no cross-contamination)
// 3. Project vault memories are accessible but separate from agent vault memories
func TestPerAgentMemory_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	pb, err := memory.NewPebbleBackend(dir + "/test.pebble")
	if err != nil {
		t.Fatal(err)
	}
	defer pb.Close()

	ctx := context.Background()

	steveVault := "huginn:agent:mj:steve"
	chrisVault := "huginn:agent:mj:chris"
	projectVault := "huginn:project:myproject"

	// Write a learning specific to Steve
	_ = pb.Write(ctx, steveVault, "l1", "User prefers short function names in Go", []string{"style", "go"})
	// Write a project-wide fact
	_ = pb.Write(ctx, projectVault, "p1", "Project uses PostgreSQL 16", []string{"database"})
	// Write something specific to Chris (should NOT appear when Steve activates)
	_ = pb.Write(ctx, chrisVault, "c1", "User works in Python when doing data science", []string{"python"})

	// Steve activates from his own vault: should find his learning
	steveResults, err := pb.Recall(ctx, steveVault, []string{"user preferences style"}, 10)
	if err != nil {
		t.Fatal(err)
	}

	foundSteve := false
	foundChrisInSteve := false
	for _, r := range steveResults {
		if strings.Contains(r.Content, "short function names") {
			foundSteve = true
		}
		if strings.Contains(r.Content, "Python") {
			foundChrisInSteve = true
		}
	}
	if !foundSteve {
		t.Error("Steve's learning not recalled from his vault")
	}
	if foundChrisInSteve {
		t.Error("vault isolation violation: Chris's memory appeared in Steve's recall")
	}

	// Chris activates from his own vault: should find his learning, not Steve's
	chrisResults, err := pb.Recall(ctx, chrisVault, []string{"user preferences"}, 10)
	if err != nil {
		t.Fatal(err)
	}

	foundChris := false
	foundSteveInChris := false
	for _, r := range chrisResults {
		if strings.Contains(r.Content, "Python") {
			foundChris = true
		}
		if strings.Contains(r.Content, "short function names") {
			foundSteveInChris = true
		}
	}
	if !foundChris {
		t.Error("Chris's learning not recalled from his vault")
	}
	if foundSteveInChris {
		t.Error("vault isolation violation: Steve's memory appeared in Chris's recall")
	}

	// Project vault is separate from both agent vaults
	projectResults, err := pb.Recall(ctx, projectVault, []string{"database"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	foundProject := false
	for _, r := range projectResults {
		if strings.Contains(r.Content, "PostgreSQL") {
			foundProject = true
		}
	}
	if !foundProject {
		t.Error("project-level memory not recalled from project vault")
	}
}
