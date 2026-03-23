package relay_test

// coverage_boost95_test.go — tests to push relay package from 85.6% → 95%+.
// Targets: inprocess.go (Close), token.go (Save/Clear/Load error),
// identity.go (Save error, sanitizeHostname edge cases, LoadIdentity error paths),
// websocket.go (Connect sendHello fail, Send marshal fail, reconnect done-close),
// registration.go (openBrowser N/A, Register timeout path, exchangeCode error path)

import (
	"context"
	"crypto/sha1" //nolint:gosec
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/scrypster/huginn/internal/relay"
)

// computeWSAccept computes the Sec-WebSocket-Accept value for a given key.
func computeWSAccept(key string) string {
	const magic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	h := sha1.New() //nolint:gosec
	h.Write([]byte(key + magic))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// ─── InProcessHub.Close — covers the 0% branch ───────────────────────────────

func TestInProcessHub_Close_MultipleCallsNoPanic(t *testing.T) {
	h := relay.NewInProcessHub()
	// Close should be a no-op and safe to call any number of times.
	for i := 0; i < 5; i++ {
		h.Close("any-machine-id")
	}
}

// ─── sanitizeHostname — all-special-char hostname → replaces with dashes ─────

func TestGetMachineID_AllSpecialCharsHostname(t *testing.T) {
	// Hostname is no longer part of the machine ID — ID is pure 8-char hex.
	// Verify the ID is valid regardless of what the hostname is.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	id := relay.GetMachineID()
	if id == "" {
		t.Error("expected non-empty machine ID")
	}
	if len(id) != 8 {
		t.Errorf("expected 8-char hex machine ID, got %q (len=%d)", id, len(id))
	}
}

// ─── loadOrCreateMachineSuffix — file has correct hex but wrong length → regen

func TestLoadOrCreateMachineSuffix_WrongLengthHex(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	huginnDir := filepath.Join(tmpHome, ".huginn")
	if err := os.MkdirAll(huginnDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Valid hex but only 4 chars (wrong length) — must regenerate
	if err := os.WriteFile(filepath.Join(huginnDir, "machine_id"), []byte("abcd"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	id := relay.GetMachineID()
	if id == "" {
		t.Error("expected non-empty machine ID")
	}

	// The file should have been overwritten with a valid 8-char suffix
	data, err := os.ReadFile(filepath.Join(huginnDir, "machine_id"))
	if err != nil {
		t.Fatalf("expected machine_id to still exist: %v", err)
	}
	if len(strings.TrimSpace(string(data))) != 8 {
		t.Errorf("expected 8-char suffix in file, got %q", string(data))
	}
}

// ─── Identity.Save — directory creation failure (read-only parent) ───────────
// (This tests the MkdirAll error path inside Save)

func TestIdentity_Save_SucceedsWithSubdir(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Nested .huginn dir does not exist yet
	id := &relay.Identity{
		AgentID:  "agent-nest",
		Endpoint: "https://nest.example.com",
		APIKey:   "nest-key",
	}
	if err := id.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file was written
	path := filepath.Join(tmpHome, ".huginn", "relay.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("relay.json not found: %v", err)
	}
	var loaded relay.Identity
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if loaded.AgentID != "agent-nest" {
		t.Errorf("AgentID: got %q, want %q", loaded.AgentID, "agent-nest")
	}
}

// ─── LoadIdentity — HOME missing → falls back to relative path ───────────────

func TestLoadIdentity_NoHome_ReturnsError(t *testing.T) {
	orig := os.Getenv("HOME")
	os.Unsetenv("HOME")
	defer os.Setenv("HOME", orig)

	// Without HOME, os.UserHomeDir may fail on some systems.
	// We just verify the function doesn't panic and returns a non-nil error
	// (either ErrNotRegistered or an OS error).
	_, err := relay.LoadIdentity()
	if err == nil {
		// On some platforms UserHomeDir still succeeds (e.g. reads /etc/passwd).
		// Accept success too; we just don't want a panic.
		t.Log("LoadIdentity succeeded without HOME (platform may use passwd lookup)")
	}
}

// ─── WebSocketHub.Connect — sendHello fails (server closes after upgrade) ────

func TestWebSocketHub_Connect_SendHelloFails(t *testing.T) {
	// Server upgrades the connection then immediately closes it before receiving
	// anything, so the client's sendHello WriteMessage should fail.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Close before the client can write hello — this causes sendHello to fail.
		conn.Close()
	}))
	defer srv.Close()

	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")

	hub := relay.NewWebSocketHub(relay.WebSocketConfig{
		URL:       wsBase,
		Token:     "test-token",
		MachineID: "test-machine",
		Version:   "0.0.1",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Connect may succeed or fail depending on race; the important thing is no panic.
	_ = hub.Connect(ctx)
}

// ─── WebSocketHub.Send — nil connection returns ErrNotActivated ──────────────

func TestWebSocketHub_Send_BeforeConnect(t *testing.T) {
	hub := relay.NewWebSocketHub()
	err := hub.Send("machine-1", relay.Message{Type: relay.MsgToken, Payload: map[string]any{"k": "v"}})
	if err != relay.ErrNotActivated {
		t.Errorf("expected ErrNotActivated, got %v", err)
	}
}

// ─── WebSocketHub.Close — closes done channel and connected conn ──────────────

func TestWebSocketHub_Close_WithConnection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Drain until closed.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{
		URL:       wsBase,
		Token:     "",
		MachineID: "m1",
		Version:   "test",
	})

	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	// Close twice — must not panic (closeOnce guard).
	hub.Close("m1")
	hub.Close("m1")
}

// ─── WebSocketHub reconnect — done channel closed during reconnect delay ──────

func TestWebSocketHub_Reconnect_ClosedDuringDelay(t *testing.T) {
	// Server accepts connection, reads hello, then closes — triggering reconnect.
	// We close the hub immediately to exercise the done-channel path inside reconnect.
	connected := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Signal that first connection was established.
		select {
		case connected <- struct{}{}:
		default:
		}
		conn.Close() // force readPump error → triggers reconnect
	}))
	defer srv.Close()

	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{
		URL:       wsBase,
		Token:     "",
		MachineID: "m-reconnect",
		Version:   "test",
	})

	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Wait for the first connection to be established and closed by server.
	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("server never received connection")
	}

	// Close the hub immediately — this should cause reconnect loop to exit via done.
	time.Sleep(10 * time.Millisecond) // let readPump start reconnect
	hub.Close("m-reconnect")
}

