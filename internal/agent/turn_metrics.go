package agent

import (
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/turnmetrics"
)

// turnMetricsHook wraps a RunLoop run's callbacks to stamp t_request /
// t_first_token / t_first_signal / t_complete with near-zero overhead:
// a handful of atomics and two sync.Once gates, no I/O on the turn path.
// The actual write happens off the hot path via cfg.MetricsWriter.Enqueue,
// which is itself non-blocking (see turnmetrics.Writer).
//
// t_request is stamped at RunLoop entry — the earliest point RunLoop has
// consistent access to the resolved model/messages for every call site
// (chat_engine.go, orchestrator.go, agent_dispatcher.go, mcp_agent_chat.go
// all construct RunLoopConfig immediately before calling RunLoop, so entry
// and "turn started processing" are effectively the same instant).
type turnMetricsHook struct {
	writer *turnmetrics.Writer

	base turnmetrics.TurnMetric // fields fixed at construction (session, model, ...)

	firstTokenOnce  sync.Once
	firstSignalOnce sync.Once
	firstTokenAt    atomic.Int64 // UnixNano; 0 = never
	firstSignalAt   atomic.Int64
	toolCalls       atomic.Int32
}

// newTurnMetricsHook returns nil when writer is nil so every call site stays
// a cheap nil check (`if h != nil`) rather than a branch on a config flag.
func newTurnMetricsHook(cfg *RunLoopConfig) *turnMetricsHook {
	if cfg.MetricsWriter == nil {
		return nil
	}
	promptChars := 0
	for _, m := range cfg.Messages {
		promptChars += len(m.Content)
	}
	h := &turnMetricsHook{writer: cfg.MetricsWriter}
	h.base = turnmetrics.TurnMetric{
		SessionID:    cfg.SessionID,
		AgentName:    cfg.AgentName,
		Model:        cfg.ModelName,
		Provider:     guessProvider(cfg.ModelName),
		TurnKind:     cfg.TurnKind,
		PromptChars:  promptChars,
		MessageCount: len(cfg.Messages),
		TRequest:     time.Now(),
	}

	// Wrap callbacks in place so every downstream path (including the many
	// early-return branches in RunLoop) records through the same hook
	// without threading a new parameter through each one.
	origToken := cfg.OnToken
	cfg.OnToken = func(tok string) {
		h.markToken()
		if origToken != nil {
			origToken(tok)
		}
	}
	origEvent := cfg.OnEvent
	cfg.OnEvent = func(ev backend.StreamEvent) {
		h.markSignal()
		if ev.Type == backend.StreamText || ev.Type == backend.StreamThought {
			h.markToken()
		}
		if origEvent != nil {
			origEvent(ev)
		}
	}
	origToolCall := cfg.OnToolCall
	cfg.OnToolCall = func(callID, name string, args map[string]any) {
		h.markSignal()
		h.toolCalls.Add(1)
		if origToolCall != nil {
			origToolCall(callID, name, args)
		}
	}
	return h
}

func (h *turnMetricsHook) markToken() {
	h.firstTokenOnce.Do(func() { h.firstTokenAt.Store(time.Now().UnixNano()) })
	h.markSignal()
}

func (h *turnMetricsHook) markSignal() {
	h.firstSignalOnce.Do(func() { h.firstSignalAt.Store(time.Now().UnixNano()) })
}

// finish stamps t_complete and enqueues the row. Safe to call at most once
// (RunLoop calls it from a single top-level defer); isErr reflects whether
// the run ended in error.
func (h *turnMetricsHook) finish(isErr bool) {
	if h == nil {
		return
	}
	m := h.base
	m.ToolCallCount = int(h.toolCalls.Load())
	m.Complete = time.Now()
	m.IsError = isErr
	if ns := h.firstTokenAt.Load(); ns != 0 {
		m.HadFirstToken = true
		m.FirstToken = time.Unix(0, ns)
	}
	if ns := h.firstSignalAt.Load(); ns != 0 {
		m.FirstSignal = time.Unix(0, ns)
	}
	h.writer.Enqueue(m)

	// One greppable INFO line per turn — metadata only, never token/message
	// content (see turnmetrics.TurnMetric doc comment).
	ftMs := int64(-1)
	if m.HadFirstToken {
		ftMs = m.FirstToken.Sub(m.TRequest).Milliseconds()
	}
	slog.Info("turn_metrics",
		"model", m.Model,
		"agent", m.AgentName,
		"turn_kind", m.TurnKind,
		"ft_ms", ftMs,
		"complete_ms", m.Complete.Sub(m.TRequest).Milliseconds(),
		"tool_calls", m.ToolCallCount,
		"error", m.IsError,
	)
}

// guessProvider derives a provider label from a model id via cheap prefix/
// substring checks — no network or catalog lookup on the hot path. "litellm
// style" ids (provider/model) resolve from the prefix; everything else falls
// back to a family guess so the dashboard still groups sensibly.
func guessProvider(model string) string {
	if model == "" {
		return "unknown"
	}
	if i := strings.IndexByte(model, '/'); i > 0 {
		return model[:i]
	}
	lower := strings.ToLower(model)
	switch {
	case strings.Contains(lower, "claude"):
		return "anthropic"
	case strings.Contains(lower, "gpt") || strings.Contains(lower, "o1") ||
		strings.Contains(lower, "o3") || strings.Contains(lower, "o4"):
		return "openai"
	case strings.Contains(lower, "gemini"):
		return "google"
	case strings.Contains(lower, "grok"):
		return "xai"
	case strings.Contains(lower, "deepseek"):
		return "deepseek"
	case strings.Contains(lower, "llama") || strings.Contains(lower, "qwen") || strings.Contains(lower, "mistral") ||
		strings.Contains(lower, "gemma") || strings.Contains(lower, "phi"):
		return "ollama"
	default:
		return "unknown"
	}
}
