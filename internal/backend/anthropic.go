package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/modelconfig"
)

// streamingTransport returns an *http.Transport configured for long-lived SSE
// streaming connections. It bounds the connection-establishment and header
// phases so a stalled server never hangs the caller indefinitely, while
// leaving the body-read phase unconstrained (http.Client.Timeout must remain
// 0 at the call site).
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

const (
	anthropicDefaultEndpoint = "https://api.anthropic.com"
	anthropicAPIVersion      = "2023-06-01"
	anthropicBeta            = "interleaved-thinking-2025-05-14"
)

// streamStallTimeout is the maximum time the SSE scanner may be idle (no
// non-empty line received) before the stream is aborted. This prevents the
// scan loop from hanging indefinitely when a server returns 200 OK and then
// stops sending data without closing the connection.
//
// Declared as a var (not const) so tests in this package can lower the value
// to exercise the idle-timeout path without waiting 60 seconds.
var streamStallTimeout = 60 * time.Second

// AnthropicBackend calls the Anthropic Messages API.
// It is safe for concurrent use.
type AnthropicBackend struct {
	endpoint        string          // base URL, e.g. "https://api.anthropic.com"
	client          *http.Client
	keyResolver     KeyResolver     // resolves the API key on each request
	model           string          // configured model name
	maxOutputTokens int             // override max_tokens when non-zero
	cb              *circuitBreaker // guards against sustained upstream degradation
}

// NewAnthropicBackend creates an AnthropicBackend with the default endpoint.
func NewAnthropicBackend(resolver KeyResolver, model string) *AnthropicBackend {
	return NewAnthropicBackendWithEndpoint(resolver, model, anthropicDefaultEndpoint)
}

// NewAnthropicBackendWithEndpoint creates an AnthropicBackend with a custom endpoint.
func NewAnthropicBackendWithEndpoint(resolver KeyResolver, model, endpoint string) *AnthropicBackend {
	return &AnthropicBackend{
		endpoint:    strings.TrimRight(endpoint, "/"),
		client:      &http.Client{Timeout: 0, Transport: streamingTransport()},
		keyResolver: resolver,
		model:       model,
		cb:          newCircuitBreaker(),
	}
}

// BackendStatus implements StatusReporter for circuit-breaker state reporting.
// Returns "closed" (normal), "open" (degraded), or "half-open" (probing).
func (b *AnthropicBackend) BackendStatus() string { return b.cb.State() }

// ContextWindow returns the known context window for the configured model.
// Anthropic models default to 200,000 if unknown.
func (b *AnthropicBackend) ContextWindow() int {
	cw := modelconfig.ContextWindowForModel(b.model)
	// Anthropic models have a default of 200k, not 8k
	if cw == modelconfig.DefaultContextWindow() {
		return 200_000
	}
	return cw
}

const (
	// chatMaxRetries is the maximum number of additional attempts after the
	// first for transient 5xx responses. Total attempts = 1 + chatMaxRetries.
	chatMaxRetries = 2
	// chatRetryBase is the initial backoff delay before the first retry.
	chatRetryBase = 100 * time.Millisecond
)

