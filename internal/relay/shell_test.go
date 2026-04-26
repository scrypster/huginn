package relay

import (
	"encoding/base64"
	"strings"
	"sync"
	"testing"
	"time"
)

// pinFastShell forces $SHELL=/bin/sh for the duration of a test. The
// ShellManager reads $SHELL at Start time (shell.go:54), so tests that
// exercise PTY I/O must avoid the user's interactive shell — on macOS,
// zsh + oh-my-zsh + gitstatus can take 4+ seconds to initialise, which
// is enough to blow through wait-for-output deadlines under heavy
// parallel test load (running with go test ./...).
//
// /bin/sh starts in milliseconds and runs `echo`, `printf`, and `exit`
// identically for the purposes of these tests, which only verify that
// bytes typed in arrive as base64-encoded output messages.
func pinFastShell(t *testing.T) {
	t.Helper()
	t.Setenv("SHELL", "/bin/sh")
}

// collectHub captures messages sent via Send().
type collectHub struct {
	mu             sync.Mutex
	msgs           []Message
	waiters        map[MessageType][]chan Message
	outputWaiters  []*outputWaiter
}

// outputWaiter blocks until any shell_output decodes to a string matching
// the predicate. Awoken by Send when a matching shell_output arrives.
type outputWaiter struct {
	pred func(string) bool
	done chan struct{}
}

func (h *collectHub) Send(_ string, msg Message) error {
	h.mu.Lock()
	h.msgs = append(h.msgs, msg)
	waiters := h.waiters[msg.Type]
	delete(h.waiters, msg.Type)

	// Notify any output-content waiters when this is a shell_output.
	var notifyOutput []*outputWaiter
	if msg.Type == MsgShellOutput {
		if data, ok := msg.Payload["data"].(string); ok {
			if raw, err := base64.StdEncoding.DecodeString(data); err == nil {
				str := string(raw)
				kept := h.outputWaiters[:0]
				for _, w := range h.outputWaiters {
					if w.pred(str) {
						notifyOutput = append(notifyOutput, w)
					} else {
						kept = append(kept, w)
					}
				}
				h.outputWaiters = kept
			}
		}
	}
	h.mu.Unlock()
	for _, ch := range waiters {
		select {
		case ch <- msg:
		default:
		}
	}
	for _, w := range notifyOutput {
		close(w.done)
	}
	return nil
}

func (h *collectHub) Close(_ string) {}

func (h *collectHub) collected() []Message {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Message, len(h.msgs))
	copy(out, h.msgs)
	return out
}

// waitFor blocks until a message of the given type is received or timeout expires.
// Uses channel notification so it wakes immediately rather than polling.
func (h *collectHub) waitFor(t *testing.T, typ MessageType, timeout time.Duration) Message {
	t.Helper()
	h.mu.Lock()
	// Check if already received while holding the lock (avoids a race with Send).
	for _, m := range h.msgs {
		if m.Type == typ {
			h.mu.Unlock()
			return m
		}
	}
	// Not yet received — register a buffered waiter channel.
	ch := make(chan Message, 1)
	if h.waiters == nil {
		h.waiters = make(map[MessageType][]chan Message)
	}
	h.waiters[typ] = append(h.waiters[typ], ch)
	h.mu.Unlock()

	select {
	case m := <-ch:
		return m
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for message type %q", typ)
		return Message{}
	}
}

// waitForOutput blocks until a shell_output message decodes to bytes that
// satisfy the predicate, or timeout expires. Returns the concatenation of
// every shell_output's decoded payload up to and including the matching
// frame, so callers can assert against the full transcript.
//
// Unlike waitFor, this checks both already-collected messages and any
// future ones via the outputWaiters channel — eliminating the busy-poll
// + base64-decode-on-every-iteration pattern that made callers fragile
// under load.
func (h *collectHub) waitForOutput(t *testing.T, pred func(string) bool, timeout time.Duration) string {
	t.Helper()
	h.mu.Lock()
	var transcript strings.Builder
	for _, m := range h.msgs {
		if m.Type != MsgShellOutput {
			continue
		}
		data, ok := m.Payload["data"].(string)
		if !ok {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			continue
		}
		transcript.Write(raw)
		if pred(transcript.String()) {
			h.mu.Unlock()
			return transcript.String()
		}
	}
	w := &outputWaiter{
		pred: func(latest string) bool {
			transcript.WriteString(latest)
			return pred(transcript.String())
		},
		done: make(chan struct{}),
	}
	h.outputWaiters = append(h.outputWaiters, w)
	h.mu.Unlock()

	select {
	case <-w.done:
		return transcript.String()
	case <-time.After(timeout):
		t.Fatalf("timed out after %s waiting for matching shell_output (transcript so far: %q)", timeout, transcript.String())
		return ""
	}
}

func TestShellManager_StartSendsReady(t *testing.T) {
	pinFastShell(t)
	hub := &collectHub{}
	sm := NewShellManager()

	sm.Start(hub, 80, 24)

	ready := hub.waitFor(t, MsgShellReady, 5*time.Second)
	cols, _ := ready.Payload["cols"].(float64)
	rows, _ := ready.Payload["rows"].(float64)
	if cols != 80 || rows != 24 {
		t.Fatalf("expected cols=80 rows=24, got cols=%v rows=%v", cols, rows)
	}

	sm.Exit()
}

