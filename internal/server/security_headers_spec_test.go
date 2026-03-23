package server

// security_headers_spec_test.go — Behavior specs for securityHeadersMiddleware.
//
// These verify the actual header values and that they are present on every
// response regardless of status code or handler behavior.  Each test maps
// directly to a security property, not just "header exists".

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// noopHandler is the inner handler wrapped by the middleware under test.
var noopHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestSecurityHeaders_XContentTypeOptions_IsNosniff(t *testing.T) {
	rr := httptest.NewRecorder()
	securityHeadersMiddleware(noopHandler).ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	got := rr.Header().Get("X-Content-Type-Options")
	if got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
}

func TestSecurityHeaders_XFrameOptions_IsDeny(t *testing.T) {
	rr := httptest.NewRecorder()
	securityHeadersMiddleware(noopHandler).ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	got := rr.Header().Get("X-Frame-Options")
	if got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want %q", got, "DENY")
	}
}

func TestSecurityHeaders_ReferrerPolicy_IsStrictOrigin(t *testing.T) {
	rr := httptest.NewRecorder()
	securityHeadersMiddleware(noopHandler).ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	got := rr.Header().Get("Referrer-Policy")
	if got != "strict-origin-when-cross-origin" {
		t.Errorf("Referrer-Policy = %q, want %q", got, "strict-origin-when-cross-origin")
	}
}

func TestSecurityHeaders_CacheControl_IsNoStore(t *testing.T) {
	rr := httptest.NewRecorder()
	securityHeadersMiddleware(noopHandler).ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	got := rr.Header().Get("Cache-Control")
	if got != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-store")
	}
}

func TestSecurityHeaders_XSSProtection_IsDisabled(t *testing.T) {
	// Modern guidance: set to "0" to disable the legacy XSS auditor which can
	// itself be exploited.  Confirm the value is exactly "0".
	rr := httptest.NewRecorder()
	securityHeadersMiddleware(noopHandler).ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	got := rr.Header().Get("X-XSS-Protection")
	if got != "0" {
		t.Errorf("X-XSS-Protection = %q, want %q", got, "0")
	}
}

// TestSecurityHeaders_PresentOnErrorResponse verifies headers are added even
// when the inner handler writes a non-200 status code.  Middleware must not
// short-circuit before calling next.
func TestSecurityHeaders_PresentOnErrorResponse(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	rr := httptest.NewRecorder()
	securityHeadersMiddleware(inner).ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))

	headers := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Cache-Control":          "no-store",
	}
	for name, want := range headers {
		if got := rr.Header().Get(name); got != want {
			t.Errorf("[500 response] %s = %q, want %q", name, got, want)
		}
	}
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("inner handler status overwritten: got %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}

// TestSecurityHeaders_InnerHandlerIsInvoked verifies the middleware always
// delegates to the next handler (not a short-circuit guard).
func TestSecurityHeaders_InnerHandlerIsInvoked(t *testing.T) {
	invoked := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		invoked = true
		w.WriteHeader(http.StatusOK)
	})
	rr := httptest.NewRecorder()
	securityHeadersMiddleware(inner).ServeHTTP(rr, httptest.NewRequest("GET", "/test", nil))
	if !invoked {
		t.Error("securityHeadersMiddleware did not invoke the inner handler")
	}
}

// TestSecurityHeaders_AllFivePresentOnGETRequest is a single-request
// completeness check — easier to scan in CI output than five separate tests.
func TestSecurityHeaders_AllFivePresentOnGETRequest(t *testing.T) {
	rr := httptest.NewRecorder()
	securityHeadersMiddleware(noopHandler).ServeHTTP(rr, httptest.NewRequest("GET", "/api/anything", nil))

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
		"Cache-Control":          "no-store",
		"X-XSS-Protection":       "0",
	}
	for h, v := range want {
		if got := rr.Header().Get(h); got != v {
			t.Errorf("%s = %q, want %q", h, got, v)
		}
	}
}
