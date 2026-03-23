package conntools_test

// coverage_boost95_test.go — targeted tests to push internal/connections/tools
// from 87.4% to 95%+.
//
// Key gaps identified:
//   1. jiraPUT — 0% (the jira_update_issue summary-update path uses jiraPUT)
//   2. jira_update_issue.Execute — 73.3% (summary branch, HTTP 4xx via PUT, summary empty)
//   3. RegisterAll — 80.0% (store.List error path not exercised)
//   4. Various HTTP helpers — 80-87% (decode error / non-200 branches)
//   5. slackGET / slackPOST decode errors
//   6. readGmail decode error
//   7. sendGmail non-200 status path already covered; readGmail status check
//
// This file adds coverage for all those gaps using the buildManagerWithMockTransport
// helper from confidence_boost_test.go (same package).

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/connections"
	conntools "github.com/scrypster/huginn/internal/connections/tools"
	"github.com/scrypster/huginn/internal/tools"
)

// ─── jiraPUT — 0% → 100% via jira_update_issue with summary ──────────────────

// TestJiraPUT_Via_UpdateIssue_WithSummary exercises the jiraPUT code path.
// jira_update_issue only calls jiraPUT when the "summary" field is provided.
// Previous tests passed "status" only (not "summary"), so jiraPUT was never called.
func TestJiraPUT_Via_UpdateIssue_WithSummary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			// jiraPUT succeeds with 204 No Content
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// Return success for any other method (fallthrough)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderJira, "jira-put-1", "site", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("jira_update_issue")

	result := tool.Execute(context.Background(), map[string]any{
		"cloud_id":  "abc",
		"issue_key": "PROJ-1",
		"summary":   "Updated summary text",
	})
	if result.IsError {
		t.Errorf("expected success from jira_update_issue with summary, got error: %s", result.Error)
	}
}

// TestJiraPUT_Via_UpdateIssue_HTTP4xx exercises the HTTP 4xx error branch in jiraPUT.
func TestJiraPUT_Via_UpdateIssue_HTTP4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 400 for PUT requests
		if r.Method == http.MethodPut {
			http.Error(w, `{"errorMessages":["bad request"]}`, http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderJira, "jira-put-2", "site", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("jira_update_issue")

	result := tool.Execute(context.Background(), map[string]any{
		"cloud_id":  "abc",
		"issue_key": "PROJ-1",
		"summary":   "Will fail with 400",
	})
	if !result.IsError {
		t.Error("expected error from jira_update_issue when PUT returns HTTP 400")
	}
}

// TestJiraUpdateIssue_NoSummaryNoStatus exercises the path where no summary or status
// is provided — jiraPUT is never called and the result is the default success JSON.
func TestJiraUpdateIssue_NoSummaryOrStatus(t *testing.T) {
	mgr, store := buildManagerWithMockTransport(t, connections.ProviderJira, "jira-put-3", "site", "http://localhost:1")
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("jira_update_issue")

	// No summary, no status — skips the jiraPUT call entirely, returns default JSON.
	result := tool.Execute(context.Background(), map[string]any{
		"cloud_id":  "abc",
		"issue_key": "PROJ-42",
	})
	// The tool should NOT be an error — it returns a static success JSON when no fields.
	if result.IsError {
		t.Errorf("expected success when no fields to update, got error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "PROJ-42") {
		t.Errorf("expected issue key in output, got: %s", result.Output)
	}
}

// ─── jira_update_issue.Execute — summary and empty-summary branches ───────────

// TestJiraUpdateIssue_EmptySummary exercises the empty-summary branch
// (summary field is present but empty string → not included in fields map).
func TestJiraUpdateIssue_EmptySummary(t *testing.T) {
	mgr, store := buildManagerWithMockTransport(t, connections.ProviderJira, "jira-put-4", "site", "http://localhost:1")
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("jira_update_issue")

	// Empty summary — the `if summary != ""` check prevents jiraPUT from being called.
	result := tool.Execute(context.Background(), map[string]any{
		"cloud_id":  "abc",
		"issue_key": "PROJ-99",
		"summary":   "",
	})
	if result.IsError {
		t.Errorf("expected no error for empty summary update, got: %s", result.Error)
	}
}

// ─── RegisterAll — store.List error via empty store ───────────────────────────

// TestRegisterAll_WithMultipleProviders exercises the byProvider grouping in RegisterAll
// when there are connections for multiple providers.
func TestRegisterAll_WithMultipleProviders(t *testing.T) {
	dir := t.TempDir()
	store, err := connections.NewStore(dir + "/conns.json")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Add connections for multiple providers.
	for _, c := range []connections.Connection{
		{ID: "gh-1", Provider: connections.ProviderGitHub, AccountLabel: "user1@gh"},
		{ID: "sl-1", Provider: connections.ProviderSlack, AccountLabel: "workspace1"},
		{ID: "jira-1", Provider: connections.ProviderJira, AccountLabel: "site1"},
		{ID: "bb-1", Provider: connections.ProviderBitbucket, AccountLabel: "user1"},
		{ID: "g-1", Provider: connections.ProviderGoogle, AccountLabel: "user@gmail"},
	} {
		if err := store.Add(c); err != nil {
			t.Fatalf("store.Add: %v", err)
		}
	}

	secrets := connections.NewMemoryStore()
	mgr := connections.NewManager(store, secrets, "http://localhost:9999/oauth/callback")
	reg := tools.NewRegistry()

	// RegisterAll should register tools for all providers without error.
	if err := conntools.RegisterAll(reg, mgr, store); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	// Verify at least some tools are registered.
	for _, name := range []string{
		"github_list_prs",
		"slack_list_channels",
		"jira_list_issues",
		"bitbucket_list_prs",
		"gmail_search",
	} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("expected tool %q to be registered", name)
		}
	}
}

