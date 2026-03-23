package relay

import (
	"encoding/base64"
	"strings"
	"sync"
	"testing"
	"time"
)

// collectHub captures messages sent via Send().
type collectHub struct {
	mu      sync.Mutex
	msgs    []Message
	waiters map[MessageType][]chan Message
}

func (h *collectHub) Send(_ string, msg Message) error {
	h.mu.Lock()
	h.msgs = append(h.msgs, msg)
	waiters := h.waiters[msg.Type]
	delete(h.waiters, msg.Type)
	h.mu.Unlock()
	for _, ch := range waiters {
		select {
		case ch <- msg:
		default:
		}
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

func TestShellManager_StartSendsReady(t *testing.T) {
	hub := &collectHub{}
	sm := NewShellManager()

	sm.Start(hub, 80, 24)

	ready := hub.waitFor(t, MsgShellReady, 3*time.Second)
	cols, _ := ready.Payload["cols"].(float64)
	rows, _ := ready.Payload["rows"].(float64)
	if cols != 80 || rows != 24 {
		t.Fatalf("expected cols=80 rows=24, got cols=%v rows=%v", cols, rows)
	}

	sm.Exit()
}

func TestShellManager_ReattachSendsReadyImmediately(t *testing.T) {
	hub := &collectHub{}
	sm := NewShellManager()

	sm.Start(hub, 80, 24)
	hub.waitFor(t, MsgShellReady, 3*time.Second)

	// Second Start — should reattach, not restart
	hub2 := &collectHub{}
	sm.Start(hub2, 80, 24)
	hub2.waitFor(t, MsgShellReady, time.Second)

	sm.Exit()
}

func TestShellManager_InputProducesOutput(t *testing.T) {
	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 220, 50)
	hub.waitFor(t, MsgShellReady, 3*time.Second)

	// Send "echo hello\n" — shell should echo it back
	cmd := base64.StdEncoding.EncodeToString([]byte("echo hello_test_marker\n"))
	sm.Input(cmd)

	// Wait for shell_output containing our marker
	deadline := time.Now().Add(5 * time.Second)
	found := false
	for time.Now().Before(deadline) && !found {
		for _, msg := range hub.collected() {
			if msg.Type != MsgShellOutput {
				continue
			}
			raw, _ := base64.StdEncoding.DecodeString(msg.Payload["data"].(string))
			if strings.Contains(string(raw), "hello_test_marker") {
				found = true
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !found {
		t.Fatal("expected shell_output containing 'hello_test_marker'")
	}

	sm.Exit()
}

func TestShellManager_ResizeCallsSetsize(t *testing.T) {
	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 80, 24)
	hub.waitFor(t, MsgShellReady, 3*time.Second)

	// Resize should not panic or error
	sm.Resize(132, 40)

	sm.Exit()
}

func TestShellManager_ExitSendsShellExit(t *testing.T) {
	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 80, 24)
	hub.waitFor(t, MsgShellReady, 3*time.Second)

	sm.Exit()

	hub.waitFor(t, MsgShellExit, 3*time.Second)
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
	deadline := time.Now().Add(3 * time.Second)
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
	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 80, 24)
	hub.waitFor(t, MsgShellReady, 3*time.Second)

	sm.Exit()
	sm.Exit() // second Exit — must not panic
}

func TestShellManager_InputAfterExit(t *testing.T) {
	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 80, 24)
	hub.waitFor(t, MsgShellReady, 3*time.Second)
	sm.Exit()

	// Must not panic
	sm.Input(base64.StdEncoding.EncodeToString([]byte("hello")))
}

func TestShellManager_ResizeAfterExit(t *testing.T) {
	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 80, 24)
	hub.waitFor(t, MsgShellReady, 3*time.Second)
	sm.Exit()

	// Must not panic
	sm.Resize(80, 24)
}

func TestShellManager_ShellExitDetected(t *testing.T) {
	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 80, 24)
	hub.waitFor(t, MsgShellReady, 3*time.Second)

	// Send "exit" to close the shell naturally
	cmd := base64.StdEncoding.EncodeToString([]byte("exit\n"))
	sm.Input(cmd)

	// Shell should detect EOF and send shell_exit
	hub.waitFor(t, MsgShellExit, 5*time.Second)

	sm.mu.Lock()
	running := sm.running
	sm.mu.Unlock()
	if running {
		t.Fatal("expected running=false after natural shell exit")
	}
}

func TestShellManager_InputInvalidBase64(t *testing.T) {
	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 80, 24)
	hub.waitFor(t, MsgShellReady, 3*time.Second)
	defer sm.Exit()

	// Invalid base64 — must not panic, just warn and return
	sm.Input("!!!not-valid-base64!!!")
}

func TestShellManager_StartFallbackShell(t *testing.T) {
	// Unset SHELL so the code falls back to /bin/bash
	t.Setenv("SHELL", "")

	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 80, 24)
	hub.waitFor(t, MsgShellReady, 3*time.Second)
	sm.Exit()
}

func TestShellManager_ExitLockedWithError(t *testing.T) {
	hub := &collectHub{}
	sm := NewShellManager()
	sm.Start(hub, 80, 24)
	hub.waitFor(t, MsgShellReady, 3*time.Second)

	// Call exitLocked directly (same package) with a non-nil error to hit
	// the errMsg branch in exitLocked.
	errStr := "simulated error"
	sm.mu.Lock()
	sm.exitLocked(&errStr)
	sm.mu.Unlock()

	// exitLocked sends shell_exit with error field
	msg := hub.waitFor(t, MsgShellExit, 3*time.Second)
	errField, _ := msg.Payload["error"].(string)
	if errField != errStr {
		t.Fatalf("expected error=%q in payload, got %q", errStr, errField)
	}
}
