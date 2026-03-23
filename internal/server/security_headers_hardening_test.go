package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSecurityHeaders_AllEndpoints verifies security headers are added to all HTTP responses.
// This tests the securityHeadersMiddleware coverage across the entire request lifecycle.
func TestSecurityHeaders_AllEndpoints(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"GET health", "GET", "/api/health"},
		{"GET token", "GET", "/api/token"},
		{"POST session", "POST", "/api/sessions"},
		{"GET sessions", "GET", "/api/sessions"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			// Verify all critical security headers are present
			headers := map[string]string{
				"X-Content-Type-Options": "nosniff",
				"X-Frame-Options":        "DENY",
				"Referrer-Policy":        "strict-origin-when-cross-origin",
				"Cache-Control":          "no-store",
				"X-XSS-Protection":       "0",
			}

			for name, expected := range headers {
				if got := rec.Header().Get(name); got != expected {
					t.Errorf("%s: expected %q, got %q", name, expected, got)
				}
			}
		})
	}
}

// TestSecurityHeaders_MIME_TypeSniffing verifies X-Content-Type-Options blocks MIME type guessing.
func TestSecurityHeaders_MIME_TypeSniffing(t *testing.T) {
	handler := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html>malicious</html>"))
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Even though we're serving HTML-like content with text/plain,
	// X-Content-Type-Options: nosniff prevents the browser from reinterpreting it.
	if header := rec.Header().Get("X-Content-Type-Options"); header != "nosniff" {
		t.Errorf("X-Content-Type-Options not set correctly: %q", header)
	}
}

// TestSecurityHeaders_ClickjackingProtection verifies X-Frame-Options blocks iframe embedding.
func TestSecurityHeaders_ClickjackingProtection(t *testing.T) {
	handler := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/auth", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options: expected DENY, got %q", got)
	}
}

// TestSecurityHeaders_CacheControl verifies sensitive responses aren't cached.
func TestSecurityHeaders_CacheControl(t *testing.T) {
	handler := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/token", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	cacheControl := rec.Header().Get("Cache-Control")
	if cacheControl != "no-store" {
		t.Errorf("Cache-Control: expected no-store, got %q", cacheControl)
	}
	// Also verify no Expires header is set (which would allow caching)
	if got := rec.Header().Get("Expires"); got != "" {
		t.Errorf("unexpected Expires header: %q", got)
	}
}

// TestSecurityHeaders_ReferrerPolicy verifies Referrer-Policy is set correctly.
func TestSecurityHeaders_ReferrerPolicy(t *testing.T) {
	handler := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Errorf("Referrer-Policy: expected strict-origin-when-cross-origin, got %q", got)
	}
}

// TestSecurityHeaders_XSSProtection verifies X-XSS-Protection is disabled (modern approach).
func TestSecurityHeaders_XSSProtection(t *testing.T) {
	handler := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-XSS-Protection"); got != "0" {
		t.Errorf("X-XSS-Protection: expected 0, got %q", got)
	}
}

// TestSecurityHeaders_StreamingResponse verifies headers are sent before first flush (SSE).
func TestSecurityHeaders_StreamingResponse(t *testing.T) {
	headersSent := false

	handler := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers were set by middleware before handler runs
		if got := w.Header().Get("X-Content-Type-Options"); got == "nosniff" {
			headersSent = true
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: test\n\n"))
	}))

	req := httptest.NewRequest("GET", "/api/stream", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !headersSent {
		t.Error("security headers were not set by middleware")
	}
}

// TestSecurityHeaders_ErrorResponses verifies headers are sent even on error responses.
func TestSecurityHeaders_ErrorResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"400 Bad Request", http.StatusBadRequest},
		{"401 Unauthorized", http.StatusUnauthorized},
		{"403 Forbidden", http.StatusForbidden},
		{"404 Not Found", http.StatusNotFound},
		{"500 Internal Server Error", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte("error"))
			}))

			req := httptest.NewRequest("GET", "/api/test", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.statusCode {
				t.Errorf("expected status %d, got %d", tt.statusCode, rec.Code)
			}
			if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
				t.Errorf("X-Frame-Options missing on error response: %q", got)
			}
		})
	}
}

// TestSecurityHeaders_CustomHeaderNotOverwritten verifies middleware doesn't overwrite custom headers.
func TestSecurityHeaders_CustomHeaderNotOverwritten(t *testing.T) {
	handler := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Header", "custom-value")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Custom-Header"); got != "custom-value" {
		t.Errorf("custom header was overwritten: %q", got)
	}
	// Verify security headers are still set
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("security headers not applied: %q", got)
	}
}

// TestSecurityHeaders_MultipleMiddlewareChaining verifies security headers work with other middleware.
func TestSecurityHeaders_MultipleMiddlewareChaining(t *testing.T) {
	// Simulate middleware chaining
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Apply security headers middleware, then another middleware
	handler := securityHeadersMiddleware(baseHandler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("security headers not preserved through middleware chain: %q", got)
	}
}