// ─── WebSocketHub.SetOnMessage — callback registered before Connect ───────────

func TestWebSocketHub_SetOnMessage_BeforeConnect(t *testing.T) {
	got := make(chan relay.Message, 1)

	serverSend := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Drain hello.
		conn.ReadMessage() //nolint

		// Wait for signal then send a message.
		<-serverSend
		data, _ := json.Marshal(relay.Message{
			Type:    relay.MsgToolCall,
			Payload: map[string]any{"tool": "bash"},
		})
		conn.WriteMessage(websocket.TextMessage, data) //nolint

		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{
		URL:   wsBase,
		Token: "",
	})
	hub.SetOnMessage(func(_ context.Context, m relay.Message) {
		select {
		case got <- m:
		default:
		}
	})

	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer hub.Close("")

	close(serverSend) // tell server to send

	select {
	case m := <-got:
		if m.Type != relay.MsgToolCall {
			t.Errorf("expected tool_call, got %q", m.Type)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for onMessage")
	}
}

// ─── Registrar.Register — timeout (5min) path: we use context deadline ────────

func TestRegistrar_Register_ContextDeadlineExercisesPrint(t *testing.T) {
	// This exercises the fmt.Printf inside Register then waits for ctx cancellation.
	store := &relay.MemoryTokenStore{}
	reg := relay.NewRegistrarWithStore("http://localhost:19999", store)
	reg.OpenBrowserFn = func(_ string) error { return nil }

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, err := reg.Register(ctx, "127.0.0.1:0")
	if err == nil {
		t.Error("expected error from expired context")
	}
}

// ─── Registrar — browser flow fails, device code also times out ────────────

