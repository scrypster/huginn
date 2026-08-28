package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/scrypster/huginn/internal/permissions"
	"gopkg.in/yaml.v3"
)

func writeAgentYAML(t *testing.T, dir, name, model string) {
	t.Helper()
	path := filepath.Join(dir, name+".yaml")
	def := map[string]any{"name": name, "model": model}
	b, err := yaml.Marshal(def)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// subscribePermissionPrompt registers a WS client for sessionID so
// PermissionPromptFunc has somewhere to send the banner. Without a
// subscriber the bridge fails closed immediately (there is no human to ask),
// so every test that expects the prompt to BLOCK must register one first.
// Returns the client's send channel so a test can assert on what was emitted.
func subscribePermissionPrompt(t *testing.T, srv *Server, sessionID string) chan WSMessage {
	t.Helper()
	ch := make(chan WSMessage, 8)
	srv.wsHub.registerWithSession(&wsClient{send: ch, sessionID: sessionID}, sessionID)
	return ch
}

// TestPermissionPromptFunc_NoSessionIDDeniesImmediately verifies the fail-
// closed behavior when a permission request has no SessionID to route to —
// there's no way to reach the user, so it must deny rather than block.
func TestPermissionPromptFunc_NoSessionIDDeniesImmediately(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	promptFunc := srv.PermissionPromptFunc()
	decision := promptFunc(permissions.PermissionRequest{ToolName: "bash", AgentName: "codey"})
	if decision != permissions.Deny {
		t.Errorf("expected Deny with empty SessionID, got %v", decision)
	}
}

// TestPermissionPromptFunc_ResolvedOnce verifies scope "once": the promptFunc
// blocks until handlePermissionResponse resolves it, and it returns Allow
// without persisting anything.
func TestPermissionPromptFunc_ResolvedOnce(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HUGINN_HOME", fakeHome)
	t.Setenv("HOME", fakeHome)

	srv, ts := newTestServer(t)
	defer ts.Close()

	subscribePermissionPrompt(t, srv, "sess-1")

	resultCh := make(chan permissions.Decision, 1)
	go func() {
		resultCh <- srv.PermissionPromptFunc()(permissions.PermissionRequest{
			ToolName:  "bash",
			AgentName: "codey",
			SessionID: "sess-1",
			Args:      map[string]any{"command": "go test ./..."},
		})
	}()

	// Wait for the registration to land, then find its id and resolve it.
	var id string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		srv.permPrompts.mu.Lock()
		for k := range srv.permPrompts.pending {
			id = k
		}
		srv.permPrompts.mu.Unlock()
		if id != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("expected a pending permission prompt to be registered")
	}

	srv.handlePermissionResponse(WSMessage{Payload: map[string]any{"id": id, "scope": "once"}})

	select {
	case d := <-resultCh:
		if d != permissions.Allow {
			t.Errorf("expected Allow, got %v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for promptFunc to return")
	}
}

// TestPermissionPromptFunc_AlwaysAgentPersistsGrant verifies scope
// "always_agent": the promptFunc returns AllowAll AND the agent's
// ApprovedTools is persisted with the tool name.
func TestPermissionPromptFunc_AlwaysAgentPersistsGrant(t *testing.T) {
	fakeHome := t.TempDir()
	agentsDir := filepath.Join(fakeHome, ".huginn", "agents")
	if err := os.MkdirAll(agentsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeAgentYAML(t, agentsDir, "codey", "claude-sonnet-4-5")
	t.Setenv("HUGINN_HOME", fakeHome)
	t.Setenv("HOME", fakeHome)

	srv, ts := newTestServer(t)
	defer ts.Close()

	subscribePermissionPrompt(t, srv, "sess-1")

	resultCh := make(chan permissions.Decision, 1)
	go func() {
		resultCh <- srv.PermissionPromptFunc()(permissions.PermissionRequest{
			ToolName:  "bash",
			AgentName: "codey",
			SessionID: "sess-1",
		})
	}()

	var id string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		srv.permPrompts.mu.Lock()
		for k := range srv.permPrompts.pending {
			id = k
		}
		srv.permPrompts.mu.Unlock()
		if id != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("expected a pending permission prompt to be registered")
	}

	srv.handlePermissionResponse(WSMessage{Payload: map[string]any{"id": id, "scope": "always_agent"}})

	select {
	case d := <-resultCh:
		if d != permissions.AllowAll {
			t.Errorf("expected AllowAll, got %v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for promptFunc to return")
	}

	b, err := os.ReadFile(filepath.Join(agentsDir, "codey.yaml"))
	if err != nil {
		t.Fatalf("read persisted agent: %v", err)
	}
	var saved map[string]any
	if err := yaml.Unmarshal(b, &saved); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	approved, _ := saved["approved_tools"].([]any)
	if len(approved) != 1 || approved[0] != "bash" {
		t.Errorf("expected approved_tools=[bash], got %v", saved["approved_tools"])
	}
}

// TestPermissionPromptFunc_Deny verifies scope "deny" resolves to Deny.
func TestPermissionPromptFunc_Deny(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HUGINN_HOME", fakeHome)
	t.Setenv("HOME", fakeHome)

	srv, ts := newTestServer(t)
	defer ts.Close()

	subscribePermissionPrompt(t, srv, "sess-1")

	resultCh := make(chan permissions.Decision, 1)
	go func() {
		resultCh <- srv.PermissionPromptFunc()(permissions.PermissionRequest{
			ToolName: "bash", AgentName: "codey", SessionID: "sess-1",
		})
	}()

	var id string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		srv.permPrompts.mu.Lock()
		for k := range srv.permPrompts.pending {
			id = k
		}
		srv.permPrompts.mu.Unlock()
		if id != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("expected a pending permission prompt to be registered")
	}

	srv.handlePermissionResponse(WSMessage{Payload: map[string]any{"id": id, "scope": "deny"}})

	select {
	case d := <-resultCh:
		if d != permissions.Deny {
			t.Errorf("expected Deny, got %v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for promptFunc to return")
	}
}

// TestPermissionPromptFunc_Timeout verifies that an unanswered prompt is
// eventually denied and cleaned up (second-line defense; the gate's own
// promptFuncTimeout will normally win the race, but this test exercises the
// permissionPrompts registry's own cleanup path directly).
func TestPermissionPrompts_TimeoutDeniesAndCleansUp(t *testing.T) {
	orig := permissionPromptTimeout
	permissionPromptTimeout = 20 * time.Millisecond
	defer func() { permissionPromptTimeout = orig }()

	p := newPermissionPrompts()
	ch := p.register("req-1", permissions.PermissionRequest{ToolName: "bash"}, nil)

	select {
	case d := <-ch:
		if d != permissions.Deny {
			t.Errorf("expected Deny on timeout, got %v", d)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for cleanup to deny")
	}

	p.mu.Lock()
	_, stillPending := p.pending["req-1"]
	p.mu.Unlock()
	if stillPending {
		t.Error("expected pending entry to be removed after timeout")
	}
}

// TestPermissionPromptFuncCtx_CancelledDeniesImmediatelyAndClearsBanner
// verifies that cancelling the caller's context while a WS permission prompt
// is pending (e.g. chat_cancel arriving mid-prompt) unblocks the bridge right
// away with Deny — instead of waiting out permissionPromptTimeout — and
// broadcasts permission_cancelled with reason "cancelled" so the banner
// clears on the client.
func TestPermissionPromptFuncCtx_CancelledDeniesImmediatelyAndClearsBanner(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HUGINN_HOME", fakeHome)
	t.Setenv("HOME", fakeHome)

	srv, ts := newTestServer(t)
	defer ts.Close()

	out := subscribePermissionPrompt(t, srv, "sess-1")

	ctx, cancel := context.WithCancel(context.Background())

	resultCh := make(chan permissions.Decision, 1)
	go func() {
		resultCh <- srv.PermissionPromptFuncCtx()(ctx, permissions.PermissionRequest{
			ToolName: "bash", AgentName: "codey", SessionID: "sess-1",
		})
	}()

	// Wait for the permission_request banner, then cancel mid-prompt.
	var reqID string
	select {
	case msg := <-out:
		if msg.Type != "permission_request" {
			t.Fatalf("expected permission_request, got %q", msg.Type)
		}
		reqID, _ = msg.Payload["id"].(string)
	case <-time.After(2 * time.Second):
		t.Fatal("no permission_request broadcast")
	}
	if reqID == "" {
		t.Fatal("expected a request id in the permission_request payload")
	}

	cancel()

	select {
	case d := <-resultCh:
		if d != permissions.Deny {
			t.Errorf("expected Deny on cancellation, got %v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("promptFunc did not unblock promptly after ctx cancellation")
	}

	select {
	case msg := <-out:
		if msg.Type != "permission_cancelled" {
			t.Fatalf("expected permission_cancelled, got %q", msg.Type)
		}
		if got, _ := msg.Payload["id"].(string); got != reqID {
			t.Errorf("cancelled id %q does not match request id %q", got, reqID)
		}
		if got, _ := msg.Payload["reason"].(string); got != "cancelled" {
			t.Errorf("expected reason=cancelled, got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no permission_cancelled broadcast after ctx cancellation")
	}

	// The pending entry must be gone — otherwise a stale timeout sweep would
	// later double-broadcast permission_cancelled for the same id.
	srv.permPrompts.mu.Lock()
	_, stillPending := srv.permPrompts.pending[reqID]
	srv.permPrompts.mu.Unlock()
	if stillPending {
		t.Error("expected pending entry to be removed after ctx cancellation")
	}
}

// TestHandlePermissionResponse_UnknownIDIsNoop ensures a stale/unknown id
// (e.g. a duplicate reply, or a reply after timeout already fired) doesn't panic.
func TestHandlePermissionResponse_UnknownIDIsNoop(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HUGINN_HOME", fakeHome)
	t.Setenv("HOME", fakeHome)

	srv, ts := newTestServer(t)
	defer ts.Close()

	srv.handlePermissionResponse(WSMessage{Payload: map[string]any{"id": "nonexistent", "scope": "once"}})
}

// TestPermissionPromptFunc_NoSubscriberDeniesImmediately covers the headless /
// closed-tab case: the request has a session, but nobody is listening on it.
// Blocking would stall the agent's turn for the gate's full 30s timeout on
// every single bash call, so the bridge must fail closed right away.
func TestPermissionPromptFunc_NoSubscriberDeniesImmediately(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HUGINN_HOME", fakeHome)
	t.Setenv("HOME", fakeHome)

	srv, ts := newTestServer(t)
	defer ts.Close()

	done := make(chan permissions.Decision, 1)
	go func() {
		done <- srv.PermissionPromptFunc()(permissions.PermissionRequest{
			ToolName:  "bash",
			AgentName: "codey",
			SessionID: "sess-nobody-home",
			Args:      map[string]any{"command": "rm -rf /"},
		})
	}()

	select {
	case d := <-done:
		if d != permissions.Deny {
			t.Errorf("expected Deny with no subscriber, got %v", d)
		}
	case <-time.After(time.Second):
		t.Fatal("promptFunc blocked with no subscriber; expected an immediate deny")
	}

	// Nothing may be left registered — a blocked-forever entry would also mean
	// a banner nobody can answer.
	srv.permPrompts.mu.Lock()
	pending := len(srv.permPrompts.pending)
	srv.permPrompts.mu.Unlock()
	if pending != 0 {
		t.Errorf("expected no pending entries, got %d", pending)
	}
}

// TestPermissionPromptFunc_WildcardSubscriberCounts verifies that a client
// registered with an empty session ID (the wildcard subscription the web UI
// uses outside a single session) is treated as a listener, since
// broadcastToSession delivers to it.
func TestPermissionPromptFunc_WildcardSubscriberCounts(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HUGINN_HOME", fakeHome)
	t.Setenv("HOME", fakeHome)

	srv, ts := newTestServer(t)
	defer ts.Close()

	subscribePermissionPrompt(t, srv, "") // wildcard

	if !srv.hasSessionSubscribers("sess-any") {
		t.Fatal("wildcard client should count as a subscriber for any session")
	}
}

// TestPermissionPromptFunc_SanitizesCommandForBanner verifies the emitted
// banner payload is stripped of control characters and bounded in length —
// a command is attacker-influenced text (the model writes it) rendered in a
// human's approval UI, so it must not be able to forge lines or run off the
// end of the banner.
func TestPermissionPromptFunc_SanitizesCommandForBanner(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HUGINN_HOME", fakeHome)
	t.Setenv("HOME", fakeHome)

	srv, ts := newTestServer(t)
	defer ts.Close()

	out := subscribePermissionPrompt(t, srv, "sess-1")

	evil := "echo hi\n\x1b[2KAllow: yes\r\x00" + strings.Repeat("A", 1000)
	go func() {
		srv.PermissionPromptFunc()(permissions.PermissionRequest{
			ToolName:  "bash",
			AgentName: "codey",
			SessionID: "sess-1",
			Args:      map[string]any{"command": evil},
		})
	}()

	var msg WSMessage
	select {
	case msg = <-out:
	case <-time.After(2 * time.Second):
		t.Fatal("no permission_request broadcast")
	}
	if msg.Type != "permission_request" {
		t.Fatalf("expected permission_request, got %q", msg.Type)
	}
	cmd, _ := msg.Payload["command"].(string)
	if strings.ContainsAny(cmd, "\n\r\x00\x1b") {
		t.Errorf("command still contains control characters: %q", cmd)
	}
	if n := len([]rune(cmd)); n > promptTextMaxRunes+1 { // +1 for the ellipsis
		t.Errorf("command not truncated: %d runes", n)
	}
	if !strings.HasPrefix(cmd, "echo hi ") {
		t.Errorf("expected readable command prefix, got %q", cmd)
	}
}

// TestSanitizePromptText covers the edge cases directly.
func TestSanitizePromptText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"go test ./...", "go test ./..."},
		{"a\nb", "a b"},
		{"a\n\n\t b", "a b"},
		{"a\x1b[31mb", "a [31mb"},
		{"a\x00\x7fb", "a b"},
		{"héllo — ünicode", "héllo — ünicode"},
	}
	for _, c := range cases {
		if got := sanitizePromptText(c.in); got != c.want {
			t.Errorf("sanitizePromptText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	long := sanitizePromptText(strings.Repeat("x", promptTextMaxRunes+50))
	if !strings.HasSuffix(long, "…") {
		t.Errorf("expected truncation marker, got %q", long[len(long)-10:])
	}
	if n := len([]rune(long)); n != promptTextMaxRunes+1 {
		t.Errorf("expected %d runes after truncation, got %d", promptTextMaxRunes+1, n)
	}
	// Truncation must never split a multi-byte rune.
	if !utf8.ValidString(sanitizePromptText(strings.Repeat("é", promptTextMaxRunes+50))) {
		t.Error("truncated multi-byte string is not valid UTF-8")
	}
}

// TestPermissionPrompts_TimeoutBroadcastsCancelled verifies the UI is told to
// take the banner down when a prompt expires unanswered. Without this the
// banner stays up with buttons that silently do nothing.
func TestPermissionPrompts_TimeoutBroadcastsCancelled(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HUGINN_HOME", fakeHome)
	t.Setenv("HOME", fakeHome)

	orig := permissionPromptTimeout
	permissionPromptTimeout = 20 * time.Millisecond
	defer func() { permissionPromptTimeout = orig }()

	srv, ts := newTestServer(t)
	defer ts.Close()

	out := subscribePermissionPrompt(t, srv, "sess-1")

	go func() {
		srv.PermissionPromptFunc()(permissions.PermissionRequest{
			ToolName: "bash", AgentName: "codey", SessionID: "sess-1",
		})
	}()

	deadline := time.After(2 * time.Second)
	var reqID string
	for {
		select {
		case msg := <-out:
			switch msg.Type {
			case "permission_request":
				reqID, _ = msg.Payload["id"].(string)
			case "permission_cancelled":
				if reqID == "" {
					t.Fatal("permission_cancelled arrived before permission_request")
				}
				if got, _ := msg.Payload["id"].(string); got != reqID {
					t.Errorf("cancelled id %q does not match request id %q", got, reqID)
				}
				if got, _ := msg.Payload["reason"].(string); got != "timeout" {
					t.Errorf("expected reason=timeout, got %q", got)
				}
				return
			}
		case <-deadline:
			t.Fatal("no permission_cancelled broadcast after prompt timeout")
		}
	}
}