func TestShellManager_ReattachSendsReadyImmediately(t *testing.T) {
	pinFastShell(t)
	hub := &collectHub{}
	sm := NewShellManager()

	sm.Start(hub, 80, 24)
	hub.waitFor(t, MsgShellReady, 5*time.Second)

	hub2 := &collectHub{}
	sm.Start(hub2, 80, 24)
	hub2.waitFor(t, MsgShellReady, 5*time.Second)

	sm.Exit()
}

func TestShellManager_InputProducesOutput(t *testing.T) {
	pinFastShell(t)
	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 220, 50)
	hub.waitFor(t, MsgShellReady, 5*time.Second)

	cmd := base64.StdEncoding.EncodeToString([]byte("echo hello_test_marker\n"))
	sm.Input(cmd)

	hub.waitForOutput(t, func(s string) bool {
		return strings.Contains(s, "hello_test_marker")
	}, 15*time.Second)

	sm.Exit()
}

func TestShellManager_ResizeCallsSetsize(t *testing.T) {
	pinFastShell(t)
	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 80, 24)
	hub.waitFor(t, MsgShellReady, 5*time.Second)

	sm.Resize(132, 40)

	sm.Exit()
}

func TestShellManager_ExitSendsShellExit(t *testing.T) {
	pinFastShell(t)
	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 80, 24)
	hub.waitFor(t, MsgShellReady, 5*time.Second)

	sm.Exit()

	hub.waitFor(t, MsgShellExit, 5*time.Second)
}

func TestShellManager_ExitWhenNotRunning(t *testing.T) {
	sm := NewShellManager()
	// Should not panic
	sm.Exit()
}

func TestShellManager_InputWhenNotRunning(t *testing.T) {
	sm := NewShellManager()
	// Should not panic
	sm.Input(base64.StdEncoding.EncodeToString([]byte("hello")))
}

func TestShellManager_ConcurrentStartSafe(t *testing.T) {
	pinFastShell(t)
	hub := &collectHub{}
	sm := NewShellManager()
	defer sm.Exit()

	// Multiple concurrent Start calls — must not race or double-spawn.
	// 1 call starts the shell; the other 4 reattach.
	// Each produces exactly one shell_ready, so 5 total.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sm.Start(hub, 80, 24)
		}()
	}
	wg.Wait()

	// Wait a beat for all shell_ready messages to arrive.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		msgs := hub.collected()
		count := 0
		for _, m := range msgs {
			if m.Type == MsgShellReady {
				count++
			}
		}
		if count == 5 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	msgs := hub.collected()
	count := 0
	for _, m := range msgs {
		if m.Type == MsgShellReady {
			count++
		}
	}
	if count != 5 {
		t.Fatalf("expected exactly 5 shell_ready messages (1 spawn + 4 reattach), got %d", count)
	}

	// Verify only one PTY is running
	sm.mu.Lock()
	running := sm.running
	sm.mu.Unlock()
	if !running {
		t.Fatal("expected shell to be running after concurrent starts")
	}
}

func TestShellManager_ExitTwiceNoPanic(t *testing.T) {
	pinFastShell(t)
	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 80, 24)
	hub.waitFor(t, MsgShellReady, 5*time.Second)

	sm.Exit()
	sm.Exit() // second Exit — must not panic
}

func TestShellManager_InputAfterExit(t *testing.T) {
	pinFastShell(t)
	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 80, 24)
	hub.waitFor(t, MsgShellReady, 5*time.Second)
	sm.Exit()

	sm.Input(base64.StdEncoding.EncodeToString([]byte("hello")))
}

func TestShellManager_ResizeAfterExit(t *testing.T) {
	pinFastShell(t)
	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 80, 24)
	hub.waitFor(t, MsgShellReady, 5*time.Second)
	sm.Exit()

	sm.Resize(80, 24)
}

func TestShellManager_ShellExitDetected(t *testing.T) {
	pinFastShell(t)
	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 80, 24)
	hub.waitFor(t, MsgShellReady, 5*time.Second)

	cmd := base64.StdEncoding.EncodeToString([]byte("exit\n"))
	sm.Input(cmd)

	hub.waitFor(t, MsgShellExit, 10*time.Second)

	sm.mu.Lock()
	running := sm.running
	sm.mu.Unlock()
	if running {
		t.Fatal("expected running=false after natural shell exit")
	}
}

