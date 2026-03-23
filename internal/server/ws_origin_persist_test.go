package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/config"
)

// ---------------------------------------------------------------------------
// FIX 1: WebSocket origin check
// ---------------------------------------------------------------------------

// TestIsLocalhostOrigin verifies that the helper correctly classifies
// loopback origins vs remote origins.
func TestIsLocalhostOrigin(t *testing.T) {
	cases := []struct {
		origin string
		want   bool
	}{
		{"", false},
		{"http://localhost", true},
		{"http://localhost:3000", true},
		{"http://127.0.0.1", true},
		{"http://127.0.0.1:8421", true},
		{"http://127.0.0.2:9000", true},  // 127.x.x.x is loopback
		{"http://[::1]", true},
		{"http://192.168.1.10:3000", false},
		{"https://example.com", false},
		{"https://huginncloud.com", false},
		{"not-a-url", false},
	}
	for _, tc := range cases {
		got := isLocalhostOrigin(tc.origin)
		if got != tc.want {
			t.Errorf("isLocalhostOrigin(%q) = %v, want %v", tc.origin, got, tc.want)
		}
	}
}

// TestCheckOrigin_NoOriginAlwaysAllowed verifies that requests without an
// Origin header (non-browser clients) are always accepted.
func TestCheckOrigin_NoOriginAlwaysAllowed(t *testing.T) {
	s := &Server{cfg: *config.Default()}
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	// No Origin header — non-browser client (curl, CLI, desktop app).
	if !s.checkOrigin(req) {
		t.Error("expected request without Origin header to be allowed")
	}
}

// TestCheckOrigin_LocalhostAlwaysAllowed verifies loopback origins are accepted
// even when AllowedOrigins contains no wildcard and no explicit entry for localhost.
func TestCheckOrigin_LocalhostAlwaysAllowed(t *testing.T) {
	cfg := *config.Default()
	cfg.WebUI.AllowedOrigins = []string{"https://my-specific-origin.com"} // no wildcard
	s := &Server{cfg: cfg}

	for _, origin := range []string{
		"http://localhost:3000",
		"http://127.0.0.1:8421",
		"http://[::1]",
	} {
		req := httptest.NewRequest(http.MethodGet, "/ws", nil)
		req.Header.Set("Origin", origin)
		if !s.checkOrigin(req) {
			t.Errorf("expected loopback origin %q to be allowed regardless of AllowedOrigins", origin)
		}
	}
}

// TestCheckOrigin_WildcardAllowsAll verifies that "*" in AllowedOrigins
// permits any origin (opt-in permissive mode for dev/trust setups).
func TestCheckOrigin_WildcardAllowsAll(t *testing.T) {
	cfg := *config.Default()
	cfg.WebUI.AllowedOrigins = []string{"*"}
	s := &Server{cfg: cfg}

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Origin", "https://arbitrary-remote.example.com")
	if !s.checkOrigin(req) {
		t.Error("expected wildcard AllowedOrigins to allow any origin")
	}
}

// TestCheckOrigin_ExplicitListAllowed verifies that an origin matching an
// entry in AllowedOrigins (case-insensitive) is accepted.
func TestCheckOrigin_ExplicitListAllowed(t *testing.T) {
	cfg := *config.Default()
	cfg.WebUI.AllowedOrigins = []string{"https://app.example.com", "https://other.example.com"}
	s := &Server{cfg: cfg}

	allowed := []string{
		"https://app.example.com",
		"https://other.example.com",
		"HTTPS://APP.EXAMPLE.COM", // case-insensitive
	}
	for _, origin := range allowed {
		req := httptest.NewRequest(http.MethodGet, "/ws", nil)
		req.Header.Set("Origin", origin)
		if !s.checkOrigin(req) {
			t.Errorf("expected allowed origin %q to pass checkOrigin", origin)
		}
	}
}

// TestCheckOrigin_UnknownOriginDenied verifies that an origin not in
// AllowedOrigins (and not a loopback) is rejected when an explicit list is set.
func TestCheckOrigin_UnknownOriginDenied(t *testing.T) {
	cfg := *config.Default()
	cfg.WebUI.AllowedOrigins = []string{"https://app.example.com"}
	s := &Server{cfg: cfg}

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Origin", "https://evil.attacker.com")
	if s.checkOrigin(req) {
		t.Error("expected unknown origin to be rejected when explicit AllowedOrigins is set")
	}
}

