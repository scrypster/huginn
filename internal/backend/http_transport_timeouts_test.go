package backend

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestAnthropicBackend_Transport_HasConnectionTimeouts verifies that the
// AnthropicBackend HTTP client uses a custom Transport with connection-level
// timeouts, rather than the zero-timeout default transport.
//
// Bug: NewAnthropicBackendWithEndpoint creates &http.Client{Timeout: 0} with
// no Transport override. The default http.Transport has no DialContext timeout,
// no TLSHandshakeTimeout, and no ResponseHeaderTimeout. A server that accepts
// a TCP connection but never sends response headers will hang the client
// goroutine indefinitely.
//
// Fix: supply a *http.Transport with DialContext, TLSHandshakeTimeout, and
// ResponseHeaderTimeout so the connection phase is always bounded, while the
// overall Client.Timeout remains 0 (streaming body can run as long as needed).
func TestAnthropicBackend_Transport_HasConnectionTimeouts(t *testing.T) {
	b := NewAnthropicBackendWithEndpoint(NewKeyResolver("test-key"), "claude-sonnet-4-6", "http://localhost:9999")

	tr, ok := b.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected b.client.Transport to be *http.Transport, got %T", b.client.Transport)
	}
	if tr.TLSHandshakeTimeout == 0 {
		t.Error("TLSHandshakeTimeout must be non-zero to bound TLS negotiation")
	}
	if tr.ResponseHeaderTimeout == 0 {
		t.Error("ResponseHeaderTimeout must be non-zero to detect stalled servers")
	}
	if tr.DialContext == nil {
		t.Error("DialContext must be set with a Dialer timeout to bound TCP connection establishment")
	}
	// Overall client timeout MUST remain 0 — streaming responses are unbounded.
	if b.client.Timeout != 0 {
		t.Errorf("client.Timeout must be 0 (no overall timeout), got %v", b.client.Timeout)
	}
}

// TestExternalBackend_Transport_HasConnectionTimeouts verifies the same
// invariant for ExternalBackend (OpenAI-compatible endpoint).
func TestExternalBackend_Transport_HasConnectionTimeouts(t *testing.T) {
	b := NewExternalBackend("http://localhost:11434")

	tr, ok := b.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected b.client.Transport to be *http.Transport, got %T", b.client.Transport)
	}
	if tr.TLSHandshakeTimeout == 0 {
		t.Error("TLSHandshakeTimeout must be non-zero")
	}
	if tr.ResponseHeaderTimeout == 0 {
		t.Error("ResponseHeaderTimeout must be non-zero")
	}
	if tr.DialContext == nil {
		t.Error("DialContext must be set with a Dialer timeout")
	}
	if b.client.Timeout != 0 {
		t.Errorf("client.Timeout must be 0 (no overall timeout), got %v", b.client.Timeout)
	}
}

// TestExternalBackend_Transport_ResponseHeaderTimeout_SufficientForModelLoad
// asserts that ExternalBackend uses a ResponseHeaderTimeout large enough to
// accommodate local model servers (e.g. Ollama) that load the model before
// sending the first response header. 30 s is too short for large models;
// the minimum acceptable value is 120 s.
func TestExternalBackend_Transport_ResponseHeaderTimeout_SufficientForModelLoad(t *testing.T) {
	b := NewExternalBackend("http://localhost:11434")
	tr, ok := b.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected b.client.Transport to be *http.Transport, got %T", b.client.Transport)
	}
	const minTimeout = 120 * time.Second
	if tr.ResponseHeaderTimeout < minTimeout {
		t.Errorf("ExternalBackend ResponseHeaderTimeout = %v, want >= %v"+
			" (local model servers need time to load before sending the first header)",
			tr.ResponseHeaderTimeout, minTimeout)
	}
}

// TestAnthropicBackend_ResponseHeaderTimeout_TriggersOnStalledServer verifies
// that a server which accepts TCP but never sends response headers causes
// ChatCompletion to return an error rather than hang.
//
// We inject a short ResponseHeaderTimeout directly on the constructed backend's
// Transport to keep the test fast.
func TestAnthropicBackend_ResponseHeaderTimeout_TriggersOnStalledServer(t *testing.T) {
	t.Parallel()

	// Handler accepts the request but blocks until either the client
	// disconnects (r.Context().Done) or a safety timer fires.
	// The short ResponseHeaderTimeout (100ms) we inject means the client will
	// disconnect quickly, cancelling the request context.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			// client disconnected after our ResponseHeaderTimeout
		case <-time.After(500 * time.Millisecond):
			// safety escape so srv.Close() never hangs
		}
	}))
	defer srv.Close()

	b := NewAnthropicBackendWithEndpoint(NewKeyResolver("test-key"), "claude-sonnet-4-6", srv.URL)

	// Inject a very short ResponseHeaderTimeout so the test finishes quickly.
	b.client.Transport = &http.Transport{
		ResponseHeaderTimeout: 100 * time.Millisecond,
	}

	// No context timeout — the ResponseHeaderTimeout must be what saves us.
	_, err := b.ChatCompletion(t.Context(), ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected ChatCompletion to return an error when server stalls, got nil")
	}
	t.Logf("got expected error: %v", err)
}