// ChatCompletion sends a chat request and returns the response.
// If req.OnEvent is set, streaming events are emitted.
// Transient 5xx errors (excluding 529) are retried up to chatMaxRetries times
// with exponential backoff and jitter. 4xx errors and rate-limit errors are
// returned immediately without retrying.
func (b *AnthropicBackend) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	apiKey, err := b.keyResolver()
	if err != nil {
		return nil, fmt.Errorf("chat completion: resolve api key: %w", err)
	}

	body, err := b.buildRequest(req)
	if err != nil {
		return nil, err
	}

	if !b.cb.Allow() {
		return nil, ErrCircuitOpen
	}

	var lastErr error
	for attempt := 0; attempt <= chatMaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff with ±25% jitter: 100ms, 200ms, 400ms, …
			delay := chatRetryBase * (1 << uint(attempt-1))
			jitter := time.Duration(rand.Int63n(int64(delay / 4)))
			if rand.Intn(2) == 0 {
				jitter = -jitter
			}
			select {
			case <-time.After(delay + jitter):
			case <-ctx.Done():
				return nil, fmt.Errorf("chat completion: %w", ctx.Err())
			}
			slog.Warn("chat completion: retrying after transient error",
				"attempt", attempt, "err", lastErr)
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			b.endpoint+"/v1/messages", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("x-api-key", apiKey)
		httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
		httpReq.Header.Set("anthropic-beta", anthropicBeta)
		httpReq.Header.Set("Accept", "text/event-stream")

		resp, err := b.client.Do(httpReq)
		if err != nil {
			// Network errors are retryable (e.g. connection reset).
			lastErr = fmt.Errorf("chat completion: %w", err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			// Read and discard the body so the TCP connection can be reused (Keep-Alive).
			// For 4xx responses, include up to 512 bytes of the body in the error so
			// callers can diagnose issues like invalid API keys without a separate curl.
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			if resp.StatusCode == http.StatusTooManyRequests {
				// Surface rate-limit errors as a typed ErrRateLimited so callers and the TUI
				// can display a clear "rate limited" message rather than a generic HTTP error.
				rl := &RateLimitError{Body: string(bytes.TrimSpace(errBody))}
				rl.RetryAfter = parseRetryAfter(resp.Header)
				return nil, rl
			}
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				// 4xx client errors are not retryable.
				if len(errBody) > 0 {
					return nil, fmt.Errorf("chat completion: HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(errBody))
				}
				return nil, fmt.Errorf("chat completion: HTTP %d", resp.StatusCode)
			}
			// 5xx server errors are transient — retry with backoff.
			lastErr = fmt.Errorf("chat completion: HTTP %d", resp.StatusCode)
			continue
		}

		result, err := b.parseSSE(ctx, resp, req)
		if err != nil {
			b.cb.RecordFailure()
			return nil, err
		}
		b.cb.RecordSuccess()
		return result, nil
	}

	b.cb.RecordFailure()
	return nil, lastErr
}

// Health checks that the endpoint is reachable.
func (b *AnthropicBackend) Health(ctx context.Context) error {
	apiKey, err := b.keyResolver()
	if err != nil {
		return fmt.Errorf("health check: resolve api key: %w", err)
	}
	hc := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.endpoint+"/v1/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", apiKey)
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("health check: authentication failed (401)")
	case http.StatusForbidden:
		return fmt.Errorf("health check: forbidden (403)")
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("health check: HTTP %d", resp.StatusCode)
	}
	return nil
}

// SetMaxOutputTokens overrides the max_tokens sent on every request.
// A value of 0 (default) uses the built-in safe default (8096).
func (b *AnthropicBackend) SetMaxOutputTokens(n int) {
	b.maxOutputTokens = n
}

// Shutdown is a no-op for AnthropicBackend (we don't own the service).
func (b *AnthropicBackend) Shutdown(_ context.Context) error { return nil }

// compile-time interface check
var _ Backend = (*AnthropicBackend)(nil)

// --- private helpers ---

// anthropicRequest is the JSON body for /v1/messages.
type anthropicRequest struct {
	Model    string              `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System   string              `json:"system,omitempty"`
	Messages []anthropicMessage  `json:"messages"`
	Tools    []anthropicTool     `json:"tools,omitempty"`
	Stream   bool                `json:"stream"`
}

type anthropicMessage struct {
	Role    string        `json:"role"`
	Content any           `json:"content"` // string or []anthropicContentBlock
}

type anthropicContentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`        // tool_use id
	Name      string         `json:"name,omitempty"`      // tool_use name
	Input     map[string]any `json:"input,omitempty"`     // tool_use input
	ToolUseID string         `json:"tool_use_id,omitempty"` // tool_result tool_use_id
	Content   string         `json:"content,omitempty"`   // tool_result content
}

type anthropicTool struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	InputSchema json.RawMessage     `json:"input_schema"`
}

// anthropicToolUseBlock is used exclusively for serializing tool_use content blocks.
// It does NOT use omitempty on Input because Anthropic requires the "input" field
// to always be present, even for zero-argument tools (omitempty drops empty maps).
type anthropicToolUseBlock struct {
	Type  string         `json:"type"`
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"` // always serialized, never omitted
}

