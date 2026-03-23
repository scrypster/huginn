package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// FIX 3: Auth-failure rate limiter
// ---------------------------------------------------------------------------

// TestExtractClientIP_XForwardedFor verifies that X-Forwarded-For is preferred
// over RemoteAddr for IP extraction.
func TestExtractClientIP_XForwardedFor(t *testing.T) {
	cases := []struct {
		xff      string
		remote   string
		wantIP   string
	}{
		{xff: "203.0.113.1", remote: "10.0.0.1:1234", wantIP: "203.0.113.1"},
		{xff: "203.0.113.1, 10.0.0.2", remote: "10.0.0.1:1234", wantIP: "203.0.113.1"},
		{xff: "  203.0.113.1  ", remote: "10.0.0.1:1234", wantIP: "203.0.113.1"},
		{xff: "", remote: "10.0.0.2:5678", wantIP: "10.0.0.2"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = tc.remote
		if tc.xff != "" {
			req.Header.Set("X-Forwarded-For", tc.xff)
		}
		got := extractClientIP(req)
		if got != tc.wantIP {
			t.Errorf("extractClientIP(xff=%q, remote=%q) = %q, want %q",
				tc.xff, tc.remote, got, tc.wantIP)
		}
	}
}

// TestExtractClientIP_RemoteAddrFallback verifies that RemoteAddr (host only,
// no port) is used when X-Forwarded-For is absent.
func TestExtractClientIP_RemoteAddrFallback(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.50:9876"
	got := extractClientIP(req)
	if got != "192.168.1.50" {
		t.Errorf("extractClientIP = %q, want %q", got, "192.168.1.50")
	}
}

// TestAuthFailLimiter_AllowsUnderLimit verifies that attempts below the limit
// are always allowed.
func TestAuthFailLimiter_AllowsUnderLimit(t *testing.T) {
	lim := newAuthFailLimiterWithClock(time.Now)
	for i := 0; i < authFailMaxPerMinute; i++ {
		if over := lim.recordFailure("1.2.3.4"); over {
			t.Fatalf("recordFailure returned over-limit at attempt %d (expected under limit)", i+1)
		}
	}
}

// TestAuthFailLimiter_BlocksAfterLimit verifies that the (limit+1)th failure
// causes recordFailure to return true (over limit).
func TestAuthFailLimiter_BlocksAfterLimit(t *testing.T) {
	lim := newAuthFailLimiterWithClock(time.Now)
	for i := 0; i < authFailMaxPerMinute; i++ {
		lim.recordFailure("1.2.3.4")
	}
	// The next (11th) failure should flip over-limit.
	over := lim.recordFailure("1.2.3.4")
	if !over {
		t.Error("expected over-limit=true after exceeding authFailMaxPerMinute failures")
	}
}

// TestAuthFailLimiter_IsBlockedAfterLimit verifies that isBlocked returns true
// after the limit is exceeded.
func TestAuthFailLimiter_IsBlockedAfterLimit(t *testing.T) {
	lim := newAuthFailLimiterWithClock(time.Now)
	for i := 0; i <= authFailMaxPerMinute; i++ {
		lim.recordFailure("5.5.5.5")
	}
	if !lim.isBlocked("5.5.5.5") {
		t.Error("expected isBlocked=true after exceeding limit")
	}
}

// TestAuthFailLimiter_SlidingWindowExpiry verifies that failures older than
// the window are evicted and the IP is unblocked.
func TestAuthFailLimiter_SlidingWindowExpiry(t *testing.T) {
	// Use an injectable clock so we can advance time without sleeping.
	now := time.Now()
	lim := newAuthFailLimiterWithClock(func() time.Time { return now })

	// Exhaust the limit.
	for i := 0; i <= authFailMaxPerMinute; i++ {
		lim.recordFailure("9.9.9.9")
	}
	if !lim.isBlocked("9.9.9.9") {
		t.Fatal("should be blocked before window expires")
	}

	// Advance time past the sliding window — all entries should expire.
	now = now.Add(authFailWindow + time.Second)

	// isBlocked checks with the current clock, so expired entries are not counted.
	if lim.isBlocked("9.9.9.9") {
		t.Error("should be unblocked after sliding window expired")
	}
}

// TestAuthFailLimiter_PerIPIsolation verifies that rate limiting is tracked
// per IP address independently.
func TestAuthFailLimiter_PerIPIsolation(t *testing.T) {
	lim := newAuthFailLimiterWithClock(time.Now)

	// Exhaust the limit for IP A.
	for i := 0; i <= authFailMaxPerMinute; i++ {
		lim.recordFailure("10.0.0.1")
	}
	if !lim.isBlocked("10.0.0.1") {
		t.Error("10.0.0.1 should be blocked")
	}

	// IP B should be completely unaffected.
	if lim.isBlocked("10.0.0.2") {
		t.Error("10.0.0.2 should not be blocked (different IP)")
	}
}

// TestAuthMiddleware_RateLimitReturns429 verifies that once an IP exceeds
// the failure threshold, subsequent requests receive HTTP 429 with Retry-After.
func TestAuthMiddleware_RateLimitReturns429(t *testing.T) {
	s := &Server{token: "correct-token"}
	// Pre-set the limiter with injectable clock so tests don't interfere.
	s.authLimiter = newAuthFailLimiterWithClock(time.Now)

	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Generate enough failures to exceed the limit.
	for i := 0; i <= authFailMaxPerMinute; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.Header.Set("X-Forwarded-For", "192.0.2.99")
		rec := httptest.NewRecorder()
		handler(rec, req)
	}

	// The next request from the same IP should be rate-limited.
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Forwarded-For", "192.0.2.99")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 after rate limit exceeded, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header on 429 response")
	}
}