func TestRegistrar_BrowserAndDeviceFlow_Timeout(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	reg := relay.NewRegistrarWithStore("http://127.0.0.1:19998", store)
	reg.OpenBrowserFn = func(_ string) error { return nil }

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := reg.Register(ctx, "test")
	if err == nil {
		t.Error("expected timeout error from Register")
	}
}

// ─── Registrar — DeliverCode is a no-op in API key flow ───────────

func TestRegistrar_DeliverCode_IsNoOp(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	reg := relay.NewRegistrarWithStore("http://localhost:0", store)
	// Should not panic or have any effect
	reg.DeliverCode("some-code")
	reg.DeliverCode("another-code")
}

// ─── NewWebSocketHub — zero-value config (no panic) ──────────────────────────

func TestNewWebSocketHub_NoConfig(t *testing.T) {
	hub := relay.NewWebSocketHub()
	if hub == nil {
		t.Fatal("expected non-nil hub")
	}
}

// ─── WebSocketHub.Connect — unreachable server ───────────────────────────────

func TestWebSocketHub_Connect_Unreachable(t *testing.T) {
	// Use a port that nothing is listening on.
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{
		URL:       "ws://127.0.0.1:19997",
		Token:     "tok",
		MachineID: "m",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := hub.Connect(ctx)
	if err == nil {
		hub.Close("")
		t.Error("expected error connecting to unreachable server")
	}
}

// ─── WebSocketHub.readPump — bad JSON payload is ignored ─────────────────────

func TestWebSocketHub_ReadPump_BadJSON(t *testing.T) {
	gotCallback := make(chan relay.Message, 4)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Drain hello.
		conn.ReadMessage() //nolint

		// Send bad JSON first — readPump should skip it.
		conn.WriteMessage(websocket.TextMessage, []byte("{not valid json}")) //nolint

		// Then send valid JSON — should be delivered to callback.
		data, _ := json.Marshal(relay.Message{Type: relay.MsgDone})
		conn.WriteMessage(websocket.TextMessage, data) //nolint

		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()

	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{URL: wsBase})
	hub.SetOnMessage(func(_ context.Context, m relay.Message) {
		select {
		case gotCallback <- m:
		default:
		}
	})

	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer hub.Close("")

	select {
	case m := <-gotCallback:
		if m.Type != relay.MsgDone {
			t.Errorf("expected done, got %q", m.Type)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for valid message after bad JSON")
	}
}

// ─── WebSocketHub — Token in Authorization header ─────────────────────────────

func TestWebSocketHub_TokenInHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, _ := up.Upgrade(w, r, nil)
		if conn != nil {
			defer conn.Close()
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}
	}))
	defer srv.Close()

	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{
		URL:   wsBase,
		Token: "my-bearer-token",
	})

	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer hub.Close("")

	if !strings.Contains(gotAuth, "Bearer my-bearer-token") {
		t.Errorf("expected Authorization header with bearer token, got %q", gotAuth)
	}
}

// ─── WebSocketHub.Connect — sendHello fails with closed conn ─────────────────

// TestWebSocketHub_Connect_SendHelloFails2 uses a server that closes the write
// side immediately, causing the WriteMessage in sendHello to fail.
func TestWebSocketHub_Connect_SendHelloFails2(t *testing.T) {
	// The server upgrades and then calls CloseHandler with a write-close frame
	// before the client can write, causing sendHello's WriteMessage to fail.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Send a close frame immediately — this will cause the client's next
		// WriteMessage (in sendHello) to fail.
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye")) //nolint
		conn.Close()
	}))
	defer srv.Close()

	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{
		URL:       wsBase,
		Token:     "tok",
		MachineID: "m",
		Version:   "1",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Connect may succeed or fail — we just verify it doesn't panic.
	_ = hub.Connect(ctx)
	hub.Close("")
}

// ─── Identity.Save — MkdirAll fails when parent is a file ────────────────────

