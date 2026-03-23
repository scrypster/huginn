package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestSessionCreateRateLimit verifies that POST /api/v1/sessions returns HTTP 429
// after the per-IP limit (10 per minute) is exhausted.
func TestSessionCreateRateLimit(t *testing.T) {
	t.Parallel()

	_, ts := newTestServer(t)

	// The default limit is 10 per minute. Send 10 requests that must succeed,
	// then the 11th should be rate-limited.
	for i := 0; i < 10; i++ {
		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/sessions", nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, resp.StatusCode)
		}
	}

	// The 11th request must be rate-limited.
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 11th request, got %d", resp.StatusCode)
	}
	if ra := resp.Header.Get("Retry-After"); ra == "" {
		t.Error("expected Retry-After header in 429 response")
	}
}

// TestSessionCreateRateLimit_ResetAfterWindow verifies that the rate-limit
// window resets after the window duration expires.
func TestSessionCreateRateLimit_ResetAfterWindow(t *testing.T) {
	t.Parallel()

	srv, ts := newTestServer(t)
	// Replace the default 1-minute window with a very short one so the test
	// doesn't take forever.
	srv.sessionCreateLimiter = newEndpointRateLimiter(2, 50*time.Millisecond)

	// Exhaust the limit.
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/sessions", nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
	}

	// Next request should be rate-limited.
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 before window reset, got %d", resp.StatusCode)
	}

	// Wait for the window to expire.
	time.Sleep(60 * time.Millisecond)

	// After reset the next request should succeed.
	req, _ = http.NewRequest("POST", ts.URL+"/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after window reset, got %d", resp.StatusCode)
	}
}

// TestSpaceCreateRateLimit verifies that POST /api/v1/spaces returns HTTP 429
// after the per-IP limit (20 per minute) is exhausted.
func TestSpaceCreateRateLimit(t *testing.T) {
	t.Parallel()

	srv, ts := newTestServer(t)
	// Override with a tiny limit so we don't need to send 20 real requests.
	srv.spaceCreateLimiter = newEndpointRateLimiter(3, time.Minute)

	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/spaces", nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		// handleCreateSpace may return 500 if spaceStore is nil, which is fine —
		// we only care that it is NOT 429 for the first N requests.
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			t.Fatalf("request %d: unexpectedly rate-limited (expected first %d to pass limiter)", i, 3)
		}
	}

	// 4th request must be rate-limited.
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/spaces", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 4th space-create request, got %d", resp.StatusCode)
	}
}

// TestWorkflowRunRateLimit verifies that POST /api/v1/workflows/{id}/run
// returns HTTP 429 after the per-IP limit is exhausted.
func TestWorkflowRunRateLimit(t *testing.T) {
	t.Parallel()

	srv, ts := newTestServer(t)
	srv.workflowRunLimiter = newEndpointRateLimiter(2, time.Minute)

	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/wf-123/run", nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			t.Fatalf("request %d: unexpectedly rate-limited", i)
		}
	}

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/wf-123/run", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 3rd workflow-run request, got %d", resp.StatusCode)
	}
}

// TestExtractClientIP verifies that extractClientIP handles IPv4, IPv6, and
// missing port correctly.
func TestExtractClientIP(t *testing.T) {
	t.Parallel()

	cases := []struct {
		remoteAddr string
		xff        string
		wantIP     string
	}{
		{"127.0.0.1:12345", "", "127.0.0.1"},
		{"[::1]:54321", "", "::1"},
		{"192.168.1.100:9999", "", "192.168.1.100"},
		// X-Forwarded-For takes priority
		{"127.0.0.1:12345", "10.0.0.1, 10.0.0.2", "10.0.0.1"},
		// Trimmed correctly
		{"127.0.0.1:12345", " 203.0.113.5 , 10.0.0.1", "203.0.113.5"},
	}

	for _, tc := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tc.remoteAddr
		if tc.xff != "" {
			r.Header.Set("X-Forwarded-For", tc.xff)
		}
		got := extractClientIP(r)
		if got != tc.wantIP {
			t.Errorf("remoteAddr=%q xff=%q: extractClientIP()=%q, want %q",
				tc.remoteAddr, tc.xff, got, tc.wantIP)
		}
	}
}

// TestEndpointRateLimiter_Allow verifies basic allow / deny behaviour.
func TestEndpointRateLimiter_Allow(t *testing.T) {
	t.Parallel()

	limiter := newEndpointRateLimiter(3, time.Minute)
	ip := "10.0.0.1"

	for i := 0; i < 3; i++ {
		if !limiter.allow(ip) {
			t.Fatalf("iteration %d: expected allow=true", i)
		}
	}
	if limiter.allow(ip) {
		t.Fatal("expected allow=false after limit exhausted")
	}
}

// TestEndpointRateLimiter_SeparateIPs verifies that different IPs have
// independent windows.
func TestEndpointRateLimiter_SeparateIPs(t *testing.T) {
	t.Parallel()

	limiter := newEndpointRateLimiter(1, time.Minute)

	if !limiter.allow("1.2.3.4") {
		t.Fatal("first request for 1.2.3.4 must be allowed")
	}
	if limiter.allow("1.2.3.4") {
		t.Fatal("second request for 1.2.3.4 must be denied")
	}
	// Different IP should have its own independent bucket.
	if !limiter.allow("5.6.7.8") {
		t.Fatal("first request for 5.6.7.8 must be allowed regardless of 1.2.3.4 state")
	}
}

// TestEndpointRateLimiter_WindowExpiry verifies that entries expire after the window.
func TestEndpointRateLimiter_WindowExpiry(t *testing.T) {
	t.Parallel()

	limiter := newEndpointRateLimiter(1, 30*time.Millisecond)
	ip := "10.0.0.2"

	if !limiter.allow(ip) {
		t.Fatal("first request must be allowed")
	}
	if limiter.allow(ip) {
		t.Fatal("second request must be denied (within window)")
	}
	time.Sleep(40 * time.Millisecond) // wait for window to expire
	if !limiter.allow(ip) {
		t.Fatal("first request in new window must be allowed")
	}
}
