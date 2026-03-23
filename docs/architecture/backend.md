# LLM Backend Abstraction

How Huginn talks to language models, manages context, and tracks cost.

---

## The Problem

LLM providers change constantly. Anthropic ships a new API version, OpenAI changes pricing, a local model gains tool-calling support, or you need to run tests without hitting a real endpoint. If provider-specific code is scattered across the codebase, every provider change becomes a refactor.

Huginn solves this with a single `Backend` interface. All agent code — session management, compaction, tool calling, streaming — calls methods on that interface. Swapping providers is a one-line config change; adding a new provider is a single implementation file.

---

## Backend Interface

```go
// internal/backend/backend.go
type Backend interface {
    ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    Health(ctx context.Context) error
    Shutdown(ctx context.Context) error  // no-op for external backends
    ContextWindow() int                  // model's context window in tokens
}
```

**Why this minimal surface?**

- `ChatCompletion` is the only thing callers actually need. All providers speak request-in, response-out.
- `Health` lets startup checks and TUI indicators test reachability without a real inference call.
- `Shutdown` is a lifecycle hook for `ManagedBackend` (which owns a `llama-server` subprocess). External backends implement it as a no-op.
- `ContextWindow` gives the compaction system a provider-accurate token budget without hardcoding it in application logic.

---

## Supported Backends

| Provider | Type | Package | Notes |
|----------|------|---------|-------|
| Anthropic | `AnthropicBackend` | `backend` | Native Messages API, SSE streaming, extended thinking |
| OpenAI | `ExternalBackend` + API key | `backend` | OpenAI-compatible `/v1/chat/completions` |
| Ollama | `ExternalBackend` | `backend` | Local inference, no API key required |
| OpenRouter | `OpenRouterBackend` | `backend` | Routes to multiple cloud providers via OpenAI-compatible API |
| Local (managed) | `ManagedBackend` | `backend` | Huginn owns the `llama-server` subprocess; Shutdown() stops it |
| Any OpenAI-compatible | `ExternalBackend` | `backend` | Generic; accepts custom endpoint + optional Bearer token |

The factory function selects the concrete type:

```go
// internal/backend/factory.go
func NewFromConfig(provider, endpoint, apiKey, model string) (Backend, error) {
    switch provider {
    case "ollama", "external", "":
        b := NewExternalBackend(endpoint)
        b.SetModel(model)
        return b, nil
    case "openai":
        b := NewExternalBackendWithAPIKey(endpoint, resolvedAPIKey)
        b.SetModel(model)
        return b, nil
    case "anthropic":
        return NewAnthropicBackend(resolvedAPIKey, model), nil
    case "openrouter":
        return NewOpenRouterBackend(resolvedAPIKey, model), nil
    // ...
    }
}
```

API keys prefixed with `$` are resolved as environment variables (`$ANTHROPIC_API_KEY` → `os.Getenv("ANTHROPIC_API_KEY")`).

---

## Streaming Events

`ChatRequest` supports two streaming callbacks:

```go
// internal/backend/backend.go
type ChatRequest struct {
    Model    string
    Messages []Message
    Tools    []Tool
    OnToken  func(string)      // backward compat; nil = collect
    OnEvent  func(StreamEvent) // richer streaming; nil = use OnToken
}
```

`OnEvent` is the preferred interface. `OnToken` exists for callers that only care about text output and predate the richer event model.

```go
type StreamEventType string

const (
    StreamText    StreamEventType = "text"
    StreamThought StreamEventType = "thought" // Anthropic extended thinking tokens
    StreamDone    StreamEventType = "done"
)

type StreamEvent struct {
    Type    StreamEventType
    Content string
}
```

**StreamText**: A fragment of the assistant's text response. Callers accumulate these to build the full response string.

**StreamThought**: Anthropic extended thinking tokens (enabled via `anthropic-beta: interleaved-thinking-2025-05-14`). These are the model's visible reasoning steps. Non-Anthropic backends never emit this event type.

