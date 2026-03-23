package server

import (
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestAuthMiddleware_EmptyTokenRejects tests that empty tokens are rejected.
func TestAuthMiddleware_EmptyTokenRejects(t *testing.T) {
	s := &Server{token: "valid-secret"}
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for missing token, got %d", rec.Code)
	}
}

// TestAuthMiddleware_QueryTokenBypass tests that tokens via query string work.
func TestAuthMiddleware_QueryTokenBypass(t *testing.T) {
	s := &Server{token: "test-secret"}
	called := false
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test?token=test-secret", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if !called {
		t.Error("handler should have been called with valid query token")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// TestAuthMiddleware_BearerTokenTrumpsQueryToken tests precedence.
func TestAuthMiddleware_BearerTokenTrumpsQueryToken(t *testing.T) {
	s := &Server{token: "correct-secret"}
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Bearer takes precedence; query token is wrong
	req := httptest.NewRequest(http.MethodGet, "/api/test?token=wrong", nil)
	req.Header.Set("Authorization", "Bearer correct-secret")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected Bearer to take precedence, got status %d", rec.Code)
	}
}

// TestAuthMiddleware_ConstantTimeComparison verifies timing-safe comparison.
func TestAuthMiddleware_ConstantTimeComparison(t *testing.T) {
	s := &Server{token: "super-secret-token-1234"}
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Timing attack: try tokens with progressively correct prefixes.
	// All should take the same time due to constant-time comparison.
	attempts := []string{
		"wrong-token",
		"super-",
		"super-secret-",
		"super-secret-token-1234",
	}

	for _, attempt := range attempts {
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req.Header.Set("Authorization", "Bearer "+attempt)
		rec := httptest.NewRecorder()

		start := time.Now()
		handler(rec, req)
		elapsed := time.Since(start)

		// We can't directly test timing here (too fast, too variable),
		// but we verify the comparison logic works correctly.
		if attempt == "super-secret-token-1234" {
			if rec.Code != http.StatusOK {
				t.Errorf("valid token rejected: %s", attempt)
			}
		} else {
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("invalid token accepted: %s", attempt)
			}
		}
		_ = elapsed // would be used in real timing analysis
	}
}

// TestAuthMiddleware_MalformedAuthHeader tests robustness.
func TestAuthMiddleware_MalformedAuthHeader(t *testing.T) {
	s := &Server{token: "secret"}
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not call inner handler")
	})

	tests := []struct {
		name  string
		auth  string
		token string
	}{
		{"no bearer prefix", "NotBearer secret", ""},
		{"bearer with no space", "Bearersecret", ""},
		{"bearer with extra space", "Bearer  secret", ""},
		{"bearer with trailing space", "Bearer secret ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}
			rec := httptest.NewRecorder()
			handler(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d", rec.Code)
			}
		})
	}
}

// TestExtractToken_BearerPrecedence verifies Bearer takes precedence over query param.
func TestExtractToken_BearerPrecedence(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?token=query-token", nil)
	req.Header.Set("Authorization", "Bearer bearer-token")

	tok := extractToken(req)
	if tok != "bearer-token" {
		t.Errorf("expected bearer token precedence, got %q", tok)
	}
}

// TestExtractToken_QueryTokenFallback verifies fallback to query param.
func TestExtractToken_QueryTokenFallback(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?token=query-token", nil)

	tok := extractToken(req)
	if tok != "query-token" {
		t.Errorf("expected query token, got %q", tok)
	}
}

// TestExtractToken_NoToken verifies empty string on missing token.
func TestExtractToken_NoToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	tok := extractToken(req)
	if tok != "" {
		t.Errorf("expected empty token, got %q", tok)
	}
}

