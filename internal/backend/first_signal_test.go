package backend

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestContentToolCallTokenGate_ToolJSON_EmitsUsingToolsStatusOnce verifies
// the perf-wave 2c first-signal fix: a stream that resolves to a leading
// tool-call JSON prefix (never painted as prose) still emits a "using
// tools" StreamStatus event the moment the gate starts holding — not
// silence all the way to Finish.
func TestContentToolCallTokenGate_ToolJSON_EmitsUsingToolsStatusOnce(t *testing.T) {
	var events []StreamEvent
	var mu sync.Mutex
	gate := NewContentToolCallTokenGate(nil, func(ev StreamEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, ev)
	})
	for _, r := range qwen14bContentJSON {
		gate.Push(string(r))
	}
	gate.Finish("")

	mu.Lock()
	defer mu.Unlock()
	var statusCount int
	for _, ev := range events {
		if ev.Type == StreamStatus {
			statusCount++
			if ev.Content != "using tools" {
				t.Errorf("unexpected status content: %q", ev.Content)
			}
		}
		if ev.Type == StreamText {
			t.Errorf("pure tool JSON must never emit StreamText, got %q", ev.Content)
		}
	}
	if statusCount != 1 {
		t.Fatalf("expected exactly 1 StreamStatus event, got %d (%+v)", statusCount, events)
	}
}

// TestContentToolCallTokenGate_Prose_NeverEmitsToolStatus verifies plain
// prose (the common case) never pays for the first-signal status — it
// streams immediately with no extra event.
func TestContentToolCallTokenGate_Prose_NeverEmitsToolStatus(t *testing.T) {
	var events []StreamEvent
	gate := NewContentToolCallTokenGate(nil, func(ev StreamEvent) { events = append(events, ev) })
	gate.Push("hello")
	gate.Push(" world")
	gate.Finish("hello world")

	for _, ev := range events {
		if ev.Type == StreamStatus {
			t.Fatalf("plain prose must never emit a tool-status event, got %+v", ev)
		}
	}
	if len(events) == 0 {
		t.Fatal("expected at least one StreamText event for prose")
	}
}

// TestParseSSE_NativeToolCalls_EmitsUsingToolsStatusBeforeDone verifies a
// straight-to-tool-call response (native delta.tool_calls, no prose) still
// gives the user a signal before RunLoop's tool_call event fires at the
// very end of the streamed response.
func TestParseSSE_NativeToolCalls_EmitsUsingToolsStatusBeforeDone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"1","function":{"name":"bash","arguments":""}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"command\":\"ls\"}"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	var events []StreamEvent
	resp, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "list files"}},
		OnEvent:  func(ev StreamEvent) { events = append(events, ev) },
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}

	var statusIdx, doneIdx = -1, -1
	var statusCount int
	for i, ev := range events {
		if ev.Type == StreamStatus && ev.Content == "using tools" {
			statusCount++
			if statusIdx == -1 {
				statusIdx = i
			}
		}
		if ev.Type == StreamDone {
			doneIdx = i
		}
	}
	if statusCount != 1 {
		t.Fatalf("expected exactly 1 'using tools' status event, got %d (%+v)", statusCount, events)
	}
	if statusIdx == -1 || doneIdx == -1 || statusIdx >= doneIdx {
		t.Fatalf("expected 'using tools' status before StreamDone, got events: %+v", events)
	}
}

// TestParseSSE_FirstProseDelta_FlushesImmediately verifies a streamed prose
// response reaches OnToken for its very first delta without waiting to
// accumulate later deltas — no batching window before first emit.
func TestParseSSE_FirstProseDelta_FlushesImmediately(t *testing.T) {
	firstTokenCh := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"He\"}}]}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		// Block here until the test confirms the first delta was already
		// delivered to OnToken — proves the backend didn't wait to batch
		// this delta with what comes next.
		select {
		case <-firstTokenCh:
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"llo\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	firstSeen := make(chan struct{}, 1)
	var mu sync.Mutex
	var got string
	go func() {
		_, _ = b.ChatCompletion(context.Background(), ChatRequest{
			Model:    "test-model",
			Messages: []Message{{Role: "user", Content: "hi"}},
			OnToken: func(tok string) {
				mu.Lock()
				got += tok
				mu.Unlock()
				select {
				case firstSeen <- struct{}{}:
				default:
				}
			},
		})
	}()

	<-firstSeen // first delta reached OnToken
	mu.Lock()
	if got != "He" {
		mu.Unlock()
		t.Fatalf("expected first delta %q to flush alone, got %q", "He", got)
	}
	mu.Unlock()
	firstTokenCh <- struct{}{} // let the server send the rest
}
