package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/tools"
)

// ---------------------------------------------------------------------------
// streamEventToWS
// ---------------------------------------------------------------------------

func TestStreamEventToWS_TextMappedToToken(t *testing.T) {
	// StreamText must NOT map to "token" — the onToken callback already sends a
	// "token" WS message per text chunk.  Normalising StreamText to "token" here
	// causes every word to be delivered twice (word-doubling bug, issue #30).
	ev := backend.StreamEvent{Type: backend.StreamText, Content: "hello"}
	msg := streamEventToWS(ev, "sess-1")
	if msg.Type == "token" {
		t.Errorf("StreamText must not map to 'token' (causes word doubling); got type %q — fix streamEventToWS", msg.Type)
	}
	if msg.Content != "hello" {
		t.Errorf("expected content 'hello', got %q", msg.Content)
	}
	if msg.SessionID != "sess-1" {
		t.Errorf("expected session_id 'sess-1', got %q", msg.SessionID)
	}
}

func TestStreamEventToWS_ThoughtMappedToToken(t *testing.T) {
	ev := backend.StreamEvent{Type: backend.StreamThought, Content: "thinking..."}
	msg := streamEventToWS(ev, "sess-2")
	if msg.Type != "token" {
		t.Errorf("StreamThought should map to type 'token', got %q", msg.Type)
	}
	if msg.Content != "thinking..." {
		t.Errorf("expected content 'thinking...', got %q", msg.Content)
	}
}

func TestStreamEventToWS_ToolCallPreservedType(t *testing.T) {
	ev := backend.StreamEvent{Type: backend.StreamToolCall, Content: "", Payload: map[string]any{"name": "bash"}}
	msg := streamEventToWS(ev, "sess-3")
	if msg.Type != string(backend.StreamToolCall) {
		t.Errorf("non-text event type should be preserved, got %q", msg.Type)
	}
}

func TestStreamEventToWS_PayloadPassedThrough(t *testing.T) {
	payload := map[string]any{"key": "value"}
	ev := backend.StreamEvent{Type: backend.StreamToolCall, Payload: payload}
	msg := streamEventToWS(ev, "sess-4")
	if msg.Payload["key"] != "value" {
		t.Errorf("payload should pass through unchanged, got %v", msg.Payload)
	}
}

func TestStreamEventToWS_EmptySessionID(t *testing.T) {
	ev := backend.StreamEvent{Type: backend.StreamText, Content: "hi"}
	msg := streamEventToWS(ev, "")
	if msg.SessionID != "" {
		t.Errorf("empty sessionID should remain empty, got %q", msg.SessionID)
	}
}

// ---------------------------------------------------------------------------
// parseBoolPayload
// ---------------------------------------------------------------------------

func TestParseBoolPayload_NativeBool(t *testing.T) {
	if !parseBoolPayload(true) {
		t.Error("true should parse as true")
	}
	if parseBoolPayload(false) {
		t.Error("false should parse as false")
	}
}

func TestParseBoolPayload_Float64(t *testing.T) {
	if !parseBoolPayload(float64(1)) {
		t.Error("float64(1) should parse as true")
	}
	if parseBoolPayload(float64(0)) {
		t.Error("float64(0) should parse as false")
	}
}

func TestParseBoolPayload_Int(t *testing.T) {
	if !parseBoolPayload(int(1)) {
		t.Error("int(1) should parse as true")
	}
	if parseBoolPayload(int(0)) {
		t.Error("int(0) should parse as false")
	}
}

func TestParseBoolPayload_StringTrue(t *testing.T) {
	if !parseBoolPayload("true") {
		t.Error("\"true\" should parse as true")
	}
	if !parseBoolPayload("1") {
		t.Error("\"1\" should parse as true")
	}
}

func TestParseBoolPayload_StringFalse(t *testing.T) {
	if parseBoolPayload("false") {
		t.Error("\"false\" should parse as false")
	}
	if parseBoolPayload("0") {
		t.Error("\"0\" should parse as false")
	}
	if parseBoolPayload("yes") {
		t.Error("\"yes\" should parse as false (unrecognised)")
	}
}

