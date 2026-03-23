package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRequestIDMiddleware_AddsHeader verifies that every response includes X-Request-ID.
func TestRequestIDMiddleware_AddsHeader(t *testing.T) {
	handler := requestIDMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	id := rec.Header().Get(requestIDHeader)
	if id == "" {
		t.Error("expected X-Request-ID header to be set")
	}
}

// TestRequestIDMiddleware_UsesIncomingIDIfPresent verifies that an incoming
// X-Request-ID is echoed back.
func TestRequestIDMiddleware_UsesIncomingIDIfPresent(t *testing.T) {
	const clientID = "client-trace-id-abc123"
	handler := requestIDMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(requestIDHeader, clientID)
	rec := httptest.NewRecorder()
	handler(rec, req)

	got := rec.Header().Get(requestIDHeader)
	if got != clientID {
		t.Errorf("expected echoed request ID %q, got %q", clientID, got)
	}
}

// TestRequestIDMiddleware_GeneratesUniqueIDs verifies that each request gets a unique ID.
func TestRequestIDMiddleware_GeneratesUniqueIDs(t *testing.T) {
	handler := requestIDMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ids := make(map[string]bool)
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		handler(rec, req)
		id := rec.Header().Get(requestIDHeader)
		if id == "" {
			t.Fatalf("empty request ID on iteration %d", i)
		}
		if ids[id] {
			t.Errorf("duplicate request ID: %q", id)
		}
		ids[id] = true
	}
}

// TestAuthMiddleware_Returns401JSON verifies that unauthorized requests get
// Content-Type: application/json (not text/plain).
func TestAuthMiddleware_Returns401JSON(t *testing.T) {
	s := &Server{token: "secret"}
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	// Intentionally no Authorization header
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected Content-Type: application/json, got %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"error"`) {
		t.Errorf("expected JSON error body, got %q", body)
	}
}

// TestAuthMiddleware_AllowsValidToken verifies that a valid Bearer token passes.
func TestAuthMiddleware_AllowsValidToken(t *testing.T) {
	s := &Server{token: "my-secret-token"}
	called := false
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.Header.Set("Authorization", "Bearer my-secret-token")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if !called {
		t.Error("expected inner handler to be called")
	}
}

// TestHandleGetConfig_RedactsAPIKey verifies the config endpoint masks secrets.
func TestHandleGetConfig_RedactsAPIKey(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.cfg.Backend.APIKey = "sk-real-secret-key"

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()

	srv.handleGetConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "sk-real-secret-key") {
		t.Error("response must not contain the real API key")
	}
	if !strings.Contains(body, "[REDACTED]") {
		t.Error("expected [REDACTED] placeholder in response")
	}
}

// TestHandleGetConfig_ContentTypeIsJSON verifies the content type header.
func TestHandleGetConfig_ContentTypeIsJSON(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	rec := httptest.NewRecorder()

	srv.handleGetConfig(rec, req)

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected application/json content type, got %q", ct)
	}
}

// TestHandleGetConfig_NoAPIKey_NoRedaction verifies that when APIKey is empty,
// the response does not include [REDACTED].
func TestHandleGetConfig_NoAPIKey_NoRedaction(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.cfg.Backend.APIKey = "" // no key

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	rec := httptest.NewRecorder()

	srv.handleGetConfig(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "[REDACTED]") {
		t.Error("empty APIKey should not produce [REDACTED] in response")
	}
}
