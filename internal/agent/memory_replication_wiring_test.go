package agent

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/stats"
	"github.com/scrypster/huginn/internal/tools"
	"github.com/scrypster/huginn/internal/workforce"
)

type replicationGoldenBackend struct {
	mu    sync.Mutex
	calls int
}

func (b *replicationGoldenBackend) ChatCompletion(_ context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls++
	if b.calls == 1 {
		return &backend.ChatResponse{
			DoneReason: "tool_use",
			ToolCalls: []backend.ToolCall{
				{
					ID: "tc-1",
					Function: backend.ToolCallFunction{
						Name: "muninn_remember",
						Arguments: map[string]any{
							"concept": "release checklist",
							"content": "Ship no-LLM golden confidence gate.",
							"type":    "decision",
						},
					},
				},
			},
		}, nil
	}
	return &backend.ChatResponse{
		DoneReason: "stop",
		Content:    "captured",
	}, nil
}

func (b *replicationGoldenBackend) Health(_ context.Context) error   { return nil }
func (b *replicationGoldenBackend) Shutdown(_ context.Context) error { return nil }
func (b *replicationGoldenBackend) ContextWindow() int               { return 8192 }

type rememberTool struct{}

func (t *rememberTool) Name() string                      { return "muninn_remember" }
func (t *rememberTool) Description() string               { return "Remember content in vault." }
func (t *rememberTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *rememberTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "muninn_remember",
			Description: "Remember content in vault.",
			Parameters: backend.ToolParameters{
				Type:       "object",
				Properties: map[string]backend.ToolProperty{},
			},
		},
	}
}
func (t *rememberTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	return tools.ToolResult{Output: `{"ok":true}`}
}

type captureReplicationQueue struct {
	mu       sync.Mutex
	inserts  int
	firstArg any
}

func (q *captureReplicationQueue) ReadQ() ReplicationDBReader  { return noopReplicationReader{} }
func (q *captureReplicationQueue) WriteQ() ReplicationDBWriter { return q }

func (q *captureReplicationQueue) ExecContext(_ context.Context, query string, args ...any) (ReplicationResult, error) {
	if !strings.Contains(query, "INSERT INTO memory_replication_queue") {
		return noopReplicationResult{}, nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.inserts++
	if q.firstArg == nil && len(args) > 0 {
		q.firstArg = args[0]
	}
	return noopReplicationResult{}, nil
}

func (q *captureReplicationQueue) insertCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.inserts
}

func (q *captureReplicationQueue) firstTargetVault() string {
	q.mu.Lock()
	defer q.mu.Unlock()
	v, _ := q.firstArg.(string)
	return v
}

type noopReplicationReader struct{}

func (noopReplicationReader) QueryContext(context.Context, string, ...any) (ReplicationRows, error) {
	return noopReplicationRows{}, nil
}
func (noopReplicationReader) QueryRowContext(context.Context, string, ...any) ReplicationRow {
	return noopReplicationRow{}
}

type noopReplicationRows struct{}

func (noopReplicationRows) Next() bool        { return false }
func (noopReplicationRows) Scan(...any) error { return nil }
func (noopReplicationRows) Close() error      { return nil }
func (noopReplicationRows) Err() error        { return nil }

type noopReplicationRow struct{}

func (noopReplicationRow) Scan(...any) error { return sql.ErrNoRows }

type noopReplicationResult struct{}

func (noopReplicationResult) RowsAffected() (int64, error) { return 1, nil }

func TestChatWithAgent_ChannelMemoryReplication_QueuesFanout(t *testing.T) {
	b := &replicationGoldenBackend{}
	orch, err := NewOrchestrator(b, modelconfig.DefaultModels(), nil, nil, stats.NoopCollector{}, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	reg := tools.NewRegistry()
	reg.Register(&rememberTool{})
	reg.TagTools([]string{"muninn_remember"}, "muninndb")
	orch.SetTools(reg, nil)

	queue := &captureReplicationQueue{}
	replicator := NewMemoryReplicator("", queue)
	orch.SetMemoryReplicator(replicator)
	defer replicator.Stop()

	ag := &agents.Agent{Name: "Lead", ModelID: "claude-haiku-4"}
	replCtx := &workforce.MemReplicationContext{
		SpaceID:   "space-golden",
		SpaceName: "Golden",
		Members: []workforce.ReplicationMember{
			{AgentName: "Lead", VaultName: "huginn:agent:user:lead"},
			{AgentName: "Helper", VaultName: "huginn:agent:user:helper"},
		},
	}
	ctx := workforce.WithReplicationContext(context.Background(), replCtx)

	if err := orch.ChatWithAgent(ctx, ag, "remember this", "sess-golden", nil, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && queue.insertCount() == 0 {
		time.Sleep(10 * time.Millisecond)
	}

	if queue.insertCount() == 0 {
		t.Fatal("expected memory replication queue insert, got none")
	}
	if got := queue.firstTargetVault(); got != "huginn:agent:user:helper" {
		t.Fatalf("target vault = %q, want helper vault", got)
	}
}

func TestChatWithAgent_WithoutReplicationContext_DoesNotQueueFanout(t *testing.T) {
	b := &replicationGoldenBackend{}
	orch, err := NewOrchestrator(b, modelconfig.DefaultModels(), nil, nil, stats.NoopCollector{}, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	reg := tools.NewRegistry()
	reg.Register(&rememberTool{})
	reg.TagTools([]string{"muninn_remember"}, "muninndb")
	orch.SetTools(reg, nil)

	queue := &captureReplicationQueue{}
	replicator := NewMemoryReplicator("", queue)
	orch.SetMemoryReplicator(replicator)
	defer replicator.Stop()

	ag := &agents.Agent{Name: "Lead", ModelID: "claude-haiku-4"}
	if err := orch.ChatWithAgent(context.Background(), ag, "remember this", "sess-no-repl", nil, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if got := queue.insertCount(); got != 0 {
		t.Fatalf("expected no replication queue inserts, got %d", got)
	}
}
