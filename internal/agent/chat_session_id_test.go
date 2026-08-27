package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/tools"
)

type captureSessionTool struct {
	mu  sync.Mutex
	sid string
}

func (t *captureSessionTool) Name() string                      { return "capture_session" }
func (t *captureSessionTool) Description() string               { return "captures session id from tool ctx" }
func (t *captureSessionTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *captureSessionTool) Schema() backend.Tool {
	return backend.Tool{Function: backend.ToolFunction{Name: t.Name()}}
}
func (t *captureSessionTool) Execute(ctx context.Context, _ map[string]any) tools.ToolResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sid = GetSessionID(ctx)
	return tools.ToolResult{Output: t.sid}
}

func chatWithCapture(t *testing.T, ctx context.Context, orchSessionID string) string {
	t.Helper()
	cap := &captureSessionTool{}
	reg := newRegistryWith(cap)
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("capture_session", "c1"),
			{Content: "done", DoneReason: "stop"},
		},
	}
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	gate := permissions.NewGate(true, nil)
	o.SetTools(reg, gate)
	ag := &agents.Agent{Name: "Steve", ModelID: "test-model", LocalTools: []string{"capture_session"}}
	if err := o.ChatWithAgent(ctx, ag, "hi", orchSessionID, nil, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}
	cap.mu.Lock()
	defer cap.mu.Unlock()
	if cap.sid == "" && orchSessionID == "" && GetSessionID(ctx) == "" {
		return ""
	}
	return cap.sid
}

func TestChatWithAgent_SetsSessionIDOnTools(t *testing.T) {
	got := chatWithCapture(t, context.Background(), "hallway-sess")
	if got != "hallway-sess" {
		t.Fatalf("tool session = %q, want hallway-sess", got)
	}
}

func TestChatWithAgent_PreservesExistingSessionID(t *testing.T) {
	ctx := SetSessionID(context.Background(), "85ff-hallway")
	got := chatWithCapture(t, ctx, "space-thread-parent-Steve")
	if got != "85ff-hallway" {
		t.Fatalf("tool session = %q, want 85ff-hallway (not the ephemeral orch id)", got)
	}
}

func TestSetSpaceID_RoundTrip(t *testing.T) {
	ctx := SetSpaceID(context.Background(), "desk-dm")
	if got := GetSpaceID(ctx); got != "desk-dm" {
		t.Fatalf("got %q", got)
	}
	if got := GetSpaceID(context.Background()); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestChatWithAgent_QueuesWhenSessionBusy(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	mb := &queueBackend{started: started, release: release}
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	ag := &agents.Agent{Name: "Winston", ModelID: "test-model"}

	var firstErr, secondErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		firstErr = o.ChatWithAgent(context.Background(), ag, "first", "hallway-shared", nil, nil, nil)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first ChatWithAgent never started")
	}
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		secondErr = o.ChatWithAgent(ctx, ag, "second", "hallway-shared", nil, nil, nil)
	}()
	time.Sleep(30 * time.Millisecond)
	close(release)
	wg.Wait()
	if firstErr != nil {
		t.Fatalf("first: %v", firstErr)
	}
	if secondErr != nil {
		t.Fatalf("second should queue, not SNAP: %v", secondErr)
	}
	if mb.calls() != 2 {
		t.Fatalf("backend calls = %d, want 2 (queued, not dropped)", mb.calls())
	}
}

type queueBackend struct {
	mu      sync.Mutex
	n       int
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (m *queueBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	m.mu.Lock()
	m.n++
	n := m.n
	m.mu.Unlock()
	if n == 1 {
		m.once.Do(func() { close(m.started) })
		<-m.release
	}
	if req.OnToken != nil {
		req.OnToken("ok")
	}
	return &backend.ChatResponse{Content: "ok", DoneReason: "stop"}, nil
}
func (m *queueBackend) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.n
}
func (m *queueBackend) Health(context.Context) error   { return nil }
func (m *queueBackend) Shutdown(context.Context) error { return nil }
func (m *queueBackend) ContextWindow() int             { return 128_000 }