// TestAuthMiddleware_ValidTokenBypassesRateLimit verifies that a valid auth
// token always succeeds regardless of previous failures from the same IP.
func TestAuthMiddleware_ValidTokenBypassesRateLimit(t *testing.T) {
	s := &Server{token: "correct-token"}
	s.authLimiter = newAuthFailLimiterWithClock(time.Now)

	called := false
	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	// Generate failures up to (but NOT reaching) the block threshold.
	// isBlocked returns true when count >= authFailMaxPerMinute, so we
	// must stay strictly below that count.
	for i := 0; i < authFailMaxPerMinute-1; i++ {
		req := httptest.NewRequest("GET", "/api/test?token=wrong", nil)
		req.Header.Set("X-Forwarded-For", "192.0.2.88")
		handler(httptest.NewRecorder(), req)
	}

	// A correct token from the same IP should still succeed.
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer correct-token")
	req.Header.Set("X-Forwarded-For", "192.0.2.88")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if !called {
		t.Error("valid token should succeed even from an IP with prior failures")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for valid token, got %d", rec.Code)
	}
}

// TestAuthMiddleware_FirstFailureStillReturns401 verifies that the first
// invalid auth attempt returns 401, not 429.
func TestAuthMiddleware_FirstFailureStillReturns401(t *testing.T) {
	s := &Server{token: "correct"}
	s.authLimiter = newAuthFailLimiterWithClock(time.Now)

	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {})

	req := httptest.NewRequest("GET", "/api/test?token=wrong", nil)
	req.Header.Set("X-Forwarded-For", "192.0.2.77")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for first invalid token, got %d", rec.Code)
	}
}

// TestAuthMiddleware_RetryAfterHeaderValue verifies the Retry-After header
// value is set to "60" (one minute) when returning 429.
func TestAuthMiddleware_RetryAfterHeaderValue(t *testing.T) {
	s := &Server{token: "tok"}
	s.authLimiter = newAuthFailLimiterWithClock(time.Now)

	handler := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {})

	for i := 0; i <= authFailMaxPerMinute; i++ {
		req := httptest.NewRequest("GET", "/api/test?token=wrong", nil)
		req.Header.Set("X-Forwarded-For", "192.0.2.55")
		handler(httptest.NewRecorder(), req)
	}

	req := httptest.NewRequest("GET", "/api/test?token=wrong", nil)
	req.Header.Set("X-Forwarded-For", "192.0.2.55")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After = %q, want %q", got, "60")
	}
}
