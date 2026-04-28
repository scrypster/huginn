package agent_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/scrypster/huginn/internal/tools"
	"github.com/scrypster/huginn/internal/workforce"
)

// interceptSpy counts Intercept calls.
type interceptSpy struct {
	calls int64
}

func (s *interceptSpy) Intercept(
	ctx context.Context,
	toolName string, args map[string]any,
	result tools.ToolResult,
	producerName string,
	replCtx *workforce.MemReplicationContext,
) {
	atomic.AddInt64(&s.calls, 1)
}

func TestInterceptSpyCountsCall(t *testing.T) {
	replCtx := &workforce.MemReplicationContext{
		SpaceID:   "space-1",
		SpaceName: "Test Space",
		Members: []workforce.ReplicationMember{
			{AgentName: "Bob", VaultName: "huginn:agent:user:bob"},
		},
	}
	ctx := workforce.WithReplicationContext(context.Background(), replCtx)

	spy := &interceptSpy{}
	spy.Intercept(ctx, "muninn_remember", map[string]any{
		"concept": "test concept",
		"content": "test content",
	}, tools.ToolResult{Output: "ok", IsError: false}, "Alice", replCtx)

	if spy.calls != 1 {
		t.Errorf("expected 1 Intercept call, got %d", spy.calls)
	}
}
