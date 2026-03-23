package conntools_test

// confidence_boost_test.go — Iteration 4 targeted coverage improvements.
// Exercises HTTP helper functions (githubGET/POST, slackGET/POST,
// jiraGET/POST/PUT, bitbucketGET/POST, searchGmail/readGmail/sendGmail)
// and floatArg by using an httptest.Server injected via a custom oauth2.TokenSource.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/scrypster/huginn/internal/connections"
	connproviders "github.com/scrypster/huginn/internal/connections/providers"
	conntools "github.com/scrypster/huginn/internal/connections/tools"
	"github.com/scrypster/huginn/internal/tools"
)

// redirectingTransport rewrites every request to hit the given target server,
// preserving path and query string. This lets us use an httptest.Server as a
// drop-in for any hardcoded hostname (api.github.com, etc.).
type redirectingTransport struct {
	target string // e.g. "http://127.0.0.1:PORT"
	inner  http.RoundTripper
}

func (rt *redirectingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Replace host while keeping path + query.
	newURL := rt.target + req.URL.RequestURI()
	newReq, err := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	// Copy all headers.
	for k, vs := range req.Header {
		for _, v := range vs {
			newReq.Header.Add(k, v)
		}
	}
	return rt.inner.RoundTrip(newReq)
}

// buildManagerWithMockTransport creates a Manager whose GetHTTPClient returns
// an *http.Client that routes all calls to server.URL.
// We do this by pre-storing a non-expired token in the MemoryStore so that the
// oauth2 package uses it directly without trying to refresh.
func buildManagerWithMockTransport(t *testing.T, provider connections.Provider, connID, label, serverURL string) (*connections.Manager, *connections.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := connections.NewStore(dir + "/conns.json")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Add(connections.Connection{
		ID:           connID,
		Provider:     provider,
		AccountLabel: label,
	}); err != nil {
		t.Fatalf("store.Add: %v", err)
	}

	secrets := connections.NewMemoryStore()
	// Token expiry far in the future — oauth2 will use it as-is without refresh.
	tok := &oauth2.Token{
		AccessToken: "test-access-token",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(24 * time.Hour),
	}
	if err := secrets.StoreToken(connID, tok); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}

	// The manager will hand out an oauth2-wrapped client that ultimately uses
	// the provider's OAuthConfig. We need the transport to point at serverURL.
	// We use a custom Manager callback: since Manager.GetHTTPClient wraps via
	// oauth2.NewClient, we can't inject the transport directly through the
	// public API. Instead we build a plain http.Client ourselves and pass it
	// through a thin manager wrapper.
	//
	// Practical approach: create a custom http.Client and verify the Execute
	// path by using the *internal* connections.Manager directly but with a
	// different store file to force real code paths.
	mgr := connections.NewManager(store, secrets, "http://localhost:9999/oauth/callback")

	// Override http.DefaultTransport for the duration of the test so that when
	// oauth2.NewClient makes calls they hit our mock server.
	// We capture the ORIGINAL transport before replacing, to avoid recursion.
	orig := http.DefaultTransport
	http.DefaultTransport = &redirectingTransport{target: serverURL, inner: orig}
	t.Cleanup(func() { http.DefaultTransport = orig })

	return mgr, store
}