func (b *AnthropicBackend) buildRequest(req ChatRequest) ([]byte, error) {
	// Extract system message if present
	var systemContent string
	msgs := make([]anthropicMessage, 0)

	for _, m := range req.Messages {
		if m.Role == "system" {
			systemContent = m.Content
			continue
		}

		am := anthropicMessage{Role: m.Role}

		// Handle role-specific content formatting
		if m.Role == "user" {
			am.Content = m.Content
		} else if m.Role == "assistant" {
			// Assistant messages may have text or tool_use content blocks
			if len(m.ToolCalls) > 0 {
				blocks := make([]anthropicToolUseBlock, 0, len(m.ToolCalls))
				for _, tc := range m.ToolCalls {
					// Always provide a non-nil input map for tool_use blocks.
					// Anthropic requires "input" to be present even for zero-arg tools.
					// anthropicToolUseBlock has no omitempty on Input so {} serializes
					// as "input":{} rather than being silently dropped.
					input := tc.Function.Arguments
					if input == nil {
						input = map[string]any{}
					}
					blocks = append(blocks, anthropicToolUseBlock{
						Type:  "tool_use",
						ID:    tc.ID,
						Name:  tc.Function.Name,
						Input: input,
					})
				}
				am.Content = blocks
			} else {
				am.Content = m.Content
			}
		} else if m.Role == "tool" {
			// Tool result messages are sent as user messages with tool_result content blocks
			am.Role = "user"
			block := anthropicContentBlock{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   m.Content,
			}
			am.Content = []anthropicContentBlock{block}
		} else {
			am.Content = m.Content
		}

		msgs = append(msgs, am)
	}

	// Convert tools to Anthropic format
	tools := make([]anthropicTool, 0, len(req.Tools))
	for _, t := range req.Tools {
		schema, err := json.Marshal(t.Function.Parameters)
		if err != nil {
			return nil, fmt.Errorf("marshal tool parameters: %w", err)
		}
		tools = append(tools, anthropicTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: schema,
		})
	}

	// Build request — use model registry for default, override if explicitly set.
	maxTok := modelconfig.MaxOutputTokensForModel(req.Model)
	if b.maxOutputTokens > 0 {
		maxTok = b.maxOutputTokens
	}
	r := anthropicRequest{
		Model:     req.Model,
		MaxTokens: maxTok,
		System:    systemContent,
		Messages:  msgs,
		Tools:     tools,
		Stream:    true,
	}
	return json.Marshal(r)
}

// toolBlockState tracks tool call blocks across SSE events.
type toolBlockState struct {
	id          string
	name        string
	partialJSON string
}

