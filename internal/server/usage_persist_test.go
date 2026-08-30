package server

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/threadmgr"
)

type usageStampBackend struct{}

func (u *usageStampBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	if req.OnToken != nil {
		req.OnToken("hi")
	}
	return &backend.ChatResponse{
		Content:          "hi",
		DoneReason:       "stop",
		PromptTokens:     11,
		CompletionTokens: 7,
	}, nil
}
func (u *usageStampBackend) Health(_ context.Context) error   { return nil }
func (u *usageStampBackend) Shutdown(_ context.Context) error { return nil }
func (u *usageStampBackend) ContextWindow() int               { return 4096 }

func TestApplyKnownUsage_StampsAssistantAndRecordsCost(t *testing.T) {
	srv := newTestServerWithBackend(t, &usageStampBackend{})
	ca := threadmgr.NewCostAccumulator(0)
	var sinkCalls int
	var sinkPrompt, sinkComp int
	ca.SetCostSink(func(_ string, _ float64, prompt, completion int) {
		sinkCalls++
		sinkPrompt, sinkComp = prompt, completion
	})
	srv.ca = ca

	if err := srv.orch.Chat(context.Background(), "hello", nil, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	msg := session.SessionMessage{Role: "assistant", Content: "hi"}
	srv.applyKnownUsage(&msg, "sess-1", "claude-sonnet-4")
	if msg.PromptTok != 11 || msg.CompTok != 7 {
		t.Fatalf("stamped usage = %d/%d, want 11/7", msg.PromptTok, msg.CompTok)
	}
	if msg.ModelName != "claude-sonnet-4" {
		t.Fatalf("ModelName = %q, want claude-sonnet-4", msg.ModelName)
	}
	if sinkCalls != 1 {
		t.Fatalf("cost sink calls = %d, want 1 (cost_history path)", sinkCalls)
	}
	if sinkPrompt != 11 || sinkComp != 7 {
		t.Fatalf("sink tokens = %d/%d, want 11/7", sinkPrompt, sinkComp)
	}
}

func TestApplyKnownUsage_SkipsUnknownZero(t *testing.T) {
	srv, _ := newTestServer(t)
	ca := threadmgr.NewCostAccumulator(0)
	var sinkCalls int
	ca.SetCostSink(func(string, float64, int, int) { sinkCalls++ })
	srv.ca = ca

	msg := session.SessionMessage{Role: "assistant", Content: "unused"}
	srv.applyKnownUsage(&msg, "sess-1", "llama3:8b")
	if msg.PromptTok != 0 || msg.CompTok != 0 {
		t.Fatalf("unknown usage must not stamp zeros as known: %d/%d", msg.PromptTok, msg.CompTok)
	}
	if sinkCalls != 0 {
		t.Fatalf("cost sink calls = %d, want 0 when usage is unknown", sinkCalls)
	}
}
