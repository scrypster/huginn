package providers_test

// hardening_iter1_test.go — coverage improvements for connections/providers.
// Tests NewSlackUser, SlackUser Name/DisplayName/OAuthConfig, Jira DisplayName,
// Bitbucket DisplayName, GetAccountInfo error paths using a mock HTTP server.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/connections/providers"
)

// ─── SlackUser provider ───────────────────────────────────────────────────────

func TestSlackUserProvider_Name(t *testing.T) {
	p := providers.NewSlackUser("cid", "csec")
	if got := p.Name(); got != connections.ProviderSlackUser {
		t.Errorf("Name() = %q, want %q", got, connections.ProviderSlackUser)
	}
}

func TestSlackUserProvider_DisplayName(t *testing.T) {
	p := providers.NewSlackUser("cid", "csec")
	dn := p.DisplayName()
	if dn == "" {
		t.Error("DisplayName() should not be empty")
	}
}

func TestSlackUserProvider_OAuthConfig_HasScopes(t *testing.T) {
	p := providers.NewSlackUser("cid", "csec")
	cfg := p.OAuthConfig("http://localhost/callback")
	if len(cfg.Scopes) == 0 {
		t.Error("expected at least one scope")
	}
	joined := strings.Join(cfg.Scopes, " ")
	if !strings.Contains(joined, "channels:read") {
		t.Errorf("expected channels:read scope in %v", cfg.Scopes)
	}
}

func TestSlackUserProvider_OAuthConfig_Endpoint(t *testing.T) {
	p := providers.NewSlackUser("cid", "csec")
	cfg := p.OAuthConfig("http://localhost/callback")
	if !strings.Contains(cfg.Endpoint.AuthURL, "slack.com") {
		t.Errorf("expected slack.com auth endpoint, got %q", cfg.Endpoint.AuthURL)
	}
}

// ─── Jira DisplayName ────────────────────────────────────────────────────────

func TestJiraProvider_DisplayName(t *testing.T) {
	p := providers.NewJira("cid", "csec")
	if got := p.DisplayName(); got != "Jira" {
		t.Errorf("DisplayName() = %q, want %q", got, "Jira")
	}
}

// ─── Bitbucket DisplayName ───────────────────────────────────────────────────

func TestBitbucketProvider_DisplayName(t *testing.T) {
	p := providers.NewBitbucket("cid", "csec")
	dn := p.DisplayName()
	if dn == "" {
		t.Error("DisplayName() should not be empty")
	}
}

// ─── GetAccountInfo error path — HTTP call fails ─────────────────────────────

// mockHTTPClient returns an *http.Client that routes through the given test server.
func mockHTTPClient(ts *httptest.Server) *http.Client {
	return ts.Client()
}

func TestGitHubProvider_GetAccountInfo_Error(t *testing.T) {
	// Mock server returns a non-JSON response to trigger a decode error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	p := providers.NewGitHub("", "")
	// Use a client pointing at srv so the request doesn't go to the real GitHub.
	client := &http.Client{Transport: &prefixRoundTripper{prefix: srv.URL, delegate: http.DefaultTransport}}
	_, err := p.GetAccountInfo(context.Background(), client)
	if err == nil {
		t.Error("expected error when response is not valid JSON")
	}
}

func TestSlackProvider_GetAccountInfo_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "not_authed"})
	}))
	defer srv.Close()

	p := providers.NewSlack("", "")
	client := &http.Client{Transport: &prefixRoundTripper{prefix: srv.URL, delegate: http.DefaultTransport}}
	_, err := p.GetAccountInfo(context.Background(), client)
	if err == nil {
		t.Error("expected error when Slack returns ok=false")
	}
}

func TestSlackUserProvider_GetAccountInfo_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "not_authed"})
	}))
	defer srv.Close()

	p := providers.NewSlackUser("", "")
	client := &http.Client{Transport: &prefixRoundTripper{prefix: srv.URL, delegate: http.DefaultTransport}}
	_, err := p.GetAccountInfo(context.Background(), client)
	if err == nil {
		t.Error("expected error when SlackUser returns ok=false")
	}
}

func TestJiraProvider_GetAccountInfo_NoResources(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	}))
	defer srv.Close()

	p := providers.NewJira("", "")
	client := &http.Client{Transport: &prefixRoundTripper{prefix: srv.URL, delegate: http.DefaultTransport}}
	_, err := p.GetAccountInfo(context.Background(), client)
	if err == nil {
		t.Error("expected error when Jira returns no accessible resources")
	}
}