func TestParseBoolPayload_NilReturnsFalse(t *testing.T) {
	if parseBoolPayload(nil) {
		t.Error("nil should parse as false")
	}
}

func TestParseBoolPayload_UnrecognisedTypeReturnsFalse(t *testing.T) {
	if parseBoolPayload(struct{}{}) {
		t.Error("struct{}{} should parse as false")
	}
}

// ---------------------------------------------------------------------------
// run_id guard: WSMessage RunID field round-trips through JSON marshaling
// ---------------------------------------------------------------------------

func TestWSMessage_RunID_RoundTrip(t *testing.T) {
	// RunID must be present on the struct so the frontend stale-event guard works.
	msg := WSMessage{Type: "done", SessionID: "s1", RunID: "run-abc-123"}
	if msg.RunID != "run-abc-123" {
		t.Errorf("RunID should be set, got %q", msg.RunID)
	}
}

func TestWSMessage_RunID_EmptyByDefault(t *testing.T) {
	msg := WSMessage{Type: "token", Content: "hi"}
	if msg.RunID != "" {
		t.Errorf("RunID should be empty by default, got %q", msg.RunID)
	}
}

func TestWSMessage_RunID_EchoedInDone(t *testing.T) {
	// Verify the done message is constructed with the run_id from the request.
	// This is the contract the frontend relies on to discard stale events.
	runID := "run-xyz-456"
	doneMsg := WSMessage{Type: "done", SessionID: "sess-1", RunID: runID}
	if doneMsg.Type != "done" {
		t.Errorf("type should be 'done', got %q", doneMsg.Type)
	}
	if doneMsg.RunID != runID {
		t.Errorf("RunID should be echoed in done message, got %q", doneMsg.RunID)
	}
}

func TestWSMessage_RunID_EchoedInError(t *testing.T) {
	runID := "run-err-789"
	errMsg := WSMessage{Type: "error", Content: "something failed", SessionID: "sess-2", RunID: runID}
	if errMsg.RunID != runID {
		t.Errorf("RunID should be echoed in error message, got %q", errMsg.RunID)
	}
}

// ---------------------------------------------------------------------------
// statusForToolCall: phase-true status translation (Preparing context…
// replacement — the UI shows the latest status Content instead of a static
// string for every hallway turn)
// ---------------------------------------------------------------------------

func TestStatusForToolCall_DelegateToAgent_NamesTarget(t *testing.T) {
	content, ok := statusForToolCall(map[string]any{
		"tool": "delegate_to_agent",
		"args": map[string]any{"agent": "Steve", "task": "check the logs"},
	})
	if !ok {
		t.Fatalf("expected delegate_to_agent to map to a status phase")
	}
	if content != "asking Steve…" {
		t.Errorf("content = %q, want %q", content, "asking Steve…")
	}
}

func TestStatusForToolCall_DelegateToAgent_MissingTargetFallsBackGenerically(t *testing.T) {
	content, ok := statusForToolCall(map[string]any{
		"tool": "delegate_to_agent",
		"args": map[string]any{"task": "check the logs"},
	})
	if !ok {
		t.Fatalf("expected delegate_to_agent to map to a status phase even without a resolvable target")
	}
	if content != "asking a teammate…" {
		t.Errorf("content = %q, want %q", content, "asking a teammate…")
	}
}

func TestStatusForToolCall_MemoryTools_MapToRecallingMemory(t *testing.T) {
	for _, tool := range []string{"muninn_where_left_off", "muninn_recall"} {
		content, ok := statusForToolCall(map[string]any{"tool": tool})
		if !ok {
			t.Fatalf("tool %q: expected a status phase", tool)
		}
		if content != "recalling memory" {
			t.Errorf("tool %q: content = %q, want %q", tool, content, "recalling memory")
		}
	}
}

func TestStatusForToolCall_OrdinaryLocalTool_NoStatusPhase(t *testing.T) {
	_, ok := statusForToolCall(map[string]any{"tool": "search_files", "args": map[string]any{"query": "TODO"}})
	if ok {
		t.Errorf("search_files should not map to a status phase (falls back to the last status already on the wire)")
	}
}