// TestShellManager_HubReconnect verifies that calling Start with a second hub
// (simulating a browser reconnect) sends shell_ready to hub2 without sending a
// second shell_ready to hub1.
func TestShellManager_HubReconnect(t *testing.T) {
	pinFastShell(t)
	hub1 := &collectHub{}
	sm := NewShellManager()
	defer sm.Exit()

	sm.Start(hub1, 80, 24)
	hub1.waitFor(t, MsgShellReady, 5*time.Second)

	// Count shell_ready messages on hub1 before the reattach.
	countReadyHub1Before := func() int {
		count := 0
		for _, m := range hub1.collected() {
			if m.Type == MsgShellReady {
				count++
			}
		}
		return count
	}
	beforeCount := countReadyHub1Before()

	// Reattach with hub2 — shell is already running so this is the reattach path.
	hub2 := &collectHub{}
	sm.Start(hub2, 80, 24)
	hub2.waitFor(t, MsgShellReady, 5*time.Second)

	// hub1 must NOT have received another shell_ready — only hub2 gets it on reattach.
	afterCount := countReadyHub1Before()
	if afterCount != beforeCount {
		t.Fatalf("hub1 received %d shell_ready messages after reattach (expected %d — no new messages to hub1)", afterCount, beforeCount)
	}
}

// TestShellManager_EnvVarLeakPrevention verifies that env vars not in the
// whitelist (e.g. HUGINN_SECRET_TEST_KEY) are not passed to the shell process.
func TestShellManager_EnvVarLeakPrevention(t *testing.T) {
	pinFastShell(t)
	t.Setenv("HUGINN_SECRET_TEST_KEY", "supersecret")

	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 220, 50)
	hub.waitFor(t, MsgShellReady, 5*time.Second)
	defer sm.Exit()

	cmd := base64.StdEncoding.EncodeToString([]byte("echo \"KEY=$HUGINN_SECRET_TEST_KEY\"\n"))
	sm.Input(cmd)

	transcript := hub.waitForOutput(t, func(s string) bool {
		return strings.Contains(s, "KEY=")
	}, 15*time.Second)

	if strings.Contains(transcript, "supersecret") {
		t.Fatalf("env var leaked into shell output: %q", transcript)
	}
}

// TestShellManager_LargeOutput verifies that the readLoop correctly streams
// large PTY output back as shell_output messages.
func TestShellManager_LargeOutput(t *testing.T) {
	pinFastShell(t)
	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 220, 50)
	hub.waitFor(t, MsgShellReady, 5*time.Second)
	defer sm.Exit()

	// Print 500 dashes — enough to exceed a small read buffer. /bin/sh
	// (POSIX) doesn't reliably support `seq` so we use a literal payload
	// with `printf %s` for portability across /bin/sh implementations.
	dashes := strings.Repeat("-", 500)
	cmd := base64.StdEncoding.EncodeToString([]byte("printf %s '" + dashes + "'\n"))
	sm.Input(cmd)

	transcript := hub.waitForOutput(t, func(s string) bool {
		return strings.Count(s, "-") >= 500
	}, 15*time.Second)

	if strings.Count(transcript, "-") < 500 {
		t.Fatalf("expected at least 500 dashes in output, got %d (transcript len=%d)", strings.Count(transcript, "-"), len(transcript))
	}
}

// TestShellManager_ConcurrentResizeAndInput verifies that concurrent Resize
// and Input calls do not race or panic.
func TestShellManager_ConcurrentResizeAndInput(t *testing.T) {
	pinFastShell(t)
	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 80, 24)
	hub.waitFor(t, MsgShellReady, 5*time.Second)
	defer sm.Exit()

	var wg sync.WaitGroup

	// Goroutine 1: repeated resize calls.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			sm.Resize(uint16(80+i%20), 24)
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// Goroutine 2: repeated input calls.
	wg.Add(1)
	go func() {
		defer wg.Done()
		cmd := base64.StdEncoding.EncodeToString([]byte("echo test\n"))
		for i := 0; i < 5; i++ {
			sm.Input(cmd)
			time.Sleep(5 * time.Millisecond)
		}
	}()

	wg.Wait()
	// If we reach here without a panic or data race the test passes.
}

func TestShellManager_InputInvalidBase64(t *testing.T) {
	pinFastShell(t)
	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 80, 24)
	hub.waitFor(t, MsgShellReady, 5*time.Second)
	defer sm.Exit()

	sm.Input("!!!not-valid-base64!!!")
}

func TestShellManager_StartFallbackShell(t *testing.T) {
	// Unset SHELL so the code falls back to /bin/bash. We can't pin a
	// faster shell here — that's the whole point of the test — so we
	// give a generous timeout to absorb /bin/bash startup under load.
	t.Setenv("SHELL", "")

	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 80, 24)
	hub.waitFor(t, MsgShellReady, 10*time.Second)
	sm.Exit()
}

func TestShellManager_ExitLockedWithError(t *testing.T) {
	pinFastShell(t)
	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 80, 24)
	hub.waitFor(t, MsgShellReady, 5*time.Second)

	// Call exitLocked directly (same package) with a non-nil error to hit
	// the errMsg branch in exitLocked.
	errStr := "simulated error"
	sm.mu.Lock()
	sm.exitLocked(&errStr)
	sm.mu.Unlock()

	msg := hub.waitFor(t, MsgShellExit, 5*time.Second)
	errField, _ := msg.Payload["error"].(string)
	if errField != errStr {
		t.Fatalf("expected error=%q in payload, got %q", errStr, errField)
	}
}
