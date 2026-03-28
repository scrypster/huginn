package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/modelconfig"
)

// externalStreamStallTimeout is the maximum time the SSE scanner may be idle
// (no non-empty line received) before the stream is aborted. Declared as a var
// so tests in this package can lower the value to exercise the timeout path.
var externalStreamStallTimeout = 60 * time.Second

// externalModelLoadStatusDelay is the time to wait for a response header before
// emitting a StreamStatus event to the client. Local model servers (e.g. Ollama)
// load the model before sending the first header; 10 s is unobtrusive for
// warm-cache loads (typically < 3 s) while providing early feedback for cold
// loads. Declared as a var so tests can lower the value.
var externalModelLoadStatusDelay = 10 * time.Second

// externalStreamingTransport returns an *http.Transport configured for
// external/local LLM endpoints (OpenAI-compatible, e.g. Ollama).
//
// It uses a longer ResponseHeaderTimeout (120 s) than the Anthropic transport
// because local model servers may spend 30–120+ seconds loading the model
// before sending the first response header.
//
// This function is self-contained and does not share state with
// streamingTransport(); every call returns a fresh *http.Transport.
func externalStreamingTransport() *http.Transport {
	return &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second,
		IdleConnTimeout:       90 * time.Second,
	}
}

// ExternalBackend calls any OpenAI-compatible /v1/chat/completions endpoint.
// It is safe for concurrent use.
type ExternalBackend struct {
	endpoint    string       // base URL, e.g. "http://localhost:11434"
	client      *http.Client
	model       string       // configured model name
	keyResolver KeyResolver  // optional; resolves API key sent as Bearer token
}

// NewExternalBackend creates an ExternalBackend pointing at endpoint.
func NewExternalBackend(endpoint string) *ExternalBackend {
	return &ExternalBackend{
		endpoint: strings.TrimRight(endpoint, "/"),
		client:   &http.Client{Timeout: 0, Transport: externalStreamingTransport()},
	}
}

// NewExternalBackendWithAPIKey creates an ExternalBackend that sends a Bearer token.
func NewExternalBackendWithAPIKey(endpoint string, resolver KeyResolver) *ExternalBackend {
	b := NewExternalBackend(endpoint)
	b.keyResolver = resolver
	return b
}

// SetModel sets the model identifier used by ContextWindow.
// It must be called before any concurrent use of this backend begins.
// The model name is used by ContextWindow() to look up the context window size
// from the modelconfig package.
func (b *ExternalBackend) SetModel(m string) {
	b.model = m
}

// ContextWindow returns the known context window for the configured model.
// Delegates to modelconfig.ContextWindowForModel for the lookup.
func (b *ExternalBackend) ContextWindow() int {
	return modelconfig.ContextWindowForModel(b.model)
}

// ChatCompletion sends a chat request and returns the response.
// If req.OnToken is set, tokens are streamed; otherwise response is collected.
// If req.OnEvent is set, streaming events are emitted; OnToken is called for backward compat if also set.
func (b *ExternalBackend) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	body, err := b.buildRequest(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		b.endpoint+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if b.keyResolver != nil {
		apiKey, err := b.keyResolver()
		if err != nil {
			return nil, fmt.Errorf("chat completion: resolve api key: %w", err)
		}
		if apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		}
	}

	// Measure how long the server takes to send the first response header.
	// If it exceeds externalModelLoadStatusDelay (e.g. Ollama loading a model cold),
	// emit a StreamStatus event synchronously before parsing begins so the client
	// sees "Loading model…" immediately before the first token — never after.
	// Firing after client.Do (not via a goroutine) guarantees correct ordering
	// with no goroutine-scheduling race.
	headerStart := time.Now()
	resp, err := b.client.Do(httpReq)
	if err == nil && req.OnEvent != nil && time.Since(headerStart) >= externalModelLoadStatusDelay {
		req.OnEvent(StreamEvent{
			Type:    StreamStatus,
			Content: "Loading model, please wait...",
		})
	}
	if err != nil {
		return nil, fmt.Errorf("chat completion: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, &RateLimitError{Body: string(bytes.TrimSpace(body))}
		}
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && len(body) > 0 {
			return nil, fmt.Errorf("chat completion: HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(body))
		}
		return nil, fmt.Errorf("chat completion: HTTP %d", resp.StatusCode)
	}

	return b.parseSSE(ctx, resp, req)
}

// Health checks that the endpoint is reachable.
func (b *ExternalBackend) Health(ctx context.Context) error {
	hc := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.endpoint+"/v1/models", nil)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("health check: HTTP %d", resp.StatusCode)
	}
	return nil
}

// Shutdown is a no-op for ExternalBackend (we don't own the server process).
func (b *ExternalBackend) Shutdown(_ context.Context) error { return nil }

// compile-time interface check
var _ Backend = (*ExternalBackend)(nil)

// --- private helpers ---

// openAIRequest is the JSON body for /v1/chat/completions.
type openAIRequest struct {
	Model    string          `json:"model"`
	Messages []openAIMessage `json:"messages"`
	Tools    []Tool          `json:"tools,omitempty"`
	Stream   bool            `json:"stream"`
}

type openAIContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content,omitempty"` // can be string or []openAIContentPart
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"` // for role=tool
}

type openAIToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function openAIToolCallFunction `json:"function"`
}

type openAIToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // raw JSON string
}