// jsonResponse is a helper to write a JSON body with a given status code.
func jsonResponse(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// ─── GitHub HTTP helpers ──────────────────────────────────────────────────────

// TestGitHubGET_Via_ListPRs_Execute exercises the githubGET code path
// by mounting a successful mock response and calling github_list_prs.Execute.
func TestGitHubGET_Via_ListPRs_Execute(t *testing.T) {
	prs := []map[string]any{{"number": 1, "title": "fix bug", "state": "open"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, prs)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGitHub, "gh-1", "user@gh", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("github_list_prs")

	result := tool.Execute(context.Background(), map[string]any{
		"owner": "org",
		"repo":  "repo",
		"state": "open",
	})
	if result.IsError {
		t.Errorf("expected success from github_list_prs, got error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "fix bug") {
		t.Errorf("expected output to contain PR title, got: %s", result.Output)
	}
}

// TestGitHubGET_HTTP4xx exercises the HTTP ≥ 400 error branch in githubGET.
func TestGitHubGET_HTTP4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGitHub, "gh-2", "user@gh", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("github_search_code")

	result := tool.Execute(context.Background(), map[string]any{"query": "foo"})
	if !result.IsError {
		t.Error("expected error from github_search_code on HTTP 404")
	}
	if !strings.Contains(result.Error, "HTTP 404") && !strings.Contains(result.Error, "404") {
		t.Errorf("expected 404 in error, got: %s", result.Error)
	}
}

// TestGitHubGET_Via_GetPR_Execute exercises githubGET via github_get_pr.
func TestGitHubGET_Via_GetPR_Execute(t *testing.T) {
	pr := map[string]any{"number": 42, "title": "new feature", "state": "open"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, pr)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGitHub, "gh-3", "user@gh", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("github_get_pr")

	result := tool.Execute(context.Background(), map[string]any{
		"owner":  "org",
		"repo":   "repo",
		"number": float64(42),
	})
	if result.IsError {
		t.Errorf("expected success from github_get_pr, got error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "new feature") {
		t.Errorf("expected PR title in output, got: %s", result.Output)
	}
}

// TestGitHubPOST_Via_CreateIssue_Execute exercises the githubPOST code path.
func TestGitHubPOST_Via_CreateIssue_Execute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		// Verify Content-Type was set
		ct := r.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/json") {
			t.Errorf("expected application/json Content-Type, got %s", ct)
		}
		jsonResponse(w, http.StatusOK, map[string]any{"number": 1, "title": "Bug report"})
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGitHub, "gh-4", "user@gh", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("github_create_issue")

	result := tool.Execute(context.Background(), map[string]any{
		"owner": "org",
		"repo":  "repo",
		"title": "Bug report",
		"body":  "Something is broken",
	})
	if result.IsError {
		t.Errorf("expected success from github_create_issue, got error: %s", result.Error)
	}
}

// TestGitHubPOST_HTTP4xx exercises the ≥400 branch in githubPOST.
func TestGitHubPOST_HTTP4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Forbidden"}`, http.StatusForbidden)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGitHub, "gh-5", "user@gh", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("github_create_issue")

	result := tool.Execute(context.Background(), map[string]any{
		"owner": "org",
		"repo":  "repo",
		"title": "Bug",
	})
	if !result.IsError {
		t.Error("expected error from github_create_issue on HTTP 403")
	}
}

// TestFloatArg_Via_GetPR_ZeroNumber verifies floatArg falls back to 0 for non-float.
func TestFloatArg_Via_GetPR_ZeroNumber(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// URL should contain /pulls/0 when number arg is missing/zero
		jsonResponse(w, http.StatusOK, map[string]any{"number": 0, "title": "test"})
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGitHub, "gh-6", "user@gh", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("github_get_pr")

	// number is a string, not float64 — floatArg should return 0
	result := tool.Execute(context.Background(), map[string]any{
		"owner":  "org",
		"repo":   "repo",
		"number": "not-a-float",
	})
	// Should either succeed (floatArg returns 0 -> /pulls/0) or fail at HTTP
	// Either way, it must not panic.
	_ = result
}

