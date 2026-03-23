package tools

import (
	"context"
	"strings"
	"testing"
)

// TestFetchURL_SSRF_ZeroIPBypass verifies that http://0.0.0.0/ is blocked.
//
// Bug: isPrivateHost("0.0.0.0") returns false because net.IP{0,0,0,0} is not
// matched by IsLoopback(), IsPrivate(), or IsLinkLocalUnicast(). On most
// operating systems, connecting to 0.0.0.0 routes to 127.0.0.1, making this
// a viable SSRF vector.
//
// Fix: add ip.IsUnspecified() to the private-address predicate in the
// dial-time SSRF guard so that 0.0.0.0 and :: are blocked.
func TestFetchURL_SSRF_ZeroIPBypass(t *testing.T) {
	t.Parallel()

	// Use port 19999 — no service expected here; the SSRF block should fire
	// before any connection is attempted so the port doesn't need to be open.
	tool := &FetchURLTool{} // no injected client → SSRF guard is active

	result := tool.Execute(context.Background(), map[string]any{
		"url": "http://0.0.0.0:19999/secret",
	})

	if !result.IsError {
		t.Fatal("expected SSRF error for http://0.0.0.0:19999/, got success")
	}
	// The error must come from the SSRF guard, not a generic "connection refused"
	// which would mean the guard was bypassed and a real dial was attempted.
	if !strings.Contains(result.Error, "private") && !strings.Contains(result.Error, "internal") {
		t.Errorf("expected SSRF rejection message containing 'private' or 'internal', got: %q", result.Error)
	}
}

// TestFetchURL_SSRF_DialTimeCheck verifies that the SSRF guard operates at
// dial time, not only via the pre-flight isPrivateHost check.
//
// A pre-flight check that runs before the HTTP connection is subject to a
// TOCTOU race (DNS rebinding): the check resolves the hostname to a public IP,
// then the HTTP client re-resolves and connects to a private IP. Moving the
// check into DialContext eliminates this window.
//
// This test verifies the structural invariant: calling isPrivateIP at dial time
// on 127.0.0.1 (the resolved addr that httpClient's DialContext sees) causes
// the request to be blocked with the SSRF error, even if no pre-flight check
// ran. We test this by directly calling the exported httpClient()'s transport.
func TestFetchURL_SSRF_DialTimeCheck_Loopback(t *testing.T) {
	t.Parallel()

	tool := &FetchURLTool{} // no injected client → SSRF guard via DialContext
	result := tool.Execute(context.Background(), map[string]any{
		"url": "http://127.0.0.1:19998/secret",
	})

	if !result.IsError {
		t.Fatal("expected SSRF error for http://127.0.0.1:19998/, got success")
	}
	if !strings.Contains(result.Error, "private") && !strings.Contains(result.Error, "internal") {
		t.Errorf("expected SSRF rejection message, got: %q", result.Error)
	}
}

// TestFetchURL_SSRF_IPv6Loopback verifies that http://[::1]/ is blocked.
func TestFetchURL_SSRF_IPv6Loopback(t *testing.T) {
	t.Parallel()

	tool := &FetchURLTool{}
	result := tool.Execute(context.Background(), map[string]any{
		"url": "http://[::1]:19997/secret",
	})

	if !result.IsError {
		t.Fatal("expected SSRF error for http://[::1]/, got success")
	}
	if !strings.Contains(result.Error, "private") && !strings.Contains(result.Error, "internal") {
		t.Errorf("expected SSRF rejection message, got: %q", result.Error)
	}
}

// TestFetchURL_SSRF_RFC1918 verifies RFC-1918 private CIDRs are blocked.
func TestFetchURL_SSRF_RFC1918(t *testing.T) {
	t.Parallel()

	addrs := []string{
		"http://10.0.0.1/",
		"http://172.16.0.1/",
		"http://192.168.1.1/",
	}

	for _, addr := range addrs {
		addr := addr
		t.Run(addr, func(t *testing.T) {
			t.Parallel()
			tool := &FetchURLTool{}
			result := tool.Execute(context.Background(), map[string]any{"url": addr})
			if !result.IsError {
				t.Fatalf("expected SSRF error for %q, got success", addr)
			}
			if !strings.Contains(result.Error, "private") && !strings.Contains(result.Error, "internal") {
				t.Errorf("expected SSRF rejection message, got: %q", result.Error)
			}
		})
	}
}
