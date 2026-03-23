package integration_test

import (
	"context"
	"errors"
	"testing"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/stats"
)

// TestIntegration_BackendError verifies that orchestrator.Chat properly
// propagates backend errors without crashing.
func TestIntegration_BackendError(t *testing.T) {
	wantErr := errors.New("backend connection timeout")
	errBackend := &errorBackend{err: wantErr}

	reg := stats.NewRegistry()
	orch, err := agent.NewOrchestrator(errBackend, nil, nil, nil, reg.Collector(), nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	err = orch.Chat(context.Background(), "test query", nil, nil)
	if err == nil {
		t.Error("expected Chat to propagate backend error")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected backend error %v, got %v", wantErr, err)
	}
}

// TestIntegration_ContextCancellation verifies that a cancelled context
// is handled correctly by Chat (may or may not propagate error depending on implementation).
func TestIntegration_ContextCancellation(t *testing.T) {
	mb := newMockBackend("delayed response")
	reg := stats.NewRegistry()
	orch, err := agent.NewOrchestrator(mb, nil, nil, nil, reg.Collector(), nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err = orch.Chat(ctx, "test query", nil, nil)
	// Chat may return an error or nil depending on timing; the orchestrator
	// should handle cancellation gracefully either way.
	if orch.CurrentState() != agent.StateIdle {
		t.Errorf("expected StateIdle after Chat, got %v", orch.CurrentState())
	}
}

// TestIntegration_EmptyQuery verifies that Chat handles empty queries gracefully.
func TestIntegration_EmptyQuery(t *testing.T) {
	mb := newMockBackend("response")
	reg := stats.NewRegistry()
	orch, err := agent.NewOrchestrator(mb, nil, nil, nil, reg.Collector(), nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	err = orch.Chat(context.Background(), "", nil, nil)
	// Empty query might be valid or invalid depending on implementation;
	// just ensure it doesn't crash.
	if orch.CurrentState() != agent.StateIdle {
		t.Errorf("expected StateIdle after Chat, got %v", orch.CurrentState())
	}
}

// TestIntegration_LargePayload verifies that Chat handles large token streams.
func TestIntegration_LargePayload(t *testing.T) {
	// Generate a large response (simulating a long-winded LLM output)
	largeResponse := ""
	for i := 0; i < 5000; i++ {
		largeResponse += "This is a test token. "
	}

	mb := newMockBackend(largeResponse)
	reg := stats.NewRegistry()
	orch, err := agent.NewOrchestrator(mb, nil, nil, nil, reg.Collector(), nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	var tokenCount int
	err = orch.Chat(context.Background(), "generate long response", func(tok string) {
		if tok != "" {
			tokenCount++
		}
	}, nil)
	if err != nil {
		t.Fatalf("Chat with large payload: %v", err)
	}
	if tokenCount == 0 {
		t.Error("expected tokens to be emitted for large payload")
	}
}

// TestIntegration_ChatAfterBadBackend verifies that a Chat after a backend error
// returns an appropriate error without panicking.
func TestIntegration_ChatAfterBadBackend(t *testing.T) {
	errBackend := &errorBackend{err: errors.New("connection failed")}
	reg := stats.NewRegistry()
	orch, err := agent.NewOrchestrator(errBackend, nil, nil, nil, reg.Collector(), nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	chatErr := orch.Chat(context.Background(), "test", nil, nil)
	if chatErr == nil {
		t.Error("expected error from Chat with failing backend")
	}
	if orch.CurrentState() != agent.StateIdle {
		t.Errorf("expected StateIdle after error, got %d", orch.CurrentState())
	}
}

// TestIntegration_MultipleChats verifies that sequential Chat calls work correctly.
func TestIntegration_MultipleChats(t *testing.T) {
	responses := []string{"first response", "second response", "third response"}
	mb := newMockBackend(responses...)

	reg := stats.NewRegistry()
	orch, err := agent.NewOrchestrator(mb, nil, nil, nil, reg.Collector(), nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	for i, expectedResp := range responses {
		var tokens []string
		err := orch.Chat(context.Background(), "query "+string(rune(i)), func(tok string) {
			if tok != "" {
				tokens = append(tokens, tok)
			}
		}, nil)
		if err != nil {
			t.Fatalf("Chat %d: %v", i, err)
		}
		got := joinTokens(tokens)
		if got != expectedResp {
			t.Errorf("Chat %d: expected %q, got %q", i, expectedResp, got)
		}
	}
}

// errorBackend is a backend that always returns an error.
type errorBackend struct {
	err error
}

func (e *errorBackend) ChatCompletion(_ context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
	return nil, e.err
}

func (e *errorBackend) Health(_ context.Context) error   { return e.err }
func (e *errorBackend) Shutdown(_ context.Context) error { return nil }
func (e *errorBackend) ContextWindow() int               { return 128_000 }

// Helper to join tokens into a single string.
func joinTokens(tokens []string) string {
	result := ""
	for _, t := range tokens {
		result += t
	}
	return result
}
