package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/claudecode/approvals"
)

// TestApprovalWriteDeadlineExtendableThroughMiddleware pins the transport half
// of the approval feature.
//
// handleClaudeApprove blocks for up to approvalDeadline (285s) while a human
// decides, but http.Server.WriteTimeout (server.go) is 120s and Go arms that
// deadline BEFORE the handler runs and never resets it. The handler therefore
// has to push the deadline out itself with http.NewResponseController. That
// only works if every ResponseWriter wrapper on this route exposes
// Unwrap() http.ResponseWriter — otherwise the controller cannot reach the
// real connection, SetWriteDeadline returns http.ErrNotSupported, and a human
// clicking Allow at t=150s gets "approved" in the UI while the hook sees a
// closed connection and denies.
//
// So this test asserts the real thing: SetWriteDeadline succeeds on a
// ResponseWriter wrapped exactly the way POST /api/v1/claude/approve wraps it,
// served by a real http.Server (httptest), not a recorder.
func TestApprovalWriteDeadlineExtendableThroughMiddleware(t *testing.T) {
	var setErr error
	var called bool

	handler := loggingMiddleware(requestIDMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
		setErr = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Minute))
		w.WriteHeader(http.StatusOK)
	}))

	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if !called {
		t.Fatal("handler never ran")
	}
	if setErr != nil {
		t.Fatalf("SetWriteDeadline through the approval route's middleware chain = %v, want nil; "+
			"a wrapper in loggingMiddleware/requestIDMiddleware is hiding the connection "+
			"(add Unwrap() http.ResponseWriter to it). Without this, any approval decided "+
			"after http.Server.WriteTimeout is written to a dead connection and the tool is denied.", setErr)
	}
}

// TestApproveExtendsWriteDeadlineOnRealConnection exercises the handler itself
// through the real middleware chain and a real connection, and proves the
// deadline it installs actually outlives the server's WriteTimeout: the
// response is written long after that timeout would have killed it.
func TestApproveExtendsWriteDeadlineOnRealConnection(t *testing.T) {
	handler := loggingMiddleware(requestIDMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Minute)); err != nil {
			t.Errorf("SetWriteDeadline: %v", err)
		}
		// Sleep past the server's WriteTimeout below. Without the extension the
		// write that follows lands on an expired deadline and the client sees a
		// transport error instead of a body.
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`{"decision":"allow"}`))
	}))

	srv := httptest.NewUnstartedServer(handler)
	srv.Config.WriteTimeout = 100 * time.Millisecond
	srv.Start()
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v — a late response never reached the client, which is exactly "+
			"the failure a hook reports as \"Huginn unreachable\" and turns into a deny", err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 64)
	n, _ := resp.Body.Read(buf)
	if got := string(buf[:n]); got != `{"decision":"allow"}` {
		t.Fatalf("body = %q, want the late-written decision", got)
	}
}

// TestApproveLateAllowReachesTheHook is the end-to-end version: the REAL
// handler, behind the REAL route middleware, on a REAL connection whose
// WriteTimeout expires long before the human decides. It is the scenario the
// bug produced in production, scaled down in time — a human clicking Allow
// after the server's WriteTimeout must still reach the hook as "allow".
//
// It fails if handleClaudeApprove stops extending its own write deadline.
func TestApproveLateAllowReachesTheHook(t *testing.T) {
	s := &Server{}
	s.approvals = approvals.New(10 * time.Second)
	defer s.approvals.Close()
	s.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{{
			Name: "codey", ClaudeSessionID: "sess",
		}}}, nil
	}

	srv := httptest.NewUnstartedServer(
		loggingMiddleware(requestIDMiddleware(s.handleClaudeApprove)))
	// Far below the time the human takes to answer, exactly as the production
	// 120s WriteTimeout sits below the 285s approval deadline.
	srv.Config.WriteTimeout = 100 * time.Millisecond
	srv.Start()
	defer srv.Close()

	// Answer Allow only after the WriteTimeout has certainly elapsed.
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if v := s.approvals.List(); len(v) == 1 {
				time.Sleep(250 * time.Millisecond) // let WriteTimeout pass
				s.approvals.Deliver(v[0].ID, approvals.Allow)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	resp, err := http.Post(srv.URL, "application/json",
		strings.NewReader(`{"tool_name":"Bash","session_id":"sess","summary":"ls"}`))
	if err != nil {
		t.Fatalf("POST: %v — the hook client sees this as \"Huginn unreachable\" and DENIES, "+
			"while the human was told the tool was approved. handleClaudeApprove must extend "+
			"this response's write deadline past approvalDeadline.", err)
	}
	defer resp.Body.Close()

	var got map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode decision: %v", err)
	}
	if got["decision"] != "allow" {
		t.Fatalf("decision = %q, want allow", got["decision"])
	}
}

// TestStopReleasesPendingApprovals is the Ctrl-C test.
//
// Server.Stop calls srv.Shutdown, which WAITS for in-flight handlers, and
// main.go passes context.Background(). handleClaudeApprove blocks for up to
// approvalDeadline (285s), so a single pending approval made Ctrl-C look like
// a hang for nearly five minutes — and because the signal had already fired, a
// second Ctrl-C was swallowed. Store.Close exists precisely to release every
// waiter with Deny; Stop has to call it, and call it BEFORE Shutdown.
func TestStopReleasesPendingApprovals(t *testing.T) {
	s := &Server{}
	// A deadline far longer than this test is willing to wait: if Stop does not
	// release the waiter, this test fails on its own timeout rather than
	// passing slowly.
	s.approvals = approvals.New(5 * time.Minute)
	s.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{{
			Name: "codey", ClaudeSessionID: "sess",
		}}}, nil
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.srv = &http.Server{
		Handler: loggingMiddleware(requestIDMiddleware(s.handleClaudeApprove)),
	}
	go func() { _ = s.srv.Serve(ln) }()

	decided := make(chan string, 1)
	go func() {
		resp, err := http.Post("http://"+ln.Addr().String(), "application/json",
			strings.NewReader(`{"tool_name":"Bash","session_id":"sess","summary":"ls"}`))
		if err != nil {
			decided <- "transport-error"
			return
		}
		defer resp.Body.Close()
		var got map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&got)
		decided <- got["decision"]
	}()

	// Wait for the approval to actually be parked before shutting down.
	for i := 0; i < 400 && len(s.approvals.List()) == 0; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if len(s.approvals.List()) != 1 {
		t.Fatal("no approval was pending; the test never reached the blocking path")
	}

	stopped := make(chan error, 1)
	start := time.Now()
	go func() { stopped <- s.Stop(context.Background()) }()

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Stop did not return with an approval pending: Ctrl-C hangs for up to " +
			"approvalDeadline, and the second Ctrl-C is swallowed because the signal " +
			"already fired. Stop must call approvals.Close() before srv.Shutdown.")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Stop took %v with one approval pending", elapsed)
	}

	// Fail-closed: the released waiter must DENY, never allow.
	select {
	case d := <-decided:
		if d != "deny" {
			t.Fatalf("decision on shutdown = %q, want deny — a released waiter must never allow", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the hook never received a decision after shutdown")
	}
}