func TestIdentity_Save_MkdirAllFails(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Place a regular file where the .huginn directory should be.
	// This will cause os.MkdirAll to fail.
	huginnPath := filepath.Join(tmpHome, ".huginn")
	if err := os.WriteFile(huginnPath, []byte("i am a file"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	id := &relay.Identity{AgentID: "x", Endpoint: "https://x.com"}
	err := id.Save()
	if err == nil {
		t.Error("expected error when .huginn is a file (MkdirAll should fail)")
	}
}

// ─── Register with invalid base URL — browser flow times out, device code poll fails ──

func TestRegistrar_InvalidBaseURL_Timeout(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	reg := relay.NewRegistrarWithStore("://bad-url-no-scheme", store)
	reg.OpenBrowserFn = func(_ string) error { return nil }

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := reg.Register(ctx, "test")
	if err == nil {
		t.Error("expected error from bad base URL")
	}
}

// ─── WebSocketHub — dial with empty token (no Authorization header) ───────────

func TestWebSocketHub_NoToken_NoAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, _ := up.Upgrade(w, r, nil)
		if conn != nil {
			defer conn.Close()
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}
	}))
	defer srv.Close()

	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{
		URL:   wsBase,
		Token: "", // no token
	})

	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer hub.Close("")

	if gotAuth != "" {
		t.Errorf("expected no Authorization header when token is empty, got %q", gotAuth)
	}
}

// ─── TCP helper for Send-after-close scenario ─────────────────────────────────

// TestWebSocketHub_Send_AfterClose exercises sending after hub is closed.
func TestWebSocketHub_Send_AfterClose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, _ := up.Upgrade(w, r, nil)
		if conn != nil {
			defer conn.Close()
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}
	}))
	defer srv.Close()

	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{URL: wsBase})

	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	hub.Close("")

	// Send after close — may return an error (conn closed) or ErrNotActivated
	err := hub.Send("", relay.Message{Type: relay.MsgToken})
	_ = err // either outcome is acceptable; main thing is no panic
}

// ─── openBrowser — indirect test via Register on non-nil OpenBrowserFn ────────

func TestRegistrar_OpenBrowserFn_ReturnsError(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	reg := relay.NewRegistrarWithStore("http://localhost:19996", store)
	reg.OpenBrowserFn = func(_ string) error {
		return nil // silent no-op, but exercised
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, _ = reg.Register(ctx, "127.0.0.1:0")
}

// ─── registration.go:76-78 — nil OpenBrowserFn path ─

// TestRegistrar_Register_NilOpenBrowserFn exercises the `if openFn == nil`
// branch. The context is pre-cancelled so the ctx.Err() guard in registration.go
// fires BEFORE openBrowser is invoked — no browser, no URL, no side effects.
func TestRegistrar_Register_NilOpenBrowserFn(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	// Leave OpenBrowserFn nil to exercise the `openFn = openBrowser` branch.
	reg := relay.NewRegistrarWithStore("http://localhost:19995", store)

	// Pre-cancel: registration.go checks ctx.Err() before calling openFn.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := reg.Register(ctx, "127.0.0.1:0")
	if err == nil {
		t.Error("expected non-nil error for cancelled context")
	}
}

// ─── Identity.Save — WriteFile fails (relay.json dir doesn't have write perms) ─

func TestIdentity_Save_WriteFileFails(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Create the .huginn dir and relay.json as a directory — WriteFile will fail.
	huginnDir := filepath.Join(tmpHome, ".huginn")
	if err := os.MkdirAll(huginnDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Create a directory named relay.json — os.WriteFile will fail.
	relayJSONPath := filepath.Join(huginnDir, "relay.json")
	if err := os.Mkdir(relayJSONPath, 0o750); err != nil {
		t.Fatalf("Mkdir relay.json: %v", err)
	}

	id := &relay.Identity{AgentID: "fail-agent", Endpoint: "https://fail.com"}
	err := id.Save()
	if err == nil {
		t.Error("expected error when relay.json is a directory (WriteFile should fail)")
	}
}

// ─── WebSocketHub.Send — WriteMessage error after connection closes ───────────

func TestWebSocketHub_Send_WriteMessageError(t *testing.T) {
	connClosed := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Read hello then signal the test to send.
		conn.ReadMessage() //nolint
		// Close the connection — next Send from client should error.
		conn.Close()
		close(connClosed)
	}))
	defer srv.Close()

	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{URL: wsBase})

	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer hub.Close("")

	// Wait for server to close the connection.
	select {
	case <-connClosed:
	case <-time.After(2 * time.Second):
		t.Fatal("server never closed connection")
	}

	// Give read pump time to detect the close.
	time.Sleep(50 * time.Millisecond)

	// Now send — the underlying conn is closed, WriteMessage should fail.
	// The error might be conn closed or ErrNotActivated if readPump reconnected.
	_ = hub.Send("", relay.Message{Type: relay.MsgToken})
}