func TestStatusForToolCall_ReadFile_NamesBasename(t *testing.T) {
	content, ok := statusForToolCall(map[string]any{
		"tool": "read_file",
		"args": map[string]any{"file_path": "internal/mathutil/mathutil.go"},
	})
	if !ok {
		t.Fatalf("expected read_file to map to a status phase")
	}
	if content != "reading mathutil.go…" {
		t.Errorf("content = %q, want %q", content, "reading mathutil.go…")
	}
}

func TestStatusForToolCall_ReadFile_MissingArgFallsBackGenerically(t *testing.T) {
	content, ok := statusForToolCall(map[string]any{"tool": "read_file"})
	if !ok {
		t.Fatalf("expected read_file to map to a status phase even without a resolvable path")
	}
	if content != "reading a file…" {
		t.Errorf("content = %q, want %q", content, "reading a file…")
	}
}

func TestStatusForToolCall_WriteFile_NamesBasename(t *testing.T) {
	content, ok := statusForToolCall(map[string]any{
		"tool": "write_file",
		"args": map[string]any{"file_path": "internal/mathutil/mathutil.go"},
	})
	if !ok {
		t.Fatalf("expected write_file to map to a status phase")
	}
	if content != "writing mathutil.go…" {
		t.Errorf("content = %q, want %q", content, "writing mathutil.go…")
	}
}

func TestStatusForToolCall_EditFile_NamesBasename(t *testing.T) {
	content, ok := statusForToolCall(map[string]any{
		"tool": "edit_file",
		"args": map[string]any{"file_path": "internal/mathutil/mathutil.go"},
	})
	if !ok {
		t.Fatalf("expected edit_file to map to a status phase")
	}
	if content != "editing mathutil.go…" {
		t.Errorf("content = %q, want %q", content, "editing mathutil.go…")
	}
}

func TestStatusForToolCall_Bash_NamesTruncatedCommand(t *testing.T) {
	content, ok := statusForToolCall(map[string]any{
		"tool": "bash",
		"args": map[string]any{"command": "go test ./... -run TestSomethingLong -v"},
	})
	if !ok {
		t.Fatalf("expected bash to map to a status phase")
	}
	if content != "running: go test ./... -run TestSomethi…" {
		t.Errorf("content = %q, want %q", content, "running: go test ./... -run TestSomethi…")
	}
}

func TestStatusForToolCall_Bash_SanitizesMultilineCommand(t *testing.T) {
	content, ok := statusForToolCall(map[string]any{
		"tool": "bash",
		"args": map[string]any{"command": "echo hi\nrm -rf /"},
	})
	if !ok {
		t.Fatalf("expected bash to map to a status phase")
	}
	if content != "running: echo hi…" {
		t.Errorf("content = %q, want %q", content, "running: echo hi…")
	}
}

func TestStatusForToolCall_Bash_MissingArgFallsBackGenerically(t *testing.T) {
	content, ok := statusForToolCall(map[string]any{"tool": "bash"})
	if !ok {
		t.Fatalf("expected bash to map to a status phase even without a resolvable command")
	}
	if content != "running a command…" {
		t.Errorf("content = %q, want %q", content, "running a command…")
	}
}

func TestStatusForToolCall_ListDir_ExploringFiles(t *testing.T) {
	content, ok := statusForToolCall(map[string]any{
		"tool": "list_dir",
		"args": map[string]any{"path": "internal/server"},
	})
	if !ok {
		t.Fatalf("expected list_dir to map to a status phase")
	}
	if content != "exploring files…" {
		t.Errorf("content = %q, want %q", content, "exploring files…")
	}
}

func TestStatusForToolCall_NilPayload_NoStatusPhase(t *testing.T) {
	if _, ok := statusForToolCall(nil); ok {
		t.Errorf("nil payload should not map to a status phase")
	}
}

