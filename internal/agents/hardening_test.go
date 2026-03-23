package agents_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
)

// blockingBackend is a mock backend that blocks until the context is cancelled.
type blockingBackend struct{}

func (b *blockingBackend) ChatCompletion(ctx context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (b *blockingBackend) Health(ctx context.Context) error   { return nil }
func (b *blockingBackend) Shutdown(ctx context.Context) error { return nil }
func (b *blockingBackend) ContextWindow() int               { return 128_000 }

// TestRegistry_ConcurrentRegisterAndLookup verifies no data races when
// Register and ByName are called concurrently from many goroutines.
func TestRegistry_ConcurrentRegisterAndLookup(t *testing.T) {
	t.Parallel()

	reg := agents.NewRegistry()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("Agent%d", i%5)
			a := &agents.Agent{Name: name, ModelID: fmt.Sprintf("model-%d", i)}
			reg.Register(a)

			// Interleave reads with writes to stress the RWMutex
			reg.ByName(name)
			reg.All()
		}()
	}

	wg.Wait()

	// Registry must still be usable after concurrent access
	all := reg.All()
	if all == nil {
		t.Error("All() returned nil after concurrent access")
	}
}

// TestConsultTool_CancelledContext verifies that a pre-cancelled context causes
// Execute to return an error (context propagates into the backend call).
func TestConsultTool_CancelledContext(t *testing.T) {
	t.Parallel()

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Mark", ModelID: "m"})

	var depth int32
	tool := agents.NewConsultAgentTool(reg, &blockingBackend{}, &depth, nil, nil)

	// Use a context that is already cancelled so the blocking backend unblocks immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Execute is called

	result := tool.Execute(ctx, map[string]any{
		"agent_name": "Mark",
		"question":   "What is the meaning of life?",
	})

	if !result.IsError {
		t.Error("expected error result when context is cancelled")
	}
	if result.Error == "" {
		t.Error("expected non-empty error message")
	}
	// The error should reference the consultation failure (which wraps the context error)
	errLower := strings.ToLower(result.Error)
	if !strings.Contains(errLower, "consultation") && !strings.Contains(errLower, "cancel") &&
		!strings.Contains(errLower, "context") && !strings.Contains(errLower, "mark") {
		t.Errorf("error should reference cancellation or consultation, got: %s", result.Error)
	}
}

// TestAgent_SwapModel_EmptyString documents that SwapModel("") is allowed
// and GetModelID() returns "" — caller's responsibility to validate.
func TestAgent_SwapModel_EmptyString(t *testing.T) {
	t.Parallel()

	a := &agents.Agent{Name: "Chris", ModelID: "some-model"}
	a.SwapModel("")

	got := a.GetModelID()
	if got != "" {
		t.Errorf("expected empty string after SwapModel(\"\"), got %q", got)
	}
}

// TestParseDirective_VeryLongInput verifies that a 10000-character string
// does not cause a panic or catastrophic regex backtracking — it must complete
// quickly and return nil or a valid directive without hanging.
func TestParseDirective_VeryLongInput(t *testing.T) {
	t.Parallel()

	reg := makeTestRegistry()
	// Build a 10000-character string that looks like it starts a directive
	// but has an enormous payload — guards against catastrophic backtracking.
	payload := strings.Repeat("x", 10000)
	long := "Have Chris plan " + payload

	// Must not panic and must return quickly without hanging.
	result := agents.ParseDirective(long, reg)
	// result may be nil or a valid directive — both are acceptable.
	// The important contract is it returns without catastrophic backtracking.
	_ = result
}

// TestLoadAgents_EmptyAgentsArray verifies that {"agents":[]} written to a file
// loads as an empty (non-nil) Agents slice with no error and no default injection.
func TestLoadAgents_EmptyAgentsArray(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "agents.json")

	data, err := json.Marshal(agents.AgentsConfig{Agents: []agents.AgentDef{}})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	cfg, err := agents.LoadAgentsFrom(path)
	if err != nil {
		t.Fatalf("unexpected error loading empty agents array: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Agents == nil {
		t.Error("Agents slice must be non-nil (empty array, not null)")
	}
	if len(cfg.Agents) != 0 {
		t.Errorf("expected 0 agents (no defaults injected), got %d", len(cfg.Agents))
	}
}

// TestBuildRegistry_EmptyConfig verifies that BuildRegistry with an empty
// AgentsConfig returns a non-nil registry where All() returns an empty (not nil) slice.
func TestBuildRegistry_EmptyConfig(t *testing.T) {
	t.Parallel()

	cfg := &agents.AgentsConfig{Agents: []agents.AgentDef{}}
	models := modelconfig.DefaultModels()

	reg := agents.BuildRegistry(cfg, models)
	if reg == nil {
		t.Fatal("expected non-nil registry from BuildRegistry with empty config")
	}

	all := reg.All()
	if all == nil {
		t.Error("All() returned nil; expected an empty non-nil slice")
	}
	if len(all) != 0 {
		t.Errorf("expected 0 agents registered, got %d", len(all))
	}
}

// TestConsultTool_MissingAgentName verifies that Execute with an unregistered
// agent name returns an error containing "unknown" or "not found".
func TestConsultTool_MissingAgentName(t *testing.T) {
	t.Parallel()

	// Registry with no agents registered
	reg := agents.NewRegistry()
	var depth int32
	tool := agents.NewConsultAgentTool(reg, &mockBackend{}, &depth, nil, nil)

	result := tool.Execute(context.Background(), map[string]any{
		"agent_name": "NonExistentAgent",
		"question":   "Are you there?",
	})

	if !result.IsError {
		t.Error("expected error when consulting an unknown agent")
	}

	errLower := strings.ToLower(result.Error)
	if !strings.Contains(errLower, "unknown") && !strings.Contains(errLower, "not found") {
		t.Errorf("error should mention 'unknown' or 'not found', got: %s", result.Error)
	}
}