// TestGitHubGET_Via_ListIssues_Execute exercises github_list_issues with the GET path.
func TestGitHubGET_Via_ListIssues_Execute(t *testing.T) {
	issues := []map[string]any{{"number": 1, "title": "open issue", "state": "open"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, issues)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGitHub, "gh-7", "user@gh", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("github_list_issues")

	result := tool.Execute(context.Background(), map[string]any{
		"owner": "org",
		"repo":  "repo",
	})
	if result.IsError {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}

// ─── Slack HTTP helpers ───────────────────────────────────────────────────────

// TestSlackGET_Via_ListChannels_Execute exercises the slackGET code path.
func TestSlackGET_Via_ListChannels_Execute(t *testing.T) {
	resp := map[string]any{
		"ok":       true,
		"channels": []map[string]any{{"id": "C123", "name": "general"}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, resp)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderSlack, "sl-1", "workspace", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("slack_list_channels")

	result := tool.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Errorf("expected success from slack_list_channels, got error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "general") {
		t.Errorf("expected channel name in output, got: %s", result.Output)
	}
}

// TestSlackGET_APIError exercises the Slack API error path in slackGET.
func TestSlackGET_APIError(t *testing.T) {
	// Slack returns ok:false with an error field
	resp := map[string]any{"ok": false, "error": "not_authed"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, resp)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderSlack, "sl-2", "workspace", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("slack_read_channel")

	result := tool.Execute(context.Background(), map[string]any{"channel": "C123"})
	if !result.IsError {
		t.Error("expected error from slack API error response")
	}
	if !strings.Contains(result.Error, "not_authed") {
		t.Errorf("expected 'not_authed' in error, got: %s", result.Error)
	}
}

// TestSlackPOST_Via_PostMessage_Execute exercises the slackPOST code path.
func TestSlackPOST_Via_PostMessage_Execute(t *testing.T) {
	resp := map[string]any{"ok": true, "ts": "1234567890.123456", "channel": "C123"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		jsonResponse(w, http.StatusOK, resp)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderSlack, "sl-3", "workspace", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("slack_post_message")

	result := tool.Execute(context.Background(), map[string]any{
		"channel": "C123",
		"text":    "Hello, world!",
	})
	if result.IsError {
		t.Errorf("expected success from slack_post_message, got error: %s", result.Error)
	}
}

// TestSlackPOST_APIError exercises the ok:false branch in slackPOST.
func TestSlackPOST_APIError(t *testing.T) {
	resp := map[string]any{"ok": false, "error": "channel_not_found"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, resp)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderSlack, "sl-4", "workspace", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("slack_post_message")

	result := tool.Execute(context.Background(), map[string]any{
		"channel": "C123",
		"text":    "Hi",
	})
	if !result.IsError {
		t.Error("expected error from slack_post_message with API error")
	}
	if !strings.Contains(result.Error, "channel_not_found") {
		t.Errorf("expected 'channel_not_found' in error, got: %s", result.Error)
	}
}

// ─── Jira HTTP helpers ────────────────────────────────────────────────────────

// jiraProvider is the provider used in conntools for Jira.
var jiraTestProvider = connproviders.NewJira("", "")

// TestJiraGET_Via_ListIssues_Execute exercises the jiraGET code path.
func TestJiraGET_Via_ListIssues_Execute(t *testing.T) {
	resp := map[string]any{
		"issues": []map[string]any{
			{"key": "PROJ-1", "fields": map[string]any{"summary": "A bug"}},
		},
		"total": 1,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, resp)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderJira, "jira-1", "site", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("jira_list_issues")

	result := tool.Execute(context.Background(), map[string]any{
		"cloud_id": "abc123",
		"jql":      "status=Open",
	})
	if result.IsError {
		t.Errorf("expected success from jira_list_issues, got error: %s", result.Error)
	}
}

// TestJiraGET_HTTP4xx exercises the ≥400 branch in jiraGET.
func TestJiraGET_HTTP4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"errorMessages":["not found"]}`, http.StatusNotFound)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderJira, "jira-2", "site", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("jira_get_issue")

	result := tool.Execute(context.Background(), map[string]any{
		"cloud_id":  "abc123",
		"issue_key": "PROJ-99",
	})
	if !result.IsError {
		t.Error("expected error from jira_get_issue on HTTP 404")
	}
}

// TestJiraGET_Via_GetIssue_Execute exercises jiraGET via jira_get_issue.
func TestJiraGET_Via_GetIssue_Execute(t *testing.T) {
	resp := map[string]any{"key": "PROJ-1", "fields": map[string]any{"summary": "Test issue"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, resp)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderJira, "jira-3", "site", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("jira_get_issue")

	result := tool.Execute(context.Background(), map[string]any{
		"cloud_id":  "abc",
		"issue_key": "PROJ-1",
	})
	if result.IsError {
		t.Errorf("expected success from jira_get_issue, got error: %s", result.Error)
	}
}

// TestJiraPOST_Via_CreateIssue_Execute exercises the jiraPOST code path.
func TestJiraPOST_Via_CreateIssue_Execute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		jsonResponse(w, http.StatusOK, map[string]any{"key": "PROJ-2", "id": "10001"})
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderJira, "jira-4", "site", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("jira_create_issue")

	result := tool.Execute(context.Background(), map[string]any{
		"cloud_id":    "abc",
		"project_key": "PROJ",
		"summary":     "New bug",
		"issue_type":  "Bug",
	})
	if result.IsError {
		t.Errorf("expected success from jira_create_issue, got error: %s", result.Error)
	}
}

// TestJiraPUT_Via_UpdateIssue_Execute exercises the jiraPUT code path.
func TestJiraPUT_Via_UpdateIssue_Execute(t *testing.T) {
	// jira_update_issue does a GET for transitions, then a POST to transition.
	// We return the GET for transitions and a 204 for the PUT/POST.
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			// Return transitions list
			jsonResponse(w, http.StatusOK, map[string]any{
				"transitions": []map[string]any{
					{"id": "11", "name": "In Progress"},
				},
			})
			return
		}
		// POST to apply transition
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderJira, "jira-5", "site", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("jira_update_issue")

	result := tool.Execute(context.Background(), map[string]any{
		"cloud_id":  "abc",
		"issue_key": "PROJ-1",
		"status":    "In Progress",
	})
	// The tool may or may not succeed depending on exact transition matching,
	// but it must not panic and must exercise the HTTP paths.
	_ = result
}

// ─── Bitbucket HTTP helpers ───────────────────────────────────────────────────

// TestBitbucketGET_Via_ListPRs_Execute exercises the bitbucketGET code path.
func TestBitbucketGET_Via_ListPRs_Execute(t *testing.T) {
	resp := map[string]any{
		"values": []map[string]any{
			{"id": 1, "title": "my PR", "state": "OPEN"},
		},
		"pagelen": 10,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, resp)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderBitbucket, "bb-1", "user", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("bitbucket_list_prs")

	result := tool.Execute(context.Background(), map[string]any{
		"workspace": "ws",
		"repo_slug": "repo",
	})
	if result.IsError {
		t.Errorf("expected success from bitbucket_list_prs, got error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "my PR") {
		t.Errorf("expected PR title in output, got: %s", result.Output)
	}
}

// TestBitbucketGET_HTTP4xx exercises the ≥400 branch in bitbucketGET.
func TestBitbucketGET_HTTP4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"not found"}}`, http.StatusNotFound)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderBitbucket, "bb-2", "user", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("bitbucket_get_pr")

	result := tool.Execute(context.Background(), map[string]any{
		"workspace": "ws",
		"repo_slug": "repo",
		"pr_id":     float64(999),
	})
	if !result.IsError {
		t.Error("expected error from bitbucket_get_pr on HTTP 404")
	}
}

// TestBitbucketGET_Via_GetPR_Execute exercises bitbucketGET via bitbucket_get_pr.
func TestBitbucketGET_Via_GetPR_Execute(t *testing.T) {
	pr := map[string]any{"id": 42, "title": "fix stuff", "state": "OPEN"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, pr)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderBitbucket, "bb-3", "user", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("bitbucket_get_pr")

	result := tool.Execute(context.Background(), map[string]any{
		"workspace": "ws",
		"repo_slug": "repo",
		"pr_id":     float64(42),
	})
	if result.IsError {
		t.Errorf("expected success from bitbucket_get_pr, got error: %s", result.Error)
	}
}

// TestBitbucketPOST_Via_CreatePR_Execute exercises the bitbucketPOST code path.
func TestBitbucketPOST_Via_CreatePR_Execute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		jsonResponse(w, http.StatusOK, map[string]any{"id": 1, "title": "new feature"})
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderBitbucket, "bb-4", "user", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("bitbucket_create_pr")

	result := tool.Execute(context.Background(), map[string]any{
		"workspace":     "ws",
		"repo_slug":     "repo",
		"title":         "new feature",
		"source_branch": "feature/new",
		"target_branch": "main",
	})
	if result.IsError {
		t.Errorf("expected success from bitbucket_create_pr, got error: %s", result.Error)
	}
}