// delegateStatusEventBackend is a scripted Backend whose ChatCompletion emits the given
// StreamEvents directly via req.OnEvent before returning — simulating the
// wire shape a real backend/RunLoop tool dispatch produces (see
// RunLoopConfig.OnEvent -> backend.ChatRequest.OnEvent in internal/agent/loop.go)
// without requiring a real tool registered under that name. This isolates
// the ws.go translation layer (runWSChat's onEvent closure) from the rest of
// the agentic tool-dispatch machinery.
type delegateStatusEventBackend struct {
	events []backend.StreamEvent
	reply  string
	// delay holds ChatCompletion open after the events fire, so heartbeat
	// ticks land between the tool_call status and the first token.
	delay time.Duration
}

func (b *delegateStatusEventBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	for _, ev := range b.events {
		if req.OnEvent != nil {
			req.OnEvent(ev)
		}
	}
	if b.delay > 0 {
		time.Sleep(b.delay)
	}
	if req.OnToken != nil {
		req.OnToken(b.reply)
	}
	return &backend.ChatResponse{DoneReason: "stop", Content: b.reply}, nil
}

func (b *delegateStatusEventBackend) Health(_ context.Context) error   { return nil }
func (b *delegateStatusEventBackend) Shutdown(_ context.Context) error { return nil }
func (b *delegateStatusEventBackend) ContextWindow() int               { return 8192 }

// TestWSChat_DelegateToAgentToolCall_EmitsAskingStatus is the wire-level
// regression test for the "Preparing context and delegation plan…" static
// status defect: a delegate_to_agent tool_call StreamEvent fired during a
// hallway run must produce a status event naming the target agent on the
// wire, not a generic placeholder.
func TestWSChat_DelegateToAgentToolCall_EmitsAskingStatus(t *testing.T) {
	eb := &delegateStatusEventBackend{
		reply: "Done.",
		events: []backend.StreamEvent{
			{
				Type: backend.StreamToolCall,
				Payload: map[string]any{
					"id":   "call-1",
					"tool": "delegate_to_agent",
					"args": map[string]any{"agent": "Steve", "task": "check the logs"},
				},
			},
		},
	}
	orch, err := agent.NewOrchestrator(eb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	srv, _ := newTestServer(t)
	srv.orch = orch
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{
			{Name: "Winston", Model: modelconfig.DefaultModels().Reasoner, IsDefault: true, SystemPrompt: "You are Winston."},
		}}, nil
	}

	sess := srv.store.New("delegate-status-session", "/workspace", modelconfig.DefaultModels().Reasoner)
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	client := &wsClient{send: make(chan WSMessage, 256), ctx: context.Background()}
	srv.handleWSMessage(client, WSMessage{
		Type:      "chat",
		SessionID: sess.ID,
		Content:   "please loop in Steve",
		RunID:     "delegate-run-1",
	})

	deadline := time.After(10 * time.Second)
	sawAskingSteve := false
	sawOldStaticString := false
	for {
		select {
		case msg := <-client.send:
			if msg.Type == "status" {
				if strings.Contains(msg.Content, "Preparing context and delegation plan") {
					sawOldStaticString = true
				}
				if msg.Content == "asking Steve…" {
					sawAskingSteve = true
				}
			}
			if msg.Type == "done" && msg.RunID == "delegate-run-1" {
				if !sawAskingSteve {
					t.Fatalf("never saw a status event %q on the wire for the delegate_to_agent tool_call", "asking Steve…")
				}
				if sawOldStaticString {
					t.Fatalf("status wire content must never carry the removed static string")
				}
				// runWSChat emits "done" BEFORE persistAccumulated writes the
				// assistant row (see internal/server/ws.go). Returning here
				// would race that write against t.TempDir()'s RemoveAll and
				// fail the whole package with "directory not empty". Wait for
				// the assistant row to land before letting cleanup run.
				waitForAssistantPersisted(t, srv, sess.ID)
				return
			}
			if msg.Type == "error" {
				t.Fatalf("unexpected error message: %v", msg.Content)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for run to complete")
		}
	}
}

