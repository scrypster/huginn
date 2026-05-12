package backend

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// toolBlockState tracks tool call blocks across SSE events.
type toolBlockState struct {
	id          string
	name        string
	partialJSON string
}

// parseAnthropicSSE parses an Anthropic Messages-API SSE stream and returns
// the assembled ChatResponse. The stream may originate from api.anthropic.com
// or from Google Vertex AI's anthropic-on-vertex endpoint — both use the same
// event schema. stallTimeout is the maximum allowed idle gap between non-empty
// SSE lines before the stream is aborted.
func parseAnthropicSSE(ctx context.Context, resp *http.Response, req ChatRequest, stallTimeout time.Duration) (*ChatResponse, error) {
	// streamCtx is cancelled either by the parent ctx or by the idle-timeout
	// watchdog goroutine below when no data arrives within stallTimeout.
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	// activityCh receives a signal every time a non-empty SSE line is processed.
	// The watchdog goroutine resets its idle timer on each signal.
	activityCh := make(chan struct{}, 1)

	// Watchdog: cancel streamCtx (and therefore close resp.Body) if no activity
	// is observed within stallTimeout. stallTimeout is already captured by the
	// caller so the goroutine never races against test code mutating the global.
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
			parseAnthropicContentBlockStart(data, toolBlocks)
		case "content_block_delta":
			parseAnthropicContentBlockDelta(data, result, req, toolBlocks)
		case "content_block_stop":
			parseAnthropicContentBlockStop(data, result, toolBlocks)
		case "message_start":
			parseAnthropicMessageStart(data, result)
		case "message_delta":
			parseAnthropicMessageDelta(data, result)
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

func parseAnthropicContentBlockStart(data string, toolBlocks map[int]*toolBlockState) {
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

func parseAnthropicContentBlockDelta(data string, result *ChatResponse, req ChatRequest, toolBlocks map[int]*toolBlockState) {
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

func parseAnthropicContentBlockStop(data string, result *ChatResponse, toolBlocks map[int]*toolBlockState) {
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

func parseAnthropicMessageStart(data string, result *ChatResponse) {
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

func parseAnthropicMessageDelta(data string, result *ChatResponse) {
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