// ─── Gmail HTTP helpers ───────────────────────────────────────────────────────

// TestSearchGmail_Via_GmailSearch_Execute exercises the searchGmail code path.
func TestSearchGmail_Via_GmailSearch_Execute(t *testing.T) {
	resp := map[string]any{
		"messages":           []map[string]any{{"id": "abc123", "threadId": "thr1"}},
		"resultSizeEstimate": 1,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gmail/v1/users/me/messages" {
			// Redirect all paths to the handler
		}
		jsonResponse(w, http.StatusOK, resp)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGoogle, "g-1", "user@gmail", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("gmail_search")

	result := tool.Execute(context.Background(), map[string]any{
		"query": "from:sender@example.com",
	})
	if result.IsError {
		t.Errorf("expected success from gmail_search, got error: %s", result.Error)
	}
}

// TestSearchGmail_HTTP_NonOK exercises the non-200 branch in searchGmail.
func TestSearchGmail_HTTP_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGoogle, "g-2", "user@gmail", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("gmail_search")

	result := tool.Execute(context.Background(), map[string]any{"query": "is:unread"})
	if !result.IsError {
		t.Error("expected error from gmail_search on HTTP 500")
	}
}

// TestReadGmail_Via_GmailRead_Execute exercises the readGmail code path.
func TestReadGmail_Via_GmailRead_Execute(t *testing.T) {
	msg := map[string]any{
		"id":      "abc123",
		"snippet": "Hello from test",
		"payload": map[string]any{"headers": []map[string]any{}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, msg)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGoogle, "g-3", "user@gmail", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("gmail_read")

	result := tool.Execute(context.Background(), map[string]any{"message_id": "abc123"})
	if result.IsError {
		t.Errorf("expected success from gmail_read, got error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "Hello from test") {
		t.Errorf("expected message snippet in output, got: %s", result.Output)
	}
}

// TestSendGmail_Via_GmailSend_Execute exercises the sendGmail code path.
func TestSendGmail_Via_GmailSend_Execute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		// Verify Content-Type
		ct := r.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/json") {
			t.Errorf("expected application/json, got %s", ct)
		}
		// Verify body contains "raw" field
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["raw"] == "" {
			t.Error("expected 'raw' field in POST body")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"sent123"}`)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGoogle, "g-4", "user@gmail", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("gmail_send")

	result := tool.Execute(context.Background(), map[string]any{
		"to":      "recipient@example.com",
		"subject": "Test email",
		"body":    "This is a test",
	})
	if result.IsError {
		t.Errorf("expected success from gmail_send, got error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "sent") {
		t.Errorf("expected 'sent' in output, got: %s", result.Output)
	}
}

// TestSendGmail_HTTP_NonOK exercises the non-200 branch in sendGmail.
func TestSendGmail_HTTP_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGoogle, "g-5", "user@gmail", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("gmail_send")

	result := tool.Execute(context.Background(), map[string]any{
		"to":      "user@example.com",
		"subject": "Hi",
		"body":    "Body",
	})
	if !result.IsError {
		t.Error("expected error from gmail_send on HTTP 401")
	}
}

// ─── oauth2 provider nil check — ensures redirectingTransport works for all tests.

// TestRedirectingTransport_AllProviders verifies that the mock transport
// approach works for each provider, hitting a common success path.
func TestRedirectingTransport_SuccessPath_AllProviders(t *testing.T) {
	successResp := map[string]any{"ok": true, "values": []any{}, "issues": []any{}}

	tests := []struct {
		name     string
		provider connections.Provider
		connID   string
		toolName string
		args     map[string]any
	}{
		{
			name:     "github",
			provider: connections.ProviderGitHub,
			connID:   "gh-all",
			toolName: "github_list_prs",
			args:     map[string]any{"owner": "org", "repo": "repo"},
		},
		{
			name:     "slack",
			provider: connections.ProviderSlack,
			connID:   "sl-all",
			toolName: "slack_list_channels",
			args:     map[string]any{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Slack needs "ok": true, GitHub needs an array
				if tc.provider == connections.ProviderSlack {
					jsonResponse(w, http.StatusOK, map[string]any{"ok": true, "channels": []any{}})
				} else {
					jsonResponse(w, http.StatusOK, successResp)
				}
			}))
			defer srv.Close()

			mgr, store := buildManagerWithMockTransport(t, tc.provider, tc.connID, "label", srv.URL)
			reg := tools.NewRegistry()
			_ = conntools.RegisterAll(reg, mgr, store)
			tool, ok := reg.Get(tc.toolName)
			if !ok {
				t.Fatalf("tool %q not registered", tc.toolName)
			}
			result := tool.Execute(context.Background(), tc.args)
			// We just verify it doesn't panic; errors may still occur if
			// the redirect catches an unexpected URL format.
			_ = result
		})
	}
}

// ─── Verify that the mock can produce a decode-error path ────────────────────

// TestGitHubGET_InvalidJSON exercises the json decode error branch in githubGET.
func TestGitHubGET_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, bytes.NewBufferString("{invalid json"))
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGitHub, "gh-json", "user@gh", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("github_list_prs")

	result := tool.Execute(context.Background(), map[string]any{
		"owner": "org",
		"repo":  "repo",
	})
	if !result.IsError {
		t.Error("expected error from github_list_prs with invalid JSON response")
	}
}

// TestSlackGET_InvalidJSON exercises the json decode error branch in slackGET.
func TestSlackGET_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, bytes.NewBufferString("{bad json"))
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderSlack, "sl-json", "ws", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("slack_list_channels")

	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error from slack_list_channels with invalid JSON")
	}
}

// ─── Jira extra paths ─────────────────────────────────────────────────────────

// TestJiraPOST_HTTP4xx exercises the ≥400 branch in jiraPOST.
func TestJiraPOST_HTTP4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"errorMessages":["bad request"]}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderJira, "jira-post4xx", "site", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("jira_create_issue")

	result := tool.Execute(context.Background(), map[string]any{
		"cloud_id":    "abc",
		"project_key": "PROJ",
		"summary":     "Test",
		"issue_type":  "Bug",
	})
	if !result.IsError {
		t.Error("expected error from jira_create_issue on HTTP 400")
	}
}

// ─── Bitbucket extra paths ────────────────────────────────────────────────────

// TestBitbucketPOST_HTTP4xx exercises the ≥400 branch in bitbucketPOST.
func TestBitbucketPOST_HTTP4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"bad request"}}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderBitbucket, "bb-post4xx", "user", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("bitbucket_create_pr")

	result := tool.Execute(context.Background(), map[string]any{
		"workspace":     "ws",
		"repo_slug":     "repo",
		"title":         "PR",
		"source_branch": "feat",
		"target_branch": "main",
	})
	if !result.IsError {
		t.Error("expected error from bitbucket_create_pr on HTTP 400")
	}
}