**StreamDone**: Signals end of stream. Emitted by the Anthropic backend after the SSE scanner exits; callers can use this to trigger UI updates.

**Backward compatibility**: If `OnEvent` is nil and `OnToken` is set, the Anthropic backend falls back to calling `OnToken` for `text_delta` events:

```go
// internal/backend/anthropic.go
case "text_delta":
    text, _ := delta["text"].(string)
    result.Content += text
    if req.OnEvent != nil {
        req.OnEvent(StreamEvent{Type: StreamText, Content: text})
    } else if req.OnToken != nil {
        req.OnToken(text)
    }
```

---

## Context Window Management

`ContextWindow()` is the mechanism by which compaction and other token-budget logic get the correct limit for the active model.

```go
// internal/modelconfig/contextwindow.go
var contextWindows = map[string]int{
    "claude-opus-4-6":   200000,
    "claude-sonnet-4-6": 200000,
    "gpt-4o":            128000,
    "gpt-4o-mini":       128000,
    "o3":                200000,
    "qwen2.5-coder:32b": 32768,
    "llama3.3:70b":      131072,
    // ...
}

func ContextWindowForModel(modelID string) int {
    if cw, ok := contextWindows[modelID]; ok {
        return cw
    }
    // Prefix match for versioned model IDs
    for key, cw := range contextWindows {
        if strings.HasPrefix(key, modelID) {
            return cw
        }
    }
    return defaultContextWindow // 8192
}
```

Resolution order: exact match → prefix match → default (8192). The prefix match handles model version suffixes (e.g., `claude-sonnet-4-6-20250514` matches the `claude-sonnet-4-6` entry).

`AnthropicBackend.ContextWindow()` overrides the default to 200,000 rather than 8,192, because all current Anthropic models have 200K windows and should not silently degrade:

```go
// internal/backend/anthropic.go
func (b *AnthropicBackend) ContextWindow() int {
    cw := modelconfig.ContextWindowForModel(b.model)
    if cw == modelconfig.DefaultContextWindow() {
        return 200_000
    }
    return cw
}
```

---

## Compaction Strategies

**Location:** `internal/compact/`

When the conversation history approaches the context window limit, the compactor reduces it. The trigger threshold is configurable; the default is **70%** of the context window.

```go
// internal/compact/compact.go
func (c *Compactor) MaybeCompact(
    ctx context.Context,
    messages []backend.Message,
    b backend.Backend,
    model string,
) ([]backend.Message, bool, error) {
    budget := b.ContextWindow() // use backend's actual window, not a hardcoded value
    // ...
}
```

### ExtractiveStrategy

```go
// internal/compact/extractive.go
func (s *ExtractiveStrategy) Compact(...) ([]backend.Message, error) {
    filePaths := extractFilePaths(messages)  // from tool call arguments
    tail := lastNExchanges(messages, 2)       // keep last 2 user turns verbatim

    // Produce: [summary_message] + tail
}
```

- Scans all tool calls for `file_path` and `path` arguments to build a "Files touched" list.
- Keeps the last 2 user exchanges verbatim (preserves the immediate task context).
- No LLM required. Always succeeds. Deterministic output.

**Trigger**: `EstimateTokens(messages) / budgetTokens >= trigger` (default 0.7).

Token estimation uses `cl100k_base` tiktoken encoding when available, falling back to `len(content)/4` as an approximation.

### LLMStrategy

```go
// internal/compact/llm.go
const llmCompactionTimeout = 30 * time.Second

const summaryPrompt = `Summarize this conversation. Preserve:
1. ALL file paths created or modified
2. All architectural decisions
3. Current task state
4. Any errors and resolutions