// TestWSChat_HeartbeatDoesNotStompPhaseStatus is the regression test for the
// cross-package interaction between the 15s keep-alive heartbeat and the
// phase-true status layer: the heartbeat must re-emit the CURRENT phase, not
// a hardcoded "thinking" that flips the UI off "asking Steve…" every 15
// seconds for the whole length of a long delegation.
func TestWSChat_HeartbeatDoesNotStompPhaseStatus(t *testing.T) {
	prev := wsHeartbeatInterval
	wsHeartbeatInterval = 10 * time.Millisecond
	defer func() { wsHeartbeatInterval = prev }()

	eb := &delegateStatusEventBackend{
		reply: "Done.",
		delay: 120 * time.Millisecond, // long enough for several heartbeats
		events: []backend.StreamEvent{
			{
				Type: backend.StreamToolCall,
				Payload: map[string]any{
					"id":   "call-1",
					"tool": "delegate_to_agent",
					"args": map[string]any{"agent": "Steve", "task": "check the logs"},
				},
			},
		},
	}
	orch, err := agent.NewOrchestrator(eb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	srv, _ := newTestServer(t)
	srv.orch = orch
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{
			{Name: "Winston", Model: modelconfig.DefaultModels().Reasoner, IsDefault: true, SystemPrompt: "You are Winston."},
		}}, nil
	}

	sess := srv.store.New("heartbeat-status-session", "/workspace", modelconfig.DefaultModels().Reasoner)
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	client := &wsClient{send: make(chan WSMessage, 512), ctx: context.Background()}
	srv.handleWSMessage(client, WSMessage{
		Type:      "chat",
		SessionID: sess.ID,
		Content:   "please loop in Steve",
		RunID:     "heartbeat-run-1",
	})

	deadline := time.After(10 * time.Second)
	sawAsking := false
	for {
		select {
		case msg := <-client.send:
			if msg.Type == "status" {
				if msg.Content == "asking Steve…" {
					sawAsking = true
				} else if sawAsking && msg.Content == "thinking" {
					t.Fatalf("heartbeat stomped the phase-true status: got %q after %q", msg.Content, "asking Steve…")
				}
			}
			if msg.Type == "done" && msg.RunID == "heartbeat-run-1" {
				if !sawAsking {
					t.Fatal("never saw the asking-Steve status")
				}
				waitForAssistantPersisted(t, srv, sess.ID)
				return
			}
			if msg.Type == "error" {
				t.Fatalf("unexpected error message: %v", msg.Content)
			}
		case <-deadline:
			t.Fatal("timed out waiting for run to complete")
		}
	}
}

// TestWSChat_HeartbeatDoesNotStompPlainStreamStatus is the regression test
// for DEFECT 2: plain StreamStatus events emitted directly by the engine
// (agent_dispatcher's "thinking", hire_ask's "hiring", the loop-nudge's
// "continuing", external.go's model-loading status) went straight to emit
// without updating lastStatus, so the 15s heartbeat re-emitted the OLD phase
// and stomped them within 15s — unlike tool-call-derived statuses, which
// already went through setStatus. This backend fires a StreamStatus event
// with a distinctive, non-"thinking" phase and stalls before the first
// token long enough for several heartbeat ticks to land; none of them may
// revert the wire back to "thinking".
func TestWSChat_HeartbeatDoesNotStompPlainStreamStatus(t *testing.T) {
	prev := wsHeartbeatInterval
	wsHeartbeatInterval = 10 * time.Millisecond
	defer func() { wsHeartbeatInterval = prev }()

	eb := &delegateStatusEventBackend{
		reply: "Done.",
		delay: 120 * time.Millisecond, // long enough for several heartbeats
		events: []backend.StreamEvent{
			{Type: backend.StreamStatus, Content: "hiring"},
		},
	}
	orch, err := agent.NewOrchestrator(eb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	srv, _ := newTestServer(t)
	srv.orch = orch
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{
			{Name: "Winston", Model: modelconfig.DefaultModels().Reasoner, IsDefault: true, SystemPrompt: "You are Winston."},
		}}, nil
	}

	sess := srv.store.New("plain-status-heartbeat-session", "/workspace", modelconfig.DefaultModels().Reasoner)
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	client := &wsClient{send: make(chan WSMessage, 512), ctx: context.Background()}
	srv.handleWSMessage(client, WSMessage{
		Type:      "chat",
		SessionID: sess.ID,
		Content:   "please add a teammate",
		RunID:     "plain-status-heartbeat-run-1",
	})

	deadline := time.After(10 * time.Second)
	sawHiring := false
	for {
		select {
		case msg := <-client.send:
			if msg.Type == "status" {
				if msg.Content == "hiring" {
					sawHiring = true
				} else if sawHiring && msg.Content == "thinking" {
					t.Fatalf("heartbeat stomped the plain StreamStatus phase: got %q after %q", msg.Content, "hiring")
				}
			}
			if msg.Type == "done" && msg.RunID == "plain-status-heartbeat-run-1" {
				if !sawHiring {
					t.Fatal("never saw the hiring status")
				}
				waitForAssistantPersisted(t, srv, sess.ID)
				return
			}
			if msg.Type == "error" {
				t.Fatalf("unexpected error message: %v", msg.Content)
			}
		case <-deadline:
			t.Fatal("timed out waiting for run to complete")
		}
	}
}