func (b *ExternalBackend) buildRequest(req ChatRequest) ([]byte, error) {
	msgs := make([]openAIMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		om := openAIMessage{Role: m.Role}
		// Set content: either Parts array or plain string
		if len(m.Parts) > 0 {
			parts := make([]openAIContentPart, len(m.Parts))
			for i, p := range m.Parts {
				parts[i] = openAIContentPart{Type: p.Type, Text: p.Text, ImageURL: p.ImageURL}
			}
			om.Content = parts
		} else {
			om.Content = m.Content
		}
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				argBytes, err := json.Marshal(tc.Function.Arguments)
				if err != nil {
					return nil, fmt.Errorf("marshal tool arguments: %w", err)
				}
				om.ToolCalls = append(om.ToolCalls, openAIToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: openAIToolCallFunction{
						Name:      tc.Function.Name,
						Arguments: string(argBytes),
					},
				})
			}
			om.Content = "" // content and tool_calls are mutually exclusive
		}
		if m.Role == "tool" {
			om.ToolCallID = m.ToolCallID
			om.Name = m.ToolName
		}
		msgs = append(msgs, om)
	}
	r := openAIRequest{
		Model:    req.Model,
		Messages: msgs,
		Tools:    req.Tools,
		Stream:   true,
	}
	return json.Marshal(r)
}

// sseToolCall is the per-chunk tool call fragment in the SSE stream.
// It has a string Arguments (raw JSON fragment) and an Index for multi-tool ordering.
type sseToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // incremental JSON fragment
	} `json:"function"`
}

// sseChunk is the SSE streaming response shape.
type sseChunk struct {
	Choices []struct {
		Delta struct {
			Content   string        `json:"content"`
			ToolCalls []sseToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage,omitempty"`
}

func (b *ExternalBackend) parseSSE(ctx context.Context, resp *http.Response, req ChatRequest) (*ChatResponse, error) {
	// streamCtx is cancelled either by the parent ctx or by the idle-timeout
	// watchdog goroutine when no SSE data arrives within externalStreamStallTimeout.
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	// activityCh is signalled on every non-empty SSE data line so the watchdog
	// can reset its idle timer.
	activityCh := make(chan struct{}, 1)

	// Watchdog: abort the stream if no activity is seen within externalStreamStallTimeout.
	// Captures the timeout once to avoid races against test code mutating the global.
	stallTimeout := externalStreamStallTimeout
	go func() {
		timer := time.NewTimer(stallTimeout)
		defer timer.Stop()
		for {
			select {
			case <-streamCtx.Done():
				return
			case <-activityCh:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(stallTimeout)
			case <-timer.C:
				slog.Warn("external: SSE stream idle timeout, aborting", "timeout", stallTimeout)
				resp.Body.Close()
				streamCancel()
				return
			}
		}
	}()

	result := &ChatResponse{}
	// accumulate tool call fragments indexed by wire index field
	tcFragments := map[int]*ToolCall{}
	// accumulate raw argument JSON fragments per tool call index
	argsBuilders := map[int]*strings.Builder{}

	sawDone := false

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
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

		if data == "[DONE]" {
			sawDone = true
			break
		}

		var chunk sseChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // skip malformed chunks
		}

		// Token usage (when present in the chunk) — extract BEFORE checking choices
		// OpenAI sends a final chunk with empty choices but populated usage
		if chunk.Usage != nil {
			result.PromptTokens = chunk.Usage.PromptTokens
			result.CompletionTokens = chunk.Usage.CompletionTokens
		}

		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]

		// Text token
		if choice.Delta.Content != "" {
			result.Content += choice.Delta.Content
			// Emit StreamEvent if OnEvent is set
			if req.OnEvent != nil {
				req.OnEvent(StreamEvent{Type: StreamText, Content: choice.Delta.Content})
			}
			// Call OnToken for backward compat (always, regardless of OnEvent)
			if req.OnToken != nil {
				req.OnToken(choice.Delta.Content)
			}
		}

		// Tool call fragments — use tc.Index (wire index) not range index i
		for _, tc := range choice.Delta.ToolCalls {
			idx := tc.Index
			if existing, ok := tcFragments[idx]; ok {
				if tc.Function.Name != "" {
					existing.Function.Name += tc.Function.Name
				}
			} else {
				tcFragments[idx] = &ToolCall{
					ID:       tc.ID,
					Function: ToolCallFunction{Name: tc.Function.Name},
				}
			}
			// Accumulate argument fragments
			if tc.Function.Arguments != "" {
				if _, ok := argsBuilders[idx]; !ok {
					argsBuilders[idx] = &strings.Builder{}
				}
				argsBuilders[idx].WriteString(tc.Function.Arguments)
			}
		}

		// Finish reason
		if choice.FinishReason != nil {
			result.DoneReason = *choice.FinishReason
		}
	}

	// Emit StreamDone event after stream completes
	if req.OnEvent != nil {
		req.OnEvent(StreamEvent{Type: StreamDone})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading SSE stream: %w", err)
	}

	// Fix 2: detect premature EOF — no [DONE] and no data received
	if !sawDone && result.Content == "" && len(tcFragments) == 0 {
		return nil, fmt.Errorf("SSE stream ended without data")
	}

	// Flatten accumulated tool calls in wire-index order, parsing arguments
	indices := make([]int, 0, len(tcFragments))
	for idx := range tcFragments {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	for _, idx := range indices {
		tc := tcFragments[idx]
		if ab, ok := argsBuilders[idx]; ok {
			raw := ab.String()
			if raw != "" {
				var args map[string]any
				if err := json.Unmarshal([]byte(raw), &args); err != nil {
					return nil, fmt.Errorf("parse tool %q arguments: %w", tc.Function.Name, err)
				}
				tc.Function.Arguments = args
			}
		}
		result.ToolCalls = append(result.ToolCalls, *tc)
	}

	return result, nil
}