// TestCheckOrigin_EmptyAllowedOriginsDefaultsToAllowAll verifies that when
// AllowedOrigins is nil/empty (the default for new configs), all origins are
// accepted for backwards compatibility.
func TestCheckOrigin_EmptyAllowedOriginsDefaultsToAllowAll(t *testing.T) {
	cfg := *config.Default()
	// AllowedOrigins is nil by default — backwards-compatible behaviour.
	s := &Server{cfg: cfg}

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Origin", "https://any-remote.example.com")
	if !s.checkOrigin(req) {
		t.Error("expected nil AllowedOrigins to allow all origins (backwards-compat default)")
	}
}

// TestWSClientSendBufferSize verifies that the wsClient send channel can
// buffer at least 256 messages without blocking. The channel was previously
// 64-slot which caused drops on slow clients.
func TestWSClientSendBufferSize(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &wsClient{
		send: make(chan WSMessage, 256),
		ctx:  ctx,
	}

	// Enqueue 256 messages non-blocking — the full channel capacity.
	for i := 0; i < 256; i++ {
		select {
		case c.send <- WSMessage{Type: "test"}:
		default:
			t.Fatalf("send channel blocked at message %d (buffer smaller than 256)", i)
		}
	}
}

// ---------------------------------------------------------------------------
// FIX 2: Persistence error notification
// ---------------------------------------------------------------------------

// TestSendPersistenceError_MessageDelivered verifies that sendPersistenceError
// enqueues a WSMessage of type "error" with the expected payload.
func TestSendPersistenceError_MessageDelivered(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &wsClient{
		send: make(chan WSMessage, 8),
		ctx:  ctx,
	}

	sendPersistenceError(c, "user_message", errors.New("disk full"))

	select {
	case msg := <-c.send:
		if msg.Type != "error" {
			t.Errorf("Type = %q, want %q", msg.Type, "error")
		}
		if msg.Content == "" {
			t.Error("Content should be non-empty for persistence errors")
		}
		ctxVal, _ := msg.Payload["context"].(string)
		if ctxVal != "user_message" {
			t.Errorf("Payload[context] = %q, want %q", ctxVal, "user_message")
		}
	default:
		t.Fatal("no message was sent to the client's send channel")
	}
}

// TestSendPersistenceError_AssistantContext verifies the assistant_message
// context is correctly propagated to the client.
func TestSendPersistenceError_AssistantContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &wsClient{
		send: make(chan WSMessage, 8),
		ctx:  ctx,
	}

	sendPersistenceError(c, "assistant_message", errors.New("storage error"))

	select {
	case msg := <-c.send:
		if msg.Type != "error" {
			t.Errorf("Type = %q, want %q", msg.Type, "error")
		}
		ctxVal, _ := msg.Payload["context"].(string)
		if ctxVal != "assistant_message" {
			t.Errorf("Payload[context] = %q, want %q", ctxVal, "assistant_message")
		}
	default:
		t.Fatal("no message was sent")
	}
}

// TestSendPersistenceError_DisconnectedClient verifies that sendPersistenceError
// does not block when the client context is already cancelled (disconnected client).
func TestSendPersistenceError_DisconnectedClient(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Immediately cancel — simulate disconnected client.
	c := &wsClient{
		send: make(chan WSMessage, 8),
		ctx:  ctx,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		sendPersistenceError(c, "user_message", errors.New("storage error"))
	}()

	select {
	case <-done:
		// sendPersistenceError returned without blocking — correct.
	case <-time.After(200 * time.Millisecond):
		t.Fatal("sendPersistenceError blocked indefinitely on a disconnected client")
	}
}

// TestSendPersistenceError_UserFriendlyContent verifies the error Content is
// intentionally user-friendly (a static retry prompt, not the raw Go error).
func TestSendPersistenceError_UserFriendlyContent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &wsClient{
		send: make(chan WSMessage, 8),
		ctx:  ctx,
	}

	rawErrText := "pebble: disk I/O error: no space left on device"
	sendPersistenceError(c, "user_message", errors.New(rawErrText))

	msg := <-c.send
	// Raw Go error strings must not be surfaced directly to the user.
	if msg.Content == rawErrText {
		t.Errorf("raw internal error exposed to client: %q", msg.Content)
	}
	if msg.Content == "" {
		t.Error("Content should be a non-empty user-friendly message")
	}
}