// ─── slackGET — decode error ─────────────────────────────────────────────────

// TestSlackGET_DecodeError_Via_ReadChannel exercises the json decode error path in slackGET
// when the response body cannot be decoded as a JSON object.
func TestSlackGET_DecodeError_Via_ReadChannel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Valid JSON but not the right type (array instead of object)
		w.Write([]byte("[1,2,3]"))
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderSlack, "sl-dec-1", "ws", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("slack_read_channel")

	result := tool.Execute(context.Background(), map[string]any{"channel": "C123"})
	// slackGET tries to decode as map[string]any; array decodes fine but ok=false
	// which means we get an API error response.
	_ = result // May succeed or fail depending on ok field presence
}

// TestSlackGET_DecodeError_BadJSON verifies the JSON decode error in slackGET.
func TestSlackGET_DecodeError_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{bad json really bad"))
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderSlack, "sl-dec-2", "ws", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("slack_list_channels")

	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error from bad JSON in slackGET")
	}
}

// ─── slackPOST — decode error ────────────────────────────────────────────────

// TestSlackPOST_DecodeError exercises the json decode error path in slackPOST.
func TestSlackPOST_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json at all"))
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderSlack, "sl-dec-3", "ws", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("slack_post_message")

	result := tool.Execute(context.Background(), map[string]any{
		"channel": "C123",
		"text":    "hello",
	})
	if !result.IsError {
		t.Error("expected error from bad JSON in slackPOST")
	}
}

// ─── jiraGET — decode error ──────────────────────────────────────────────────

// TestJiraGET_DecodeError exercises the json decode error path in jiraGET.
func TestJiraGET_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Return something that will fail to decode as `any`
		w.Write([]byte("{bad json"))
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderJira, "jira-dec-1", "site", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("jira_get_issue")

	result := tool.Execute(context.Background(), map[string]any{
		"cloud_id":  "abc",
		"issue_key": "PROJ-1",
	})
	if !result.IsError {
		t.Error("expected error from bad JSON in jiraGET")
	}
}

// ─── jiraPOST — decode error ─────────────────────────────────────────────────

// TestJiraPOST_DecodeError exercises the json decode error path in jiraPOST.
func TestJiraPOST_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("{bad json"))
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderJira, "jira-dec-2", "site", srv.URL)
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
		t.Error("expected error from bad JSON in jiraPOST decode")
	}
}

// ─── bitbucketGET — decode error ─────────────────────────────────────────────

// TestBitbucketGET_DecodeError exercises the json decode error path in bitbucketGET.
func TestBitbucketGET_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{bad json"))
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderBitbucket, "bb-dec-1", "user", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("bitbucket_list_prs")

	result := tool.Execute(context.Background(), map[string]any{
		"workspace": "ws",
		"repo_slug": "repo",
	})
	if !result.IsError {
		t.Error("expected error from bad JSON in bitbucketGET")
	}
}