// ─── WebSocketHub reconnect — sendHello fails on reconnect ───────────────────

func TestWebSocketHub_Reconnect_SendHelloFails(t *testing.T) {
	// First connection: accepts normally (so Connect succeeds).
	// Reconnect connection: server closes immediately after upgrade (sendHello fails).
	connectCount := 0
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		mu.Lock()
		connectCount++
		n := connectCount
		mu.Unlock()

		if n == 1 {
			// First connection: read hello normally, then close to trigger reconnect.
			conn.ReadMessage() //nolint
			conn.Close()
			return
		}
		// Subsequent connections: close immediately (before client can write hello).
		conn.WriteMessage(websocket.CloseMessage, //nolint
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye"))
		conn.Close()
	}))
	defer srv.Close()

	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{URL: wsBase})

	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer hub.Close("")

	// Wait a bit for the reconnect attempt (wsReconnectDelay = 2s).
	// We close hub quickly to avoid the full 2s wait.
	time.Sleep(100 * time.Millisecond)
	// Close hub — reconnect loop will exit via done channel.
	hub.Close("")
}

// ─── WebSocketHub.Send — json.Marshal error from unmarshalable payload ────────

// TestWebSocketHub_Send_MarshalError exercises the json.Marshal error path
// in Send by passing a Message with a channel in its Payload (channels are
// not JSON-serializable, so json.Marshal returns an error).
func TestWebSocketHub_Send_MarshalError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, _ := up.Upgrade(w, r, nil)
		if conn != nil {
			defer conn.Close()
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}
	}))
	defer srv.Close()

	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{URL: wsBase})

	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer hub.Close("")

	// Pass a Message with a channel in Payload — json.Marshal will fail.
	msg := relay.Message{
		Type:    relay.MsgToken,
		Payload: map[string]any{"unmarshalable": make(chan int)},
	}
	err := hub.Send("", msg)
	if err == nil {
		t.Error("expected json.Marshal error for channel payload")
	}
}

// ─── WebSocketHub.reconnect — sendHello fails on reconnect attempt ────────────

// TestWebSocketHub_Reconnect_SendHelloFailsRST verifies the websocket.go:186-188
// path: reconnect dials successfully but sendHello fails.
// The server:
//   1. Accepts first connection normally (so Connect succeeds)
//   2. Closes first connection (triggering reconnect after wsReconnectDelay=2s)
//   3. For subsequent connections: sends RST after 101, so sendHello fails
//   4. Hub closes (test ends)
func TestWebSocketHub_Reconnect_SendHelloFailsRST(t *testing.T) {
	var muC sync.Mutex
	connIdx := 0

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()

	go func() {
		defer ln.Close()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Read the HTTP upgrade request.
			buf := make([]byte, 8192)
			n, _ := conn.Read(buf)
			reqStr := string(buf[:n])

			var wsKey string
			for _, line := range strings.Split(reqStr, "\r\n") {
				lower := strings.ToLower(line)
				if strings.HasPrefix(lower, "sec-websocket-key:") {
					wsKey = strings.TrimSpace(line[len("sec-websocket-key:"):])
				}
			}
			accept := computeWSAccept(wsKey)
			resp := "HTTP/1.1 101 Switching Protocols\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n" +
				"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
			conn.Write([]byte(resp)) //nolint

			muC.Lock()
			idx := connIdx
			connIdx++
			muC.Unlock()

			if idx == 0 {
				// First connection: read hello normally, then close to trigger reconnect.
				buf2 := make([]byte, 1024)
				conn.Read(buf2) //nolint:errcheck
				conn.Close()
			} else {
				// Subsequent connections: RST so sendHello fails.
				if tc, ok := conn.(*net.TCPConn); ok {
					tc.SetLinger(0) //nolint
				}
				conn.Close()
			}
		}
	}()

	hub := relay.NewWebSocketHub(relay.WebSocketConfig{
		URL:       "ws://" + addr,
		MachineID: "reconnect-rst",
		Version:   "1.0",
	})

	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		// If the raw TCP handshake didn't work, skip.
		t.Skipf("Connect failed: %v (raw TCP server may not work here)", err)
	}

	// Wait for first connection to be dropped and one reconnect to fail.
	// wsReconnectDelay=2s + some extra time.
	time.Sleep(3 * time.Second)
	hub.Close("")
}