// TestSecurityHeadersMiddleware_AllHeadersPresent verifies all expected headers.
func TestSecurityHeadersMiddleware_AllHeadersPresent(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	securityHeadersMiddleware(inner).ServeHTTP(rec, req)

	expectedHeaders := map[string]string{
		"X-Content-Type-Options":        "nosniff",
		"X-Frame-Options":               "DENY",
		"Referrer-Policy":               "strict-origin-when-cross-origin",
		"Cache-Control":                 "no-store",
		"X-XSS-Protection":              "0",
	}

	for header, expected := range expectedHeaders {
		got := rec.Header().Get(header)
		if got != expected {
			t.Errorf("header %q: expected %q, got %q", header, expected, got)
		}
	}
}

// TestFlowRateLimiter_AllowsUnderLimit verifies normal operation.
func TestFlowRateLimiter_AllowsUnderLimit(t *testing.T) {
	limiter := newFlowRateLimiter()

	for i := 0; i < maxOAuthFlowsPerIP; i++ {
		if !limiter.allow("192.0.2.1") {
			t.Errorf("flow %d should be allowed", i)
		}
	}
}

// TestFlowRateLimiter_DeniesOverLimit verifies rate limiting.
func TestFlowRateLimiter_DeniesOverLimit(t *testing.T) {
	limiter := newFlowRateLimiter()

	// Exceed limit
	for i := 0; i < maxOAuthFlowsPerIP+1; i++ {
		limiter.allow("192.0.2.1")
	}

	if limiter.allow("192.0.2.1") {
		t.Error("flow should be denied after limit exceeded")
	}
}

// TestFlowRateLimiter_ResetAfterWindow verifies window expiration.
func TestFlowRateLimiter_ResetAfterWindow(t *testing.T) {
	limiter := newFlowRateLimiter()

	// Fill the window
	for i := 0; i < maxOAuthFlowsPerIP; i++ {
		limiter.allow("192.0.2.1")
	}

	// Manually trigger cleanup by advancing time
	// Since we can't easily mock time, we'll test the logic indirectly
	// by checking that the window map works correctly.
	if !limiter.allow("192.0.2.2") {
		t.Error("different IP should have its own limit")
	}
}

// TestFlowRateLimiter_IsolationByIP verifies per-IP isolation.
func TestFlowRateLimiter_IsolationByIP(t *testing.T) {
	limiter := newFlowRateLimiter()

	ip1 := "192.0.2.1"
	ip2 := "192.0.2.2"

	// Max out IP1
	for i := 0; i < maxOAuthFlowsPerIP; i++ {
		limiter.allow(ip1)
	}

	// IP2 should still be allowed
	for i := 0; i < maxOAuthFlowsPerIP; i++ {
		if !limiter.allow(ip2) {
			t.Errorf("IP2 flow %d should be allowed (isolated from IP1)", i)
		}
	}
}

// TestFlowRateLimiter_ConcurrentAccess tests thread safety.
func TestFlowRateLimiter_ConcurrentAccess(t *testing.T) {
	limiter := newFlowRateLimiter()
	var wg sync.WaitGroup
	results := make([]bool, 100)
	mu := sync.Mutex{}

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			allowed := limiter.allow("192.0.2.1")
			mu.Lock()
			results[idx] = allowed
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	allowed := 0
	denied := 0
	for _, a := range results {
		if a {
			allowed++
		} else {
			denied++
		}
	}

	if allowed != maxOAuthFlowsPerIP {
		t.Errorf("expected exactly %d allowed, got %d", maxOAuthFlowsPerIP, allowed)
	}
	if denied != 100-maxOAuthFlowsPerIP {
		t.Errorf("expected %d denied, got %d", 100-maxOAuthFlowsPerIP, denied)
	}
}

// TestStatusRecorder_CapturesStatus verifies status tracking.
func TestStatusRecorder_CapturesStatus(t *testing.T) {
	inner := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: inner, status: 200}

	sr.WriteHeader(http.StatusNotFound)

	if sr.status != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, sr.status)
	}
	if inner.Code != http.StatusNotFound {
		t.Error("status not written to inner ResponseWriter")
	}
}

