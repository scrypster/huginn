package backend

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ErrRateLimited is returned when the LLM backend returns HTTP 429.
// Callers should surface this distinctly in the UI (e.g. "Rate limit hit — wait N seconds").
var ErrRateLimited = errors.New("rate limited by LLM backend (HTTP 429)")

// RateLimitError carries the full HTTP 429 body for callers that want detail.
type RateLimitError struct {
	Body       string        // up to 512 bytes of the response body
	RetryAfter time.Duration // parsed from Retry-After header; 0 if not present
}

func (e *RateLimitError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("rate limited by LLM backend (HTTP 429): %s", e.Body)
	}
	return ErrRateLimited.Error()
}

// Is makes errors.Is(err, backend.ErrRateLimited) return true for *RateLimitError.
func (e *RateLimitError) Is(target error) bool {
	return target == ErrRateLimited
}

// IsRateLimited reports whether err is a rate-limit error from the LLM backend.
func IsRateLimited(err error) bool {
	return errors.Is(err, ErrRateLimited)
}

// httpClientInterface is satisfied by *http.Client; used for mocking in tests.
type httpClientInterface interface {
	Do(req *http.Request) (*http.Response, error)
}

// ContentPart represents a single part of a message (text, image, etc.)
type ContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

// Message is a single turn in a conversation.
type Message struct {
	Role       string     // "system", "user", "assistant", "tool"
	Content    string
	Parts      []ContentPart // set when message contains images or multiple parts
	ToolCalls  []ToolCall // set when Role=="assistant" and model called tools
	ToolName   string     // set when Role=="tool" (result message)
	ToolCallID string     // correlates tool result to the originating call
}

// ToolProperty describes a single parameter in a tool's schema.
type ToolProperty struct {
	Type        string `json:"type"`        // "string", "integer", "boolean", "number", "array"
	Description string `json:"description,omitempty"`
}

// ToolParameters is the JSON Schema for a tool's arguments.
type ToolParameters struct {
	Type       string                  `json:"type"`                 // always "object"
	Properties map[string]ToolProperty `json:"properties,omitempty"`
	Required   []string                `json:"required,omitempty"`
}

// ToolFunction holds the callable specification.
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  ToolParameters `json:"parameters"`
}

// Tool is the schema sent to the model to describe an available function.
type Tool struct {
	Type     string       `json:"type"` // always "function"
	Function ToolFunction `json:"function"`
}

// ToolCallFunction holds the model's invocation of a specific function.
type ToolCallFunction struct {
	Name      string         // function name
	Arguments map[string]any // parsed from JSON
}

// ToolCall is a single tool invocation returned by the model.
type ToolCall struct {
	ID       string
	Function ToolCallFunction
}

// StreamEventType identifies the kind of streaming event.
type StreamEventType string

const (
	StreamText       StreamEventType = "text"
	StreamThought    StreamEventType = "thought" // Anthropic extended thinking tokens
	StreamDone       StreamEventType = "done"
	StreamWarning    StreamEventType = "warning"     // non-fatal warnings surfaced to the user
	StreamToolCall   StreamEventType = "tool_call"   // tool invocation started
	StreamToolResult StreamEventType = "tool_result" // tool invocation completed
)

// StreamEvent is a single event emitted during streaming.
type StreamEvent struct {
	Type    StreamEventType
	Content string         // text for token/warning events; empty for tool events
	Payload map[string]any // structured data for tool events; nil for text events
}

// NewToolCallEvent constructs a tool_call StreamEvent.
// This is the only authorised constructor — keeps the wire shape in one place.
func NewToolCallEvent(toolName string) StreamEvent {
	return StreamEvent{
		Type:    StreamToolCall,
		Payload: map[string]any{"tool": toolName},
	}
}

// NewToolResultEvent constructs a tool_result StreamEvent.
// This is the only authorised constructor — keeps the wire shape in one place.
func NewToolResultEvent(toolName string, success bool) StreamEvent {
	return StreamEvent{
		Type:    StreamToolResult,
		Payload: map[string]any{"tool": toolName, "success": success},
	}
}

// ChatRequest is the input to Backend.ChatCompletion.
type ChatRequest struct {
	Model    string
	Messages []Message
	Tools    []Tool                // nil = no tool calling
	OnToken  func(string)           // backward compat; nil = collect
	OnEvent  func(StreamEvent)      // richer streaming; nil = use OnToken
}

// ChatResponse is the output of Backend.ChatCompletion.
type ChatResponse struct {
	Content          string
	ToolCalls        []ToolCall
	DoneReason       string // "stop", "tool_calls", "length"
	PromptTokens     int
	CompletionTokens int
	// ParseErrors contains non-fatal SSE parse errors (e.g. malformed tool call JSON).
	// Non-empty means some tool calls may have been silently dropped.
	ParseErrors []string
}

// StatusReporter is an optional interface implemented by backends that support
// circuit-breaker state reporting. Callers should type-assert rather than
// relying on the Backend interface, since most backends have no circuit breaker.
//
// Return values: "closed" (normal), "open" (upstream degraded), "half-open" (probing).
type StatusReporter interface {
	BackendStatus() string
}

// KeyResolver resolves an API key on demand. The key lives on the stack only
// during the HTTP request — it is never stored in the backend struct.
type KeyResolver func() (string, error)

// Backend is the single interface the rest of Huginn uses to call an LLM.
// Implementations: ExternalBackend (any OpenAI-compatible endpoint),
// ManagedBackend (huginn owns the llama-server subprocess).
type Backend interface {
	ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	Health(ctx context.Context) error
	Shutdown(ctx context.Context) error // no-op for ExternalBackend
	ContextWindow() int                  // model's context window in tokens
}