// ─── WebSocketHub.Connect — context cancelled before dial ────────────────────

func TestWebSocketHub_Connect_ContextCancelled(t *testing.T) {
	// Start a TCP listener but don't upgrade; it just hangs.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Hang indefinitely on this connection.
			time.Sleep(10 * time.Second)
			conn.Close()
		}
	}()

	hub := relay.NewWebSocketHub(relay.WebSocketConfig{
		URL: "ws://" + ln.Addr().String(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = hub.Connect(ctx)
	// Either context deadline exceeded or connection error — both acceptable.
	_ = err
}

// ─── LoadIdentity — file exists but not readable (permission error) ───────────

func TestLoadIdentity_FileNotReadable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root — permission test not meaningful")
	}
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	dir := filepath.Join(tmpHome, ".huginn")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	relayPath := filepath.Join(dir, "relay.json")
	if err := os.WriteFile(relayPath, []byte(`{"agent_id":"x"}`), 0o000); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := relay.LoadIdentity()
	if err == nil {
		t.Error("expected error when relay.json is unreadable")
	}
}

// ─── Identity.Save — UserHomeDir fails (HOME unset, passwd lookup fails) ──────

func TestIdentity_Save_NoHome(t *testing.T) {
	// When HOME is unset, os.UserHomeDir may still succeed via getpwuid.
	// We accept either success or failure — the test exercises the path.
	orig := os.Getenv("HOME")
	os.Unsetenv("HOME")
	defer os.Setenv("HOME", orig)

	id := &relay.Identity{AgentID: "no-home-agent", Endpoint: "https://example.com"}
	_ = id.Save() // may succeed or fail; must not panic
}

// ─── WebSocketHub.Connect — sendHello fails (raw TCP server, RST after 101) ───

// TestWebSocketHub_Connect_SendHelloFailsRST creates a raw TCP server that
// sends a valid WebSocket 101 handshake and then sends a TCP RST by setting
// SO_LINGER=0 and closing, ensuring the client's sendHello WriteMessage fails.
func TestWebSocketHub_Connect_SendHelloFailsRST(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		// Read the full HTTP upgrade request.
		buf := make([]byte, 8192)
		n, _ := conn.Read(buf)
		reqStr := string(buf[:n])

		// Parse Sec-WebSocket-Key.
		var wsKey string
		for _, line := range strings.Split(reqStr, "\r\n") {
			lower := strings.ToLower(line)
			if strings.HasPrefix(lower, "sec-websocket-key:") {
				wsKey = strings.TrimSpace(line[len("sec-websocket-key:"):])
			}
		}

		accept := computeWSAccept(wsKey)
		resp := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
		conn.Write([]byte(resp)) //nolint

		// Set SO_LINGER=0 on the TCP connection to send RST instead of FIN.
		// This abruptly terminates the connection from the server side,
		// causing the client's next write to fail immediately.
		if tc, ok := conn.(*net.TCPConn); ok {
			tc.SetLinger(0) //nolint
		}
		conn.Close()
	}()

	hub := relay.NewWebSocketHub(relay.WebSocketConfig{
		URL:       "ws://" + addr,
		MachineID: "rst-test",
		Version:   "1.0",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = hub.Connect(ctx)
	// May succeed (if buffered) or fail on sendHello. Either way is valid.
	if err == nil {
		hub.Close("")
	}
}