Return a ## Summary block only.`
```

- Sends the entire conversation to the LLM with `summaryPrompt`.
- Expects a `## Summary` block in the response; retries once if absent.
- Appends the last 3 user exchanges verbatim (more than extractive, for better continuity).
- Falls back to `ExtractiveStrategy` if:
  - The backend is nil
  - The LLM call times out (30s)
  - The response does not contain `## Summary` after retry
  - The compacted result still exceeds the budget

```go
// internal/compact/llm.go
if EstimateTokens(result) > budget {
    return s.fallback.Compact(ctx, messages, budget, nil, "")
}
```

### Why two strategies?

| | ExtractiveStrategy | LLMStrategy |
|--|---|---|
| Reliability | Always succeeds | Can fail/timeout |
| Summary quality | Mechanical (file list + count) | Semantic (preserves decisions, errors) |
| LLM cost | None | One summarization call |
| Use case | Safe fallback, offline mode | Production: best context quality |

The design principle: **data loss is worse than reduced summary quality**. Even if the LLM summary is richer, a failed compaction that crashes the agent loop is unacceptable. `ExtractiveStrategy` is the invariant floor.

### Compaction modes

| Mode | Behavior |
|------|---------|
| `auto` | Compact when `ShouldCompact()` returns true |
| `never` | Never compact (useful for short sessions or debugging) |
| `always` | Compact on every `MaybeCompact()` call |

---

## Pricing

**Location:** `internal/pricing/`

### PricingEntry

```go
// internal/pricing/table.go
type PricingEntry struct {
    PromptPer1M     float64 `json:"prompt"`
    CompletionPer1M float64 `json:"completion"`
}

var DefaultTable = map[string]PricingEntry{
    "claude-opus-4-6":   {3.00, 15.00},
    "claude-sonnet-4-6": {3.00, 15.00},
    "gpt-4o":            {2.50, 10.00},
    "gpt-4o-mini":       {0.15, 0.60},
    // ...
}
```

Costs are USD per 1M tokens. Users can supply an override JSON file to update pricing without recompiling.

### SessionTracker

```go
// internal/pricing/tracker.go
type SessionTracker struct {
    mu      sync.Mutex
    table   map[string]PricingEntry
    entries map[string]*UsageEntry
}

func (t *SessionTracker) Add(model string, promptTokens, completionTokens int) {
    t.mu.Lock()
    defer t.mu.Unlock()
    // accumulate per-model
}
```

`SessionTracker` accumulates token usage and cost across all `ChatCompletion` calls in a session. It is safe for concurrent use (mutex-protected). `StatusBarText()` formats the running total for TUI display: `~$0.0042`.

### FormatCost

```go
// internal/pricing/tracker.go
func FormatCost(cost float64) string {
    if cost == 0 {
        return "$0.00"
    }
    return fmt.Sprintf("~$%.4f", cost)
}
```

The `~` prefix signals that cost is an approximation (token counts from the API may differ slightly from billing).

---

## HTTP Hardening

Both `AnthropicBackend` and `ExternalBackend` use a shared `streamingTransport()`:

```go
// internal/backend/anthropic.go
func streamingTransport() *http.Transport {
    return &http.Transport{
        DialContext: (&net.Dialer{
            Timeout:   30 * time.Second,
            KeepAlive: 30 * time.Second,
        }).DialContext,
        TLSHandshakeTimeout:   10 * time.Second,
        ResponseHeaderTimeout: 30 * time.Second,
        IdleConnTimeout:       90 * time.Second,
    }
}
```

The `http.Client.Timeout` is set to **zero** (no deadline). This is intentional: streaming responses are unbounded in duration. The per-phase timeouts above bound connection and header phases; the body-read phase must remain unconstrained so a long generation does not time out mid-stream.

### Body draining on non-200

```go
// internal/backend/anthropic.go
if resp.StatusCode != http.StatusOK {
    body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
    if resp.StatusCode >= 400 && resp.StatusCode < 500 && len(body) > 0 {
        return nil, fmt.Errorf("chat completion: HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(body))
    }
    return nil, fmt.Errorf("chat completion: HTTP %d", resp.StatusCode)
}
```