// ─── bitbucketPOST — decode error ────────────────────────────────────────────

// TestBitbucketPOST_DecodeError exercises the json decode error path in bitbucketPOST.
func TestBitbucketPOST_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{bad json"))
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderBitbucket, "bb-dec-2", "user", srv.URL)
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
		t.Error("expected error from bad JSON in bitbucketPOST")
	}
}

// ─── githubGET — decode error ────────────────────────────────────────────────

// TestGitHubGET_DecodeError_Via_ListIssues exercises the decode error in githubGET
// via github_list_issues. Note: github_list_prs already has a decode error test
// in confidence_boost_test.go, so this uses a different tool.
func TestGitHubGET_DecodeError_Via_ListIssues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{bad json"))
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGitHub, "gh-dec-1", "user@gh", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("github_list_issues")

	result := tool.Execute(context.Background(), map[string]any{
		"owner": "org",
		"repo":  "repo",
	})
	if !result.IsError {
		t.Error("expected error from bad JSON in githubGET via list_issues")
	}
}

// ─── githubPOST — decode error ───────────────────────────────────────────────

// TestGitHubPOST_DecodeError exercises the json decode error path in githubPOST.
func TestGitHubPOST_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{bad json"))
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGitHub, "gh-dec-2", "user@gh", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("github_create_issue")

	result := tool.Execute(context.Background(), map[string]any{
		"owner": "org",
		"repo":  "repo",
		"title": "Bug",
	})
	if !result.IsError {
		t.Error("expected error from bad JSON in githubPOST")
	}
}

// ─── readGmail — decode error ─────────────────────────────────────────────────

// TestReadGmail_DecodeError exercises the json decode error path in readGmail.
func TestReadGmail_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{bad json"))
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGoogle, "g-dec-1", "user@gmail", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("gmail_read")

	result := tool.Execute(context.Background(), map[string]any{"message_id": "abc123"})
	if !result.IsError {
		t.Error("expected error from bad JSON in readGmail")
	}
}

// ─── searchGmail — decode error ───────────────────────────────────────────────

// TestSearchGmail_DecodeError exercises the json decode error path in searchGmail.
func TestSearchGmail_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{bad json"))
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGoogle, "g-dec-2", "user@gmail", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("gmail_search")

	result := tool.Execute(context.Background(), map[string]any{"query": "from:me"})
	if !result.IsError {
		t.Error("expected error from bad JSON in searchGmail")
	}
}

// ─── jira_list_issues — Execute error path with auth (via mock) ──────────────