// prefixRoundTripper rewrites the host of any request to a fixed mock server URL
// so we can intercept API calls without a real network.
type prefixRoundTripper struct {
	prefix   string
	delegate http.RoundTripper
}

func (rt *prefixRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = "http"
	cloned.URL.Host = strings.TrimPrefix(rt.prefix, "http://")
	return rt.delegate.RoundTrip(cloned)
}

// ─── Slack provider GetAccountInfo success ────────────────────────────────────

func TestSlackProvider_GetAccountInfo_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"user_id": "U123",
			"user":    "alice",
			"team":    "myteam",
			"team_id": "T456",
		})
	}))
	defer srv.Close()

	p := providers.NewSlack("", "")
	client := &http.Client{Transport: &prefixRoundTripper{prefix: srv.URL, delegate: http.DefaultTransport}}
	info, err := p.GetAccountInfo(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.ID != "U123" {
		t.Errorf("expected ID=U123, got %q", info.ID)
	}
	if !strings.Contains(info.Label, "alice") {
		t.Errorf("expected alice in label, got %q", info.Label)
	}
}

func TestSlackUserProvider_GetAccountInfo_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"user_id": "U789",
			"user":    "bob",
			"team":    "myteam",
		})
	}))
	defer srv.Close()

	p := providers.NewSlackUser("", "")
	client := &http.Client{Transport: &prefixRoundTripper{prefix: srv.URL, delegate: http.DefaultTransport}}
	info, err := p.GetAccountInfo(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.ID != "U789" {
		t.Errorf("expected ID=U789, got %q", info.ID)
	}
}

func TestJiraProvider_GetAccountInfo_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": "site-1", "name": "MyJira", "url": "https://myjira.atlassian.net"},
		})
	}))
	defer srv.Close()

	p := providers.NewJira("", "")
	client := &http.Client{Transport: &prefixRoundTripper{prefix: srv.URL, delegate: http.DefaultTransport}}
	info, err := p.GetAccountInfo(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.ID != "site-1" {
		t.Errorf("expected ID=site-1, got %q", info.ID)
	}
}

func TestGitHubProvider_GetAccountInfo_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":         1234,
			"login":      "octocat",
			"name":       "The Octocat",
			"avatar_url": "https://avatars.github.com/octocat",
		})
	}))
	defer srv.Close()

	p := providers.NewGitHub("", "")
	client := &http.Client{Transport: &prefixRoundTripper{prefix: srv.URL, delegate: http.DefaultTransport}}
	info, err := p.GetAccountInfo(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Label != "octocat" {
		t.Errorf("expected login=octocat, got %q", info.Label)
	}
}

// ─── Bitbucket GetAccountInfo ────────────────────────────────────────────────────

func TestBitbucketProvider_GetAccountInfo_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"username": "testuser",
			"uuid":     "{aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee}",
		})
	}))
	defer srv.Close()

	p := providers.NewBitbucket("", "")
	client := &http.Client{Transport: &prefixRoundTripper{prefix: srv.URL, delegate: http.DefaultTransport}}
	info, err := p.GetAccountInfo(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Label != "testuser" {
		t.Errorf("expected testuser, got %q", info.Label)
	}
}

func TestBitbucketProvider_GetAccountInfo_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	p := providers.NewBitbucket("", "")
	client := &http.Client{Transport: &prefixRoundTripper{prefix: srv.URL, delegate: http.DefaultTransport}}
	_, err := p.GetAccountInfo(context.Background(), client)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ─── Google GetAccountInfo ───────────────────────────────────────────────────────

func TestGoogleProvider_GetAccountInfo_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"email": "testuser@gmail.com",
			"sub":   "sub123",
		})
	}))
	defer srv.Close()

	p := providers.NewGoogle("", "", []string{})
	client := &http.Client{Transport: &prefixRoundTripper{prefix: srv.URL, delegate: http.DefaultTransport}}
	info, err := p.GetAccountInfo(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Label != "testuser@gmail.com" {
		t.Errorf("expected testuser@gmail.com, got %q", info.Label)
	}
}

func TestGoogleProvider_GetAccountInfo_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("bad json"))
	}))
	defer srv.Close()

	p := providers.NewGoogle("", "", []string{})
	client := &http.Client{Transport: &prefixRoundTripper{prefix: srv.URL, delegate: http.DefaultTransport}}
	_, err := p.GetAccountInfo(context.Background(), client)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