**Why drain the body?** HTTP/1.1 Keep-Alive connection reuse requires the previous response body to be fully consumed before the connection can be returned to the pool. Abandoning the body leaks the connection.

**Why include body in 4xx errors?** A 401 from Anthropic includes a JSON message like `{"error": {"message": "invalid x-api-key"}}`. Including up to 512 bytes in the error lets callers diagnose authentication and quota issues without running a separate `curl` command.

---

## Why this design?

### Why a Backend interface instead of vendor SDKs directly?

Three reasons:

1. **Testability**: Tests inject a `MockBackend` that returns canned responses. No real API keys, no network, no flaky tests.
2. **Provider switching**: Changing from Anthropic to OpenAI is a config change, not a code change.
3. **Local model support**: `ManagedBackend` and `ExternalBackend` (pointed at Ollama) let Huginn run fully offline. The agent code is identical in both cases.

### Why compact as a separate package?

Compaction strategy is independent of transport. `ExtractiveStrategy.Compact()` does not call `Backend.ChatCompletion()` at all. `LLMStrategy.Compact()` calls it, but only to summarize — it does not need to know whether the backend is Anthropic or Ollama. This separation means:

- Compaction can be tested with a stub backend or nil backend.
- The strategy can be changed (extractive vs. LLM) without touching the HTTP layer.
- New strategies (e.g., a local summarizer) plug in via `CompactionStrategy` interface without modifying `Compactor`.

### Why extractive fallback?

LLM calls can fail. They can time out. The backend may be temporarily unavailable. In all of these cases, the agent must still be able to continue operating — it cannot simply block or crash because compaction failed. The extractive strategy provides a deterministic, zero-dependency fallback that always produces a valid (if lower quality) result.

---

## Thread Safety

| Component | Concurrency guarantee |
|-----------|----------------------|
| `AnthropicBackend` | Safe for concurrent `ChatCompletion` calls (stateless HTTP client) |
| `ExternalBackend` | Safe for concurrent `ChatCompletion` calls (stateless HTTP client) |
| `ManagedBackend` | Safe for concurrent use; `Shutdown` is idempotent |
| `StreamEvent` delivery | `OnEvent`/`OnToken` are called from a single goroutine (the SSE scanner loop); no locking needed in the callback |
| `SessionTracker` | `sync.Mutex` protects `Add`, `SessionCost`, `Breakdown`, `Reset` |
| `Compactor.MaybeCompact` | Not safe for concurrent calls with the same `Compactor` instance; caller must serialize |

---

## Limitations

- **Compaction loses nuance**: Even `LLMStrategy` summaries omit detail. Long debugging sessions with complex state may lose context that was important to the agent's reasoning. This is an inherent limitation of any summarization approach.
- **Pricing table needs manual updates**: New models require a code change to `internal/pricing/table.go` or a user-supplied override file. There is no auto-discovery of pricing from provider APIs.
- **Extended thinking is Anthropic-only**: `StreamThought` events are only emitted by `AnthropicBackend`. Other backends silently omit thinking tokens; callers that depend on them will not receive them when the provider is switched.
- **`ContextWindow()` defaults to 8192 for unknown models**: An unrecognized model ID returns the conservative default. This may cause premature compaction for local models with large windows. Override by adding the model to `modelconfig/contextwindow.go`.
- **No retry logic**: `ChatCompletion` does not retry on transient errors (429 rate limit, 503 overload). Callers are responsible for retry if needed.

---

## See Also

- [code-intelligence.md](./code-intelligence.md) — Code Intelligence Pipeline
- `swarm.md` — Multi-agent orchestration (uses `Backend` for sub-agent calls)
- `session-and-memory.md` — Session state and how compaction integrates with the conversation loop