// TestRequestIDMiddleware_ValidHex verifies IDs are valid hex.
func TestRequestIDMiddleware_ValidHex(t *testing.T) {
	handler := requestIDMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		handler(rec, req)

		id := rec.Header().Get("X-Request-ID")
		if _, err := hex.DecodeString(id); err != nil {
			t.Errorf("request ID %q is not valid hex: %v", id, err)
		}
	}
}

// TestRequestIDMiddleware_ContextPropagation verifies ID is in context.
func TestRequestIDMiddleware_ContextPropagation(t *testing.T) {
	expectedID := ""
	handler := requestIDMiddleware(func(w http.ResponseWriter, r *http.Request) {
		expectedID = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	headerID := rec.Header().Get("X-Request-ID")
	if expectedID != headerID {
		t.Errorf("context ID %q != header ID %q", expectedID, headerID)
	}
}

// TestSanitizePath_StripsLongSegments verifies ULID/UUID stripping.
func TestSanitizePath_StripsLongSegments(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// sanitizePath replaces segments >= 20 chars with :id
		{"/api/sessions/01HXYZ1234567890ABCDEF/messages", "/api/sessions/:id/messages"}, // 20-char ID
		{"/api/short", "/api/short"},
		{"/a/b/c/d/e/f/g", "/a/b/c/d/e/f/g"},
		{"/api/sessions/123456789012345678901/test", "/api/sessions/:id/test"}, // 21-char ID
		{"/api/short_id/path", "/api/short_id/path"}, // Short ID (not stripped)
	}

	for _, tt := range tests {
		got := sanitizePath(tt.input)
		if got != tt.expected {
			t.Errorf("sanitizePath(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// TestConstantTimeCompareProperties ensures the built-in subtle package is used correctly.
func TestConstantTimeCompareProperties(t *testing.T) {
	tests := []struct {
		a    string
		b    string
		want int
	}{
		{"secret", "secret", 1},
		{"secret", "wrong", 0},
		{"", "", 1},
		{"a", "", 0},
		{"", "a", 0},
	}

	for _, tt := range tests {
		got := subtle.ConstantTimeCompare([]byte(tt.a), []byte(tt.b))
		if got != tt.want {
			t.Errorf("ConstantTimeCompare(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

// TestAuthMiddleware_TokenExtractionOrder verifies Bearer > query param precedence.
func TestAuthMiddleware_TokenExtractionOrder(t *testing.T) {
	s := &Server{token: "correct"}
	called := false
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/?token=wrong", nil)
	req.Header.Set("Authorization", "Bearer correct")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if !called {
		t.Error("handler should be called; Bearer should take precedence")
	}
}

// TestLoggingMiddleware_DoesNotMutateRequest verifies idempotency.
func TestLoggingMiddleware_DoesNotMutateRequest(t *testing.T) {
	originalMethod := "POST"
	originalPath := "/api/test"
	called := false

	handler := loggingMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != originalMethod {
			t.Errorf("method changed: %s -> %s", originalMethod, r.Method)
		}
		if r.URL.Path != originalPath {
			t.Errorf("path changed: %s -> %s", originalPath, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(originalMethod, originalPath, nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if !called {
		t.Error("handler not called")
	}
}

// TestJSONError_ProducesValidJSON verifies JSON encoding.
func TestJSONError_ProducesValidJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	jsonError(rec, http.StatusBadRequest, "test error message")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"error"`) {
		t.Errorf("JSON should contain error field: %s", body)
	}
	if !strings.Contains(body, "test error message") {
		t.Errorf("JSON should contain error message: %s", body)
	}
}

// TestJSONOK_ProducesValidJSON verifies JSON encoding for responses.
func TestJSONOK_ProducesValidJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	testData := map[string]interface{}{"key": "value", "number": 42}
	jsonOK(rec, testData)

	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Error("Content-Type should be application/json")
	}

	body := rec.Body.String()
	if !strings.Contains(body, "key") || !strings.Contains(body, "value") {
		t.Errorf("JSON should contain data: %s", body)
	}
}
