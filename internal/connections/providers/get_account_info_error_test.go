package providers_test

// coverage_boost95_test.go — targeted tests to push providers from 86.2% to 95%+.
//
// Remaining gaps after hardening_iter1_test.go:
//   1. client.Do failure branches in all providers' GetAccountInfo (transport error).
//   2. JSON decode error branches for Jira and Slack variants.
//   3. OAuthConfig product branches (calendar) in Google provider.
//
// The prefixRoundTripper type is declared in hardening_iter1_test.go (same package).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scrypster/huginn/internal/connections/providers"
)

// errorTransport is an http.RoundTripper that always returns a transport-level error.
type errorTransport struct{ err error }

func (e *errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, e.err
}

// errClient returns an *http.Client whose every request fails at the transport layer.
func errClient() *http.Client {
	return &http.Client{Transport: &errorTransport{err: errors.New("simulated transport failure")}}
}

// ─── GitHub GetAccountInfo — client.Do error ──────────────────────────────────

func TestGitHubProvider_GetAccountInfo_DoError(t *testing.T) {
	p := providers.NewGitHub("cid", "csec")
	_, err := p.GetAccountInfo(context.Background(), errClient())
	if err == nil {
		t.Fatal("expected error when client.Do fails for GitHub")
	}
}

// ─── Jira GetAccountInfo — client.Do error ────────────────────────────────────

func TestJiraProvider_GetAccountInfo_DoError(t *testing.T) {
	p := providers.NewJira("cid", "csec")
	_, err := p.GetAccountInfo(context.Background(), errClient())
	if err == nil {
		t.Fatal("expected error when client.Do fails for Jira")
	}
}

// ─── Jira GetAccountInfo — JSON decode error (body is not a JSON array) ───────

// TestJiraProvider_GetAccountInfo_DecodeNotArray exercises the decode-error branch
// when the server returns a non-array JSON body (object instead of []resource).
func TestJiraProvider_GetAccountInfo_DecodeNotArray(t *testing.T) {
	// Return a JSON object (not an array), which will fail to decode into []struct.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"error": "not an array"})
	}))
	defer srv.Close()

	p := providers.NewJira("", "")
	client := &http.Client{Transport: &prefixRoundTripper{prefix: srv.URL, delegate: http.DefaultTransport}}
	_, err := p.GetAccountInfo(context.Background(), client)
	if err == nil {
		t.Error("expected error when Jira response is not a JSON array")
	}
}

// ─── Slack GetAccountInfo — client.Do error ───────────────────────────────────

func TestSlackProvider_GetAccountInfo_DoError(t *testing.T) {
	p := providers.NewSlack("cid", "csec")
	_, err := p.GetAccountInfo(context.Background(), errClient())
	if err == nil {
		t.Fatal("expected error when client.Do fails for Slack")
	}
}

// ─── Slack GetAccountInfo — JSON decode error ─────────────────────────────────

// TestSlackProvider_GetAccountInfo_DecodeError exercises the JSON decode error branch.
func TestSlackProvider_GetAccountInfo_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not valid json {{{"))
	}))
	defer srv.Close()

	p := providers.NewSlack("", "")
	client := &http.Client{Transport: &prefixRoundTripper{prefix: srv.URL, delegate: http.DefaultTransport}}
	_, err := p.GetAccountInfo(context.Background(), client)
	if err == nil {
		t.Error("expected error when Slack response body is invalid JSON")
	}
}

// ─── SlackUser GetAccountInfo — client.Do error ───────────────────────────────

func TestSlackUserProvider_GetAccountInfo_DoError(t *testing.T) {
	p := providers.NewSlackUser("cid", "csec")
	_, err := p.GetAccountInfo(context.Background(), errClient())
	if err == nil {
		t.Fatal("expected error when client.Do fails for SlackUser")
	}
}

// ─── SlackUser GetAccountInfo — JSON decode error ─────────────────────────────

func TestSlackUserProvider_GetAccountInfo_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not valid json {{{"))
	}))
	defer srv.Close()

	p := providers.NewSlackUser("", "")
	client := &http.Client{Transport: &prefixRoundTripper{prefix: srv.URL, delegate: http.DefaultTransport}}
	_, err := p.GetAccountInfo(context.Background(), client)
	if err == nil {
		t.Error("expected error when SlackUser response body is invalid JSON")
	}
}

// ─── Google GetAccountInfo — client.Do error ──────────────────────────────────

func TestGoogleProvider_GetAccountInfo_DoError(t *testing.T) {
	p := providers.NewGoogle("cid", "csec", []string{"gmail"})
	_, err := p.GetAccountInfo(context.Background(), errClient())
	if err == nil {
		t.Fatal("expected error when client.Do fails for Google")
	}
}

// ─── Bitbucket GetAccountInfo — client.Do error ───────────────────────────────

func TestBitbucketProvider_GetAccountInfo_DoError(t *testing.T) {
	p := providers.NewBitbucket("cid", "csec")
	_, err := p.GetAccountInfo(context.Background(), errClient())
	if err == nil {
		t.Fatal("expected error when client.Do fails for Bitbucket")
	}
}

// ─── Google OAuthConfig — additional product branches ─────────────────────────

// TestGoogleProvider_OAuthConfig_CalendarScopes exercises the "calendar" key in GoogleScopes.
func TestGoogleProvider_OAuthConfig_CalendarScopes(t *testing.T) {
	p := providers.NewGoogle("cid", "csec", []string{"calendar"})
	cfg := p.OAuthConfig("http://localhost/callback")
	found := false
	for _, s := range cfg.Scopes {
		if s == "https://www.googleapis.com/auth/calendar.readonly" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected calendar scope in %v", cfg.Scopes)
	}
}

// TestGoogleProvider_OAuthConfig_UnknownProduct exercises the branch where a product
// key is not in GoogleScopes (no extra scopes added, only base email scope).
func TestGoogleProvider_OAuthConfig_UnknownProduct(t *testing.T) {
	p := providers.NewGoogle("cid", "csec", []string{"nonexistent_product"})
	cfg := p.OAuthConfig("http://localhost/callback")
	for _, s := range cfg.Scopes {
		if s == "nonexistent_product" {
			t.Error("unknown product scope should not appear in the config")
		}
	}
	if len(cfg.Scopes) == 0 {
		t.Error("expected at least the base userinfo.email scope")
	}
}

// TestGoogleProvider_OAuthConfig_MultipleProducts exercises more than one product
// to ensure all scopes from each product are included.
func TestGoogleProvider_OAuthConfig_MultipleProducts(t *testing.T) {
	p := providers.NewGoogle("cid", "csec", []string{"gmail", "calendar", "drive"})
	cfg := p.OAuthConfig("http://localhost/callback")
	// gmail (2 scopes) + calendar (1) + drive (1) + base email = 5 minimum
	if len(cfg.Scopes) < 5 {
		t.Errorf("expected at least 5 scopes for gmail+calendar+drive, got %d: %v", len(cfg.Scopes), cfg.Scopes)
	}
}

// TestGoogleProvider_OAuthConfig_EmptyProducts exercises no products (base email only).
func TestGoogleProvider_OAuthConfig_EmptyProducts(t *testing.T) {
	p := providers.NewGoogle("cid", "csec", []string{})
	cfg := p.OAuthConfig("http://localhost/callback")
	// Only the base userinfo.email scope should be present.
	if len(cfg.Scopes) != 1 {
		t.Errorf("expected exactly 1 scope for empty products, got %d: %v", len(cfg.Scopes), cfg.Scopes)
	}
}