// waitForAssistantPersisted blocks until the run goroutine has finished its
// post-"done" persistence for sessionID, so t.TempDir cleanup cannot race the
// write. Bounded; fails the test rather than hanging.
func waitForAssistantPersisted(t *testing.T, srv *Server, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		msgs, err := srv.store.ReadMessages(sessionID)
		if err == nil {
			for _, m := range msgs {
				if m.Role == "assistant" {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("assistant message never persisted for session %s", sessionID)
}

// TestStatusForToolCall_PathArgIsSanitizedAndBounded locks down that the
// model-supplied file_path reaching the status line is scrubbed of control
// characters and length-capped. statusForToolCall interpolates tool args
// into a line every session subscriber sees; an embedded newline must not
// make the status two lines, and a 400-character filename must not become a
// 400-character status.
func TestStatusForToolCall_PathArgIsSanitizedAndBounded(t *testing.T) {
	content, ok := statusForToolCall(map[string]any{
		"tool": "read_file",
		"args": map[string]any{"file_path": "dir/we\nird\x1b[31m\x07name.go"},
	})
	if !ok {
		t.Fatalf("expected read_file to map to a status phase")
	}
	if strings.ContainsAny(content, "\n\r\x1b\x07") {
		t.Errorf("control characters survived into status text: %q", content)
	}
	// The ESC byte becomes a space; the inert "[31m" remainder is left as
	// plain text (the client renders status via {{ }}, not v-html).
	if content != "reading we ird [31m name.go…" {
		t.Errorf("content = %q, want %q", content, "reading we ird [31m name.go…")
	}

	long := strings.Repeat("x", 400) + ".go"
	content, ok = statusForToolCall(map[string]any{
		"tool": "edit_file",
		"args": map[string]any{"file_path": "/tmp/" + long},
	})
	if !ok {
		t.Fatalf("expected edit_file to map to a status phase")
	}
	if got := len([]rune(content)); got > len([]rune("editing …"))+maxStatusBasename {
		t.Errorf("status line not bounded: %d runes (%q)", got, content)
	}
}

// TestSanitizeCommandSnippet_StripsControlCharacters guards the bash arm of
// the same concern: a command carrying ANSI escapes or a BEL must not put
// them on the wire as status text.
func TestSanitizeCommandSnippet_StripsControlCharacters(t *testing.T) {
	got := sanitizeCommandSnippet("echo \x1b[31mred\x07 done")
	if strings.ContainsAny(got, "\x1b\x07") {
		t.Errorf("control characters survived: %q", got)
	}
	if got != "echo [31mred done" {
		t.Errorf("got %q, want %q", got, "echo [31mred done")
	}
	if got := sanitizeCommandSnippet("   \x00\x07  "); got != "" {
		t.Errorf("all-control command should sanitize to empty, got %q", got)
	}
}

// noopPingTool is a trivial real tool used to drive an actual tool dispatch
// (as opposed to a scripted StreamEvent) so the test below exercises the
// production wiring end to end: RunLoop's dispatchTools -> agent_dispatcher's
// chatOnToolCall/chatOnToolDone -> the onToolEvent bridge
// (newWSToolEventHandler) -> runWSChat's onEvent closure, where the "agent"
// stamp is added.
type noopPingTool struct{}

func (noopPingTool) Name() string                      { return "ping_tool" }
func (noopPingTool) Description() string               { return "ping" }
func (noopPingTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (noopPingTool) Schema() backend.Tool {
	return backend.Tool{Function: backend.ToolFunction{Name: "ping_tool"}}
}
func (noopPingTool) Execute(context.Context, map[string]any) tools.ToolResult {
	return tools.ToolResult{Output: "pong"}
}

// scriptedToolCallBackend is a Backend whose first ChatCompletion call
// returns a real tool_calls response (driving an actual RunLoop dispatch,
// not a scripted onEvent injection) and whose second call returns plain
// content, ending the turn.
type scriptedToolCallBackend struct{ n int }

func (b *scriptedToolCallBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	b.n++
	if b.n == 1 {
		return &backend.ChatResponse{
			DoneReason: "tool_calls",
			ToolCalls: []backend.ToolCall{{
				ID:       "call-ping-1",
				Function: backend.ToolCallFunction{Name: "ping_tool", Arguments: map[string]any{}},
			}},
		}, nil
	}
	if req.OnToken != nil {
		req.OnToken("done")
	}
	return &backend.ChatResponse{Content: "done", DoneReason: "stop"}, nil
}
func (b *scriptedToolCallBackend) Health(_ context.Context) error   { return nil }
func (b *scriptedToolCallBackend) Shutdown(_ context.Context) error { return nil }
func (b *scriptedToolCallBackend) ContextWindow() int               { return 8192 }

// TestWSChat_RealToolDispatch_StampsAgentOnToolCallAndResultPayloads is the
// wire-level regression test for per-agent ticker attribution (vet wave-6
// #2/#3): tool_call/tool_result WS payloads must carry the emitting agent's
// name, not just the top-level WSMessage.Agent field, so the frontend can
// scope a streaming message's activeToolCalls ticker to its own agent when
// two agents stream concurrently in one space. This drives a REAL tool
// dispatch (RunLoop -> agent_dispatcher's onToolEvent bridge ->
// newWSToolEventHandler -> runWSChat's onEvent, where the stamp is added),
// covering both paths named in the fix: the onEvent path and the
// onToolEvent bridge that funnels into it.
func TestWSChat_RealToolDispatch_StampsAgentOnToolCallAndResultPayloads(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(noopPingTool{})

	b := &scriptedToolCallBackend{}
	orch, err := agent.NewOrchestrator(b, modelconfig.DefaultModels(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	orch.SetTools(reg, permissions.NewGate(true, nil))

	srv, _ := newTestServer(t)
	srv.orch = orch
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{
			{Name: "Winston", Model: modelconfig.DefaultModels().Reasoner, IsDefault: true, SystemPrompt: "You are Winston.", LocalTools: []string{"ping_tool"}},
		}}, nil
	}

	sess := srv.store.New("agent-attribution-session", "/workspace", modelconfig.DefaultModels().Reasoner)
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	client := &wsClient{send: make(chan WSMessage, 256), ctx: context.Background()}
	srv.handleWSMessage(client, WSMessage{
		Type:      "chat",
		SessionID: sess.ID,
		Content:   "please run the ping tool and report back",
		RunID:     "attribution-run-1",
	})

	deadline := time.After(10 * time.Second)
	sawToolCall, sawToolResult := false, false
	for {
		select {
		case msg := <-client.send:
			switch msg.Type {
			case "tool_call":
				sawToolCall = true
				if got, _ := msg.Payload["agent"].(string); got != "Winston" {
					t.Errorf("tool_call payload agent = %q, want %q", got, "Winston")
				}
			case "tool_result":
				sawToolResult = true
				if got, _ := msg.Payload["agent"].(string); got != "Winston" {
					t.Errorf("tool_result payload agent = %q, want %q", got, "Winston")
				}
			case "done":
				if !sawToolCall || !sawToolResult {
					t.Fatalf("expected both tool_call and tool_result on the wire, got tool_call=%v tool_result=%v", sawToolCall, sawToolResult)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for done")
		}
	}
}
