// internal/server/ws_replication_test.go
package server_test

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/workforce"
)

func TestInjectSpaceContextAttachesReplicationContext(t *testing.T) {
	// Verify the contract: WithReplicationContext + GetReplicationContext roundtrip.
	rc := &workforce.MemReplicationContext{
		SpaceID:   "space-abc",
		SpaceName: "Test",
		Members: []workforce.ReplicationMember{
			{AgentName: "Alice", VaultName: "huginn:agent:user:alice"},
			{AgentName: "Bob", VaultName: "huginn:agent:user:bob"},
		},
	}
	ctx := workforce.WithReplicationContext(context.Background(), rc)
	got := workforce.GetReplicationContext(ctx)
	if got == nil {
		t.Fatal("GetReplicationContext returned nil after WithReplicationContext")
	}
	if got.SpaceID != "space-abc" {
		t.Errorf("SpaceID: got %q, want %q", got.SpaceID, "space-abc")
	}
	if len(got.Members) != 2 {
		t.Errorf("Members: got %d, want 2", len(got.Members))
	}
}