// parseSSE parses the Anthropic SSE stream and returns the ChatResponse.
//
// Idle stall detection: if no non-empty SSE line is received within
// streamStallTimeout, the underlying response body is closed and the scan loop
// exits. This prevents the scanner from blocking indefinitely when the server
// returns 200 OK but stops sending data without closing the connection.
func (b *AnthropicBackend) parseSSE(ctx context.Context, resp *http.Response, req ChatRequest) (*ChatResponse, error) {
	// streamCtx is cancelled either by the parent ctx or by the idle-timeout
	// watchdog goroutine below when no data arrives within streamStallTimeout.
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	// activityCh receives a signal every time a non-empty SSE line is processed.
	// The watchdog goroutine resets its idle timer on each signal.
	activityCh := make(chan struct{}, 1)

	// Watchdog: cancel streamCtx (and therefore close resp.Body) if no activity
	// is observed within streamStallTimeout. Capture the timeout once so the
	// goroutine never races against test code mutating the global.
	stallTimeout := streamStallTimeout
	go func() {
		timer := time.NewTimer(stallTimeout)
		defer timer.Stop()
		for {
			select {
			case <-streamCtx.Done():
				return
			case <-activityCh:
				// Reset the idle timer on every received event.
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(stallTimeout)
			case <-timer.C:
				// No activity for stallTimeout — abort the stream.
				slog.Warn("anthropic: SSE stream idle timeout, aborting",
					"timeout", stallTimeout)
				// Cancel the context BEFORE closing the body so that by the
				// time the scanner sees an I/O error, streamCtx.Err() is
				// already set and the caller can distinguish an idle-timeout
				// abort from a genuine network error.
				streamCancel()
				resp.Body.Close()
				return
			}
		}
	}()

	result := &ChatResponse{}
	toolBlocks := make(map[int]*toolBlockState) // index -> block state

	var currentEventType string
	scanner := bufio.NewScanner(resp.Body)

	for scanner.Scan() {
		line := scanner.Text()

		// Track event type from "event: X" lines
		if strings.HasPrefix(line, "event: ") {
			currentEventType = strings.TrimPrefix(line, "event: ")
			// Signal activity so the watchdog resets its idle timer.
			select {
			case activityCh <- struct{}{}:
			default:
			}
			continue
		}

		// Parse data lines
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "" {
			continue
		}

		// Signal activity so the watchdog resets its idle timer.
		select {
		case activityCh <- struct{}{}:
		default:
		}

		// Parse based on event type
		switch currentEventType {
		case "content_block_start":
			b.parseContentBlockStart(data, toolBlocks)
		case "content_block_delta":
			b.parseContentBlockDelta(data, result, req, toolBlocks)
		case "content_block_stop":
			b.parseContentBlockStop(data, result, toolBlocks)
		case "message_start":
			b.parseMessageStart(data, result)
		case "message_delta":
			b.parseMessageDelta(data, result)
		case "message_stop":
			// Stream is ending
		}
	}

	// Emit StreamDone event if OnEvent is set
	if req.OnEvent != nil {
		req.OnEvent(StreamEvent{Type: StreamDone})
	}

	if err := scanner.Err(); err != nil {
		// If the stream was aborted due to an idle timeout, return a more
		// descriptive error rather than the raw I/O error from the closed body.
		if streamCtx.Err() != nil {
			return nil, fmt.Errorf("SSE stream aborted: idle timeout after %s", stallTimeout)
		}
		return nil, fmt.Errorf("reading SSE stream: %w", err)
	}

	// If the scanner finished cleanly but the stream context was cancelled due
	// to the idle watchdog, surface that as an error.
	if streamCtx.Err() != nil {
		return nil, fmt.Errorf("SSE stream aborted: idle timeout after %s", stallTimeout)
	}

	return result, nil
}

func (b *AnthropicBackend) parseContentBlockStart(data string, toolBlocks map[int]*toolBlockState) {
	var event map[string]any
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return
	}

	index, ok := event["index"].(float64)
	if !ok {
		return
	}

	contentBlock, ok := event["content_block"].(map[string]any)
	if !ok {
		return
	}

	blockType, _ := contentBlock["type"].(string)
	if blockType != "tool_use" {
		return
	}

	id, _ := contentBlock["id"].(string)
	name, _ := contentBlock["name"].(string)

	toolBlocks[int(index)] = &toolBlockState{
		id:          id,
		name:        name,
		partialJSON: "",
	}
}

func (b *AnthropicBackend) parseContentBlockDelta(data string, result *ChatResponse, req ChatRequest, toolBlocks map[int]*toolBlockState) {
	var event map[string]any
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return
	}

	delta, ok := event["delta"].(map[string]any)
	if !ok {
		return
	}

	deltaType, _ := delta["type"].(string)

	switch deltaType {
	case "text_delta":
		text, _ := delta["text"].(string)
		result.Content += text
		if req.OnEvent != nil {
			req.OnEvent(StreamEvent{Type: StreamText, Content: text})
		}
		// Always call OnToken when set — even when OnEvent is also set — so that
		// callers relying on OnToken for content accumulation and WS "token"
		// messages receive every text chunk. The external (OpenAI/Ollama) backend
		// already does this; the Anthropic backend was using else-if which caused
		// OnToken to be silently skipped, resulting in empty assistant messages
		// after tool-call turns (no content persisted, no tokens streamed to UI).
		if req.OnToken != nil {
			req.OnToken(text)
		}

	case "thinking_delta":
		thinking, _ := delta["thinking"].(string)
		if req.OnEvent != nil {
			req.OnEvent(StreamEvent{Type: StreamThought, Content: thinking})
		}

	case "input_json_delta":
		partialJSON, _ := delta["partial_json"].(string)
		index, ok := event["index"].(float64)
		if !ok {
			return
		}
		block, ok := toolBlocks[int(index)]
		if ok {
			block.partialJSON += partialJSON
		}
	}
}