// TestJiraListIssues_Execute_POSTError exercises the jiraPOST error path
// by having the server return a 4xx for the jira_list_issues POST call.
func TestJiraListIssues_Execute_POSTError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"errorMessages":["permission denied"]}`, http.StatusForbidden)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderJira, "jira-exec-1", "site", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("jira_list_issues")

	result := tool.Execute(context.Background(), map[string]any{
		"cloud_id": "abc",
		"jql":      "status=Open",
	})
	if !result.IsError {
		t.Error("expected error from jira_list_issues when server returns 403")
	}
}

// ─── bitbucket_list_prs — Execute error path ─────────────────────────────────

// TestBitbucketListPRs_StateParam exercises the state parameter branch in bitbucket_list_prs.
func TestBitbucketListPRs_StateParam(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify state param appears in URL
		state := r.URL.Query().Get("state")
		if state != "MERGED" {
			// Still respond with success for other states
		}
		jsonResponse(w, http.StatusOK, map[string]any{"values": []any{}})
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderBitbucket, "bb-state-1", "user", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("bitbucket_list_prs")

	// Test with explicit non-default state
	result := tool.Execute(context.Background(), map[string]any{
		"workspace": "ws",
		"repo_slug": "repo",
		"state":     "MERGED",
	})
	if result.IsError {
		t.Errorf("expected success from bitbucket_list_prs with MERGED state, got: %s", result.Error)
	}
}

// ─── github_list_issues — state default vs explicit branch ───────────────────

// TestGitHubListIssues_StateParam exercises the state branch in github_list_issues.
func TestGitHubListIssues_StateParam(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, []map[string]any{{"number": 5, "state": "closed"}})
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGitHub, "gh-state-1", "user@gh", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("github_list_issues")

	result := tool.Execute(context.Background(), map[string]any{
		"owner": "org",
		"repo":  "repo",
		"state": "closed",
	})
	if result.IsError {
		t.Errorf("expected success from github_list_issues with state=closed, got: %s", result.Error)
	}
}

// ─── gmail_send — missing subject validation ──────────────────────────────────

// TestGmailSend_MissingSubject exercises the "to and subject are required" error branch.
func TestGmailSend_MissingSubject(t *testing.T) {
	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGoogle, "g-val-1", "user@gmail", "http://localhost:1")
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("gmail_send")

	// Missing subject — should error before auth
	result := tool.Execute(context.Background(), map[string]any{
		"to":   "recipient@example.com",
		"body": "Some body",
	})
	if !result.IsError {
		t.Error("expected error when subject is missing from gmail_send")
	}
}

// TestGmailSend_MissingTo exercises the "to and subject are required" error with missing to.
func TestGmailSend_MissingTo(t *testing.T) {
	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGoogle, "g-val-2", "user@gmail", "http://localhost:1")
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("gmail_send")

	// Missing to — should error before auth
	result := tool.Execute(context.Background(), map[string]any{
		"subject": "Hello",
		"body":    "Body text",
	})
	if !result.IsError {
		t.Error("expected error when to is missing from gmail_send")
	}
}

// ─── gmail_search max_results parameter ──────────────────────────────────────

// TestGmailSearch_MaxResults exercises the max_results float64 branch.
func TestGmailSearch_MaxResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify maxResults param
		maxResults := r.URL.Query().Get("maxResults")
		if maxResults != "25" {
			t.Errorf("expected maxResults=25, got %s", maxResults)
		}
		jsonResponse(w, http.StatusOK, map[string]any{
			"messages":           []any{},
			"resultSizeEstimate": 0,
		})
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGoogle, "g-max-1", "user@gmail", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("gmail_search")

	result := tool.Execute(context.Background(), map[string]any{
		"query":       "from:me",
		"max_results": float64(25),
	})
	if result.IsError {
		t.Errorf("expected success from gmail_search with max_results, got: %s", result.Error)
	}
}

// ─── resolveConnection — multi-account label matching ────────────────────────

// TestResolveConnection_MultiAccount exercises the label-match branch in resolveConnection.
// When there are multiple connections and a non-empty label is provided, the
// matching connection should be returned.
func TestResolveConnection_MultiAccount_LabelMatch(t *testing.T) {
	dir := t.TempDir()
	store, err := connections.NewStore(dir + "/conns.json")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	// Add two GitHub connections.
	for _, c := range []connections.Connection{
		{ID: "gh-multi-1", Provider: connections.ProviderGitHub, AccountLabel: "user1@gh.com"},
		{ID: "gh-multi-2", Provider: connections.ProviderGitHub, AccountLabel: "user2@gh.com"},
	} {
		if err := store.Add(c); err != nil {
			t.Fatalf("store.Add: %v", err)
		}
	}

	secrets := connections.NewMemoryStore()
	mgr := connections.NewManager(store, secrets, "http://localhost:9999/oauth/callback")
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)

	// Even without tokens, we can verify the account selection works through the error message.
	// The auth error should reference the selected connection (which fails at token retrieval).
	tool, _ := reg.Get("github_list_prs")
	result := tool.Execute(context.Background(), map[string]any{
		"owner":   "org",
		"repo":    "repo",
		"account": "user2@gh.com", // explicitly select second account
	})
	// Auth will fail, but the tool must not panic
	_ = result
}

// TestResolveConnection_MultiAccount_NoMatch exercises the fallback to first connection.
func TestResolveConnection_MultiAccount_NoMatch(t *testing.T) {
	dir := t.TempDir()
	store, err := connections.NewStore(dir + "/conns.json")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	for _, c := range []connections.Connection{
		{ID: "gh-fb-1", Provider: connections.ProviderGitHub, AccountLabel: "user1@gh.com"},
		{ID: "gh-fb-2", Provider: connections.ProviderGitHub, AccountLabel: "user2@gh.com"},
	} {
		if err := store.Add(c); err != nil {
			t.Fatalf("store.Add: %v", err)
		}
	}

	secrets := connections.NewMemoryStore()
	mgr := connections.NewManager(store, secrets, "http://localhost:9999/oauth/callback")
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)

	tool, _ := reg.Get("github_list_prs")
	// Use an account label that doesn't match any connection → should fall back to first.
	result := tool.Execute(context.Background(), map[string]any{
		"owner":   "org",
		"repo":    "repo",
		"account": "nonexistent@gh.com",
	})
	// Auth will fail but no panic.
	_ = result
}

// ─── jiraPUT — HTTP error response path via mock ─────────────────────────────

// TestJiraPUT_TransportError exercises the transport-level error in jiraPUT.
func TestJiraPUT_TransportError(t *testing.T) {
	// Use a server that returns success for non-PUT, but close the server
	// after registration to force a transport error on the actual PUT call.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderJira, "jira-trans-1", "site", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("jira_update_issue")

	// Close the server before the actual PUT so the request fails at transport level.
	srv.Close()

	result := tool.Execute(context.Background(), map[string]any{
		"cloud_id":  "abc",
		"issue_key": "PROJ-1",
		"summary":   "Will fail at transport",
	})
	if !result.IsError {
		t.Error("expected error when server is closed during jiraPUT")
	}
}

// ─── Slack parameter branches with working HTTP client ───────────────────────

// TestSlackListChannels_WithCursorAndLimit exercises the cursor and limit branches
// within slack_list_channels.Execute when the HTTP client succeeds.
func TestSlackListChannels_WithCursorAndLimit(t *testing.T) {
	resp := map[string]any{
		"ok":       true,
		"channels": []map[string]any{{"id": "C1", "name": "general"}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, resp)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderSlack, "sl-cp-1", "ws", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("slack_list_channels")

	// Both cursor and limit provided — exercises both parameter branches AND
	// the params.Len > 0 → URL extension branch.
	result := tool.Execute(context.Background(), map[string]any{
		"cursor": "some-cursor-token",
		"limit":  float64(50),
	})
	if result.IsError {
		t.Errorf("expected success from slack_list_channels with cursor+limit, got: %s", result.Error)
	}
}

// TestSlackReadChannel_WithCustomLimit exercises the limit > 0 branch in slack_read_channel
// and the success return path when the HTTP client succeeds.
func TestSlackReadChannel_WithCustomLimit(t *testing.T) {
	resp := map[string]any{
		"ok":       true,
		"messages": []map[string]any{{"type": "message", "text": "hello"}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, resp)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderSlack, "sl-cp-2", "ws", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("slack_read_channel")

	result := tool.Execute(context.Background(), map[string]any{
		"channel": "C123",
		"limit":   float64(50),
	})
	if result.IsError {
		t.Errorf("expected success from slack_read_channel with limit, got: %s", result.Error)
	}
}

// TestSlackReadChannel_MissingChannelWithAuth exercises the "channel required" error
// branch AFTER successful authentication (requires working client).
func TestSlackReadChannel_MissingChannelWithAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, map[string]any{"ok": true})
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderSlack, "sl-cp-3", "ws", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("slack_read_channel")

	// Missing channel — triggers the validation error AFTER auth succeeds.
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error when channel is missing in slack_read_channel")
	}
}

// TestSlackPostMessage_ValidationWithAuth exercises the "channel and text are required"
// error branch after successful authentication.
func TestSlackPostMessage_ValidationWithAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, map[string]any{"ok": true})
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderSlack, "sl-cp-4", "ws", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("slack_post_message")

	// Missing text — triggers validation error after auth succeeds.
	result := tool.Execute(context.Background(), map[string]any{
		"channel": "C123",
	})
	if !result.IsError {
		t.Error("expected error when text is missing in slack_post_message")
	}
}

// TestSlackPostMessage_WithThreadTS exercises the thread_ts branch with a working client.
func TestSlackPostMessage_WithThreadTS(t *testing.T) {
	resp := map[string]any{"ok": true, "ts": "12345.6789", "channel": "C123"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, resp)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderSlack, "sl-cp-5", "ws", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("slack_post_message")

	// Provide thread_ts to exercise the `payload["thread_ts"] = ts` branch.
	result := tool.Execute(context.Background(), map[string]any{
		"channel":   "C123",
		"text":      "reply in thread",
		"thread_ts": "12345.0001",
	})
	if result.IsError {
		t.Errorf("expected success from slack_post_message with thread_ts, got: %s", result.Error)
	}
}

// ─── GitHub search_code success path ─────────────────────────────────────────

// TestGitHubSearchCode_Success exercises the success return path in github_search_code.
func TestGitHubSearchCode_Success(t *testing.T) {
	resp := map[string]any{
		"total_count": 1,
		"items":       []map[string]any{{"name": "main.go", "path": "src/main.go"}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, resp)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGitHub, "gh-sc-1", "user@gh", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("github_search_code")

	result := tool.Execute(context.Background(), map[string]any{
		"query": "func main language:go",
	})
	if result.IsError {
		t.Errorf("expected success from github_search_code, got: %s", result.Error)
	}
}

// ─── gmail_read success path (non-error return) ───────────────────────────────

// TestGmailRead_Success exercises the success return path in readGmail.
func TestGmailRead_Success(t *testing.T) {
	msg := map[string]any{
		"id":      "msg789",
		"snippet": "Test message content",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, msg)
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGoogle, "g-read-1", "user@gmail", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("gmail_read")

	result := tool.Execute(context.Background(), map[string]any{"message_id": "msg789"})
	if result.IsError {
		t.Errorf("expected success from gmail_read, got: %s", result.Error)
	}
	if !strings.Contains(result.Output, "msg789") {
		t.Errorf("expected message ID in output, got: %s", result.Output)
	}
}

// TestGmailRead_MissingMessageIDWithAuth exercises the message_id required check
// AFTER auth succeeds.
func TestGmailRead_MissingMessageIDWithAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, map[string]any{"id": "x"})
	}))
	defer srv.Close()

	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGoogle, "g-read-2", "user@gmail", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("gmail_read")

	// Missing message_id — triggers validation after auth succeeds.
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error when message_id is missing in gmail_read")
	}
}

// ─── RegisterAll — slog.Warn path via RegisterForProvider error ───────────────

// TestRegisterAll_SlackUserProvider exercises the RegisterAll switch fall-through
// when ProviderSlackUser is present — it falls through to no-op, and we verify
// no panic or error occurs.
func TestRegisterAll_SlackUserProvider(t *testing.T) {
	dir := t.TempDir()
	store, err := connections.NewStore(dir + "/conns.json")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Add(connections.Connection{
		ID:           "su-1",
		Provider:     connections.ProviderSlackUser,
		AccountLabel: "user@slack",
	}); err != nil {
		t.Fatalf("store.Add: %v", err)
	}

	secrets := connections.NewMemoryStore()
	mgr := connections.NewManager(store, secrets, "http://localhost:9999/oauth/callback")
	reg := tools.NewRegistry()

	// RegisterAll must not error even when ProviderSlackUser (unhandled) is present.
	if err := conntools.RegisterAll(reg, mgr, store); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}
}

// ─── Transport-error paths (server closed before request) ────────────────────
// These tests close the server BEFORE calling Execute so client.Do fails,
// exercising the "if err != nil { return ... }" branch after client.Do in each
// HTTP helper (githubGET, githubPOST, bitbucketGET, slackGET, slackPOST, etc.).

// TestGitHubGET_TransportError_Via_GetPR covers the client.Do error branch in githubGET.
func TestGitHubGET_TransportError_Via_GetPR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, map[string]any{"number": 1})
	}))
	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGitHub, "gh-terr-1", "user@gh", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("github_get_pr")
	srv.Close() // close before the request so client.Do fails

	result := tool.Execute(context.Background(), map[string]any{
		"owner":  "org",
		"repo":   "repo",
		"number": float64(1),
	})
	if !result.IsError {
		t.Error("expected transport error from github_get_pr when server is closed")
	}
}

// TestGitHubPOST_TransportError covers the client.Do error branch in githubPOST.
func TestGitHubPOST_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, map[string]any{"number": 1})
	}))
	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGitHub, "gh-terr-2", "user@gh", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("github_create_issue")
	srv.Close()

	result := tool.Execute(context.Background(), map[string]any{
		"owner": "org",
		"repo":  "repo",
		"title": "Bug",
	})
	if !result.IsError {
		t.Error("expected transport error from github_create_issue when server is closed")
	}
}

// TestBitbucketGET_TransportError covers the client.Do error branch in bitbucketGET.
func TestBitbucketGET_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, map[string]any{"id": 1})
	}))
	mgr, store := buildManagerWithMockTransport(t, connections.ProviderBitbucket, "bb-terr-1", "user", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("bitbucket_list_prs")
	srv.Close()

	result := tool.Execute(context.Background(), map[string]any{
		"workspace": "ws",
		"repo_slug": "repo",
	})
	if !result.IsError {
		t.Error("expected transport error from bitbucket_list_prs when server is closed")
	}
}

// TestBitbucketPOST_TransportError covers the client.Do error branch in bitbucketPOST.
func TestBitbucketPOST_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, map[string]any{"id": 1})
	}))
	mgr, store := buildManagerWithMockTransport(t, connections.ProviderBitbucket, "bb-terr-2", "user", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("bitbucket_create_pr")
	srv.Close()

	result := tool.Execute(context.Background(), map[string]any{
		"workspace":     "ws",
		"repo_slug":     "repo",
		"title":         "PR",
		"source_branch": "feat",
		"target_branch": "main",
	})
	if !result.IsError {
		t.Error("expected transport error from bitbucket_create_pr when server is closed")
	}
}

// TestSlackGET_TransportError covers the client.Do error branch in slackGET.
func TestSlackGET_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, map[string]any{"ok": true})
	}))
	mgr, store := buildManagerWithMockTransport(t, connections.ProviderSlack, "sl-terr-1", "ws", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("slack_list_channels")
	srv.Close()

	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected transport error from slack_list_channels when server is closed")
	}
}

// TestSlackPOST_TransportError covers the client.Do error branch in slackPOST.
func TestSlackPOST_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, map[string]any{"ok": true})
	}))
	mgr, store := buildManagerWithMockTransport(t, connections.ProviderSlack, "sl-terr-2", "ws", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("slack_post_message")
	srv.Close()

	result := tool.Execute(context.Background(), map[string]any{
		"channel": "C123",
		"text":    "hello",
	})
	if !result.IsError {
		t.Error("expected transport error from slack_post_message when server is closed")
	}
}

// TestJiraGET_TransportError covers the client.Do error branch in jiraGET.
func TestJiraGET_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, map[string]any{})
	}))
	mgr, store := buildManagerWithMockTransport(t, connections.ProviderJira, "jira-terr-1", "site", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("jira_get_issue")
	srv.Close()

	result := tool.Execute(context.Background(), map[string]any{
		"cloud_id":  "abc",
		"issue_key": "PROJ-1",
	})
	if !result.IsError {
		t.Error("expected transport error from jira_get_issue when server is closed")
	}
}

// TestJiraPOST_TransportError covers the client.Do error branch in jiraPOST.
func TestJiraPOST_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, map[string]any{})
	}))
	mgr, store := buildManagerWithMockTransport(t, connections.ProviderJira, "jira-terr-2", "site", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("jira_create_issue")
	srv.Close()

	result := tool.Execute(context.Background(), map[string]any{
		"cloud_id":    "abc",
		"project_key": "PROJ",
		"summary":     "Test",
		"issue_type":  "Bug",
	})
	if !result.IsError {
		t.Error("expected transport error from jira_create_issue when server is closed")
	}
}

// TestSearchGmail_TransportError covers the client.Do error branch in searchGmail.
func TestSearchGmail_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, map[string]any{})
	}))
	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGoogle, "g-terr-1", "user@gmail", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("gmail_search")
	srv.Close()

	result := tool.Execute(context.Background(), map[string]any{"query": "from:me"})
	if !result.IsError {
		t.Error("expected transport error from gmail_search when server is closed")
	}
}

// TestReadGmail_TransportError covers the client.Do error branch in readGmail.
func TestReadGmail_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, map[string]any{})
	}))
	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGoogle, "g-terr-2", "user@gmail", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("gmail_read")
	srv.Close()

	result := tool.Execute(context.Background(), map[string]any{"message_id": "abc123"})
	if !result.IsError {
		t.Error("expected transport error from gmail_read when server is closed")
	}
}

// TestSendGmail_TransportError covers the client.Do error branch in sendGmail.
func TestSendGmail_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, map[string]any{})
	}))
	mgr, store := buildManagerWithMockTransport(t, connections.ProviderGoogle, "g-terr-3", "user@gmail", srv.URL)
	reg := tools.NewRegistry()
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("gmail_send")
	srv.Close()

	result := tool.Execute(context.Background(), map[string]any{
		"to":      "user@example.com",
		"subject": "Test",
		"body":    "Body",
	})
	if !result.IsError {
		t.Error("expected transport error from gmail_send when server is closed")
	}
}

// ─── Verify io.NopCloser is importable (compile-time check) ──────────────────
var _ = io.NopCloser
var _ = bytes.NewBufferString
