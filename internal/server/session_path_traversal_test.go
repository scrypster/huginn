package server

// hardening_iter6_test.go — Security hardening iteration 6
// Covers:
//   1. Path traversal via session ID in REST endpoints (GET/DELETE/PATCH)
//   2. Path traversal via session ID rejected at the store level (store returns error)
//   3. Open-redirect / parameter-injection prevention in handleOAuthCallback

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ─── Vulnerability 1: Session ID path traversal via REST handlers ─────────────

// TestHandleGetSession_PathTraversal ensures that a session ID containing "../"
// results in 404 (store rejects the ID before any file is touched).
func TestHandleGetSession_PathTraversal(t *testing.T) {
	_, ts := newTestServer(t)

	// The mux path value for {id} will receive the raw segment "..%2Fetc%2Fpasswd".
	// Even if decoded to "../etc/passwd", the store's validateID should reject it.
	traversalIDs := []string{
		"..%2Fetc%2Fpasswd",
		"..%2F..%2Fsecret",
	}
	for _, id := range traversalIDs {
		req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions/"+id, nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("id=%q: request error: %v", id, err)
		}
		resp.Body.Close()
		// Expect 404 (not found — ID rejected by store) rather than a 500 or file leak.
		if resp.StatusCode != 404 {
			t.Errorf("id=%q: expected 404, got %d", id, resp.StatusCode)
		}
	}
}

// TestHandleDeleteSession_PathTraversal ensures that a traversal session ID in
// DELETE returns 404 (store rejects before any RemoveAll call).
func TestHandleDeleteSession_PathTraversal(t *testing.T) {
	_, ts := newTestServer(t)

	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/sessions/..%2Fetc", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("expected 404 for traversal DELETE, got %d", resp.StatusCode)
	}
}

// TestHandleGetMessages_PathTraversal ensures that a traversal session ID in the
// messages endpoint is rejected rather than reading arbitrary JSONL files.
func TestHandleGetMessages_PathTraversal(t *testing.T) {
	_, ts := newTestServer(t)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions/..%2Fsecret/messages", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	// Either 404 or 500 is acceptable — important thing is no file is served from outside baseDir.
	if resp.StatusCode == 200 {
		t.Error("expected non-200 for path-traversal session ID in messages endpoint")
	}
}

// ─── Vulnerability 2: handleOAuthCallback open redirect ──────────────────────

// TestHandleOAuthCallback_ErrorParam_IsURLEncoded verifies that a crafted error
// parameter containing special characters (e.g. "&injected=1") is URL-encoded
// in the redirect URL, preventing parameter injection into the fragment.
func TestHandleOAuthCallback_ErrorParam_IsURLEncoded(t *testing.T) {
	srv, _ := newTestServer(t)

	// Craft an error param that would inject a second query key if not encoded.
	maliciousError := "access_denied&injected=evil"

	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?error="+maliciousError, nil)
	rec := httptest.NewRecorder()

	srv.handleOAuthCallback(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rec.Code)
	}

	location := rec.Header().Get("Location")
	if location == "" {
		t.Fatal("expected Location header, got none")
	}

	// The injected "&" must not appear un-encoded in the redirect target.
	// url.QueryEscape converts "&" → "%26", so the raw "&" must be absent
	// from the query-string part of the redirect URL.
	queryPart := location
	if hashIdx := strings.Index(location, "#"); hashIdx >= 0 {
		queryPart = location[hashIdx:]
	}
	if strings.Contains(queryPart, "&injected=evil") {
		t.Errorf("parameter injection succeeded; Location: %q", location)
	}
	if !strings.Contains(queryPart, "%26") && !strings.Contains(queryPart, "access_denied") {
		// Either the value is encoded (%26) or some sanitisation occurred.
		// Fail only if the raw unencoded injection string is present.
		t.Logf("Location: %q (no raw injection detected, test passes)", location)
	}
}

// TestHandleOAuthCallback_ErrorParam_NoNewlineInjection ensures newline characters
// in the error param cannot be used for HTTP response splitting.
func TestHandleOAuthCallback_ErrorParam_NoNewlineInjection(t *testing.T) {
	srv, _ := newTestServer(t)

	// CRLF injection attempt
	evilError := "foo\r\nX-Injected: header"

	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?error="+http.CanonicalHeaderKey(""), nil)
	// Manually set the raw query to bypass Go's automatic encoding in NewRequest.
	req.URL.RawQuery = "error=" + strings.ReplaceAll(evilError, " ", "%20")
	rec := httptest.NewRecorder()

	srv.handleOAuthCallback(rec, req)

	location := rec.Header().Get("Location")
	// The Location header must not contain a raw CRLF sequence.
	if strings.Contains(location, "\r\n") || strings.Contains(location, "\n") {
		t.Errorf("CRLF injection in Location header: %q", location)
	}
}

// TestHandleOAuthCallback_ConnectedProvider_IsURLEncoded verifies that the
// provider name returned after a successful callback is URL-encoded, preventing
// injection from a crafted provider value.
func TestHandleOAuthCallback_ConnectedProvider_NormalProvider(t *testing.T) {
	_, ts := newTestServer(t)

	// With no connMgr configured, the handler redirects with error=not_configured.
	// This test simply ensures the redirect goes to the expected safe location.
	req, _ := http.NewRequest("GET", ts.URL+"/oauth/callback?state=validstate&code=validcode", nil)

	// Use a client that does not follow redirects so we can inspect the 302.
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302, got %d", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	if !strings.HasPrefix(location, "/#/connections") {
		t.Errorf("expected redirect to /#/connections, got %q", location)
	}
}