func (b *AnthropicBackend) parseContentBlockStop(data string, result *ChatResponse, toolBlocks map[int]*toolBlockState) {
	var event map[string]any
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		slog.Warn("anthropic: parseContentBlockStop: malformed event JSON", "err", err)
		return
	}

	index, ok := event["index"].(float64)
	if !ok {
		return
	}

	block, ok := toolBlocks[int(index)]
	if !ok {
		return
	}

	// Parse the accumulated JSON as the tool arguments.
	// When a tool takes no parameters Anthropic emits no input_json_delta
	// events, leaving partialJSON empty. Treat "" as "{}" (empty args) so
	// zero-argument tools (e.g. muninn_where_left_off) are not dropped.
	//
	// When partialJSON is non-empty but fails to parse (truncated SSE stream),
	// fall back to "{}" so zero-arg tools still execute rather than being silently
	// dropped. Tools that genuinely require parameters will fail at the server level
	// with a descriptive error, which the LLM can handle; silent drops cannot.
	raw := block.partialJSON
	if raw == "" {
		raw = "{}"
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		slog.Warn("anthropic: SSE tool call args truncated, retrying with empty args",
			"tool", block.name, "id", block.id, "partial_len", len(raw), "err", err)
		args = map[string]any{}
	}

	// Append to ToolCalls
	tc := ToolCall{
		ID: block.id,
		Function: ToolCallFunction{
			Name:      block.name,
			Arguments: args,
		},
	}
	result.ToolCalls = append(result.ToolCalls, tc)

	// Clean up
	delete(toolBlocks, int(index))
}

func (b *AnthropicBackend) parseMessageStart(data string, result *ChatResponse) {
	var event map[string]any
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return
	}

	message, ok := event["message"].(map[string]any)
	if !ok {
		return
	}

	usage, ok := message["usage"].(map[string]any)
	if !ok {
		return
	}

	if inputTokens, ok := usage["input_tokens"].(float64); ok {
		result.PromptTokens = int(inputTokens)
	}
}

func (b *AnthropicBackend) parseMessageDelta(data string, result *ChatResponse) {
	var event map[string]any
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return
	}

	// Extract stop_reason from delta
	delta, ok := event["delta"].(map[string]any)
	if ok {
		if stopReason, ok := delta["stop_reason"].(string); ok {
			result.DoneReason = stopReason
		}
	}

	// Extract output tokens from usage
	usage, ok := event["usage"].(map[string]any)
	if ok {
		if outputTokens, ok := usage["output_tokens"].(float64); ok {
			result.CompletionTokens = int(outputTokens)
		}
	}
}

// maxRetryAfter is the maximum duration we will honour from a Retry-After header.
const maxRetryAfter = 60 * time.Second

// parseRetryAfter parses the Retry-After header from h and returns its value as
// a duration. Supports both integer-seconds ("30") and HTTP-date forms.
// Returns 0 if the header is absent, cannot be parsed, or is in the past.
// Caps the result at maxRetryAfter to prevent excessive backoff.
func parseRetryAfter(h http.Header) time.Duration {
	ra := h.Get("Retry-After")
	if ra == "" {
		return 0
	}
	// Try integer seconds first.
	if secs, err := strconv.Atoi(ra); err == nil {
		d := time.Duration(secs) * time.Second
		if d <= 0 {
			return 0
		}
		if d > maxRetryAfter {
			return maxRetryAfter
		}
		return d
	}
	// Try HTTP-date form.
	if t, err := http.ParseTime(ra); err == nil {
		d := time.Until(t)
		if d <= 0 {
			return 0
		}
		if d > maxRetryAfter {
			return maxRetryAfter
		}
		return d
	}
	return 0
}

