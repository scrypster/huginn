package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- names / permissions ---

func TestBitbucketTools_Names(t *testing.T) {
	toolset := []Tool{
		&BitbucketPRCreateTool{},
		&BitbucketPRViewTool{},
		&BitbucketPRChecksTool{},
		&BitbucketPRCommentTool{},
		&BitbucketPRMergeTool{},
	}
	wantNames := []string{
		"bitbucket_pr_create", "bitbucket_pr_view", "bitbucket_pr_checks",
		"bitbucket_pr_comment", "bitbucket_pr_merge",
	}
	for i, tool := range toolset {
		if tool.Name() != wantNames[i] {
			t.Errorf("tool[%d].Name() = %q, want %q", i, tool.Name(), wantNames[i])
		}
	}
}

func TestBitbucketTools_Permissions(t *testing.T) {
	reads := []Tool{&BitbucketPRViewTool{}, &BitbucketPRChecksTool{}}
	for _, tool := range reads {
		if tool.Permission() != PermRead {
			t.Errorf("%s should be PermRead", tool.Name())
		}
	}
	writes := []Tool{&BitbucketPRCreateTool{}, &BitbucketPRCommentTool{}, &BitbucketPRMergeTool{}}
	for _, tool := range writes {
		if tool.Permission() != PermWrite {
			t.Errorf("%s should be PermWrite", tool.Name())
		}
	}
}

func TestBitbucketToolNames_MatchesRegistered(t *testing.T) {
	reg := NewRegistry()
	RegisterBitbucketTools(reg, "/tmp", nil)
	names := BitbucketToolNames()
	if len(names) != len(reg.All()) {
		t.Fatalf("BitbucketToolNames() has %d entries, registry has %d tools", len(names), len(reg.All()))
	}
	for _, n := range names {
		if _, ok := reg.Get(n); !ok {
			t.Errorf("BitbucketToolNames() lists %q but it is not registered", n)
		}
	}
}

// --- remote URL parsing ---

func TestParseBitbucketRemote_SSH(t *testing.T) {
	ws, repo, err := parseBitbucketRemote("git@bitbucket.org:scrypster/huginn.git")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws != "scrypster" || repo != "huginn" {
		t.Errorf("got (%q, %q), want (scrypster, huginn)", ws, repo)
	}
}

func TestParseBitbucketRemote_HTTPS(t *testing.T) {
	ws, repo, err := parseBitbucketRemote("https://bitbucket.org/scrypster/huginn.git")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws != "scrypster" || repo != "huginn" {
		t.Errorf("got (%q, %q), want (scrypster, huginn)", ws, repo)
	}
}

func TestParseBitbucketRemote_HTTPSNoGitSuffix(t *testing.T) {
	ws, repo, err := parseBitbucketRemote("https://bitbucket.org/scrypster/huginn")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws != "scrypster" || repo != "huginn" {
		t.Errorf("got (%q, %q), want (scrypster, huginn)", ws, repo)
	}
}

func TestParseBitbucketRemote_HTTPSWithAuth(t *testing.T) {
	ws, repo, err := parseBitbucketRemote("https://user@bitbucket.org/scrypster/huginn.git")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws != "scrypster" || repo != "huginn" {
		t.Errorf("got (%q, %q), want (scrypster, huginn)", ws, repo)
	}
}

func TestParseBitbucketRemote_NonBitbucket(t *testing.T) {
	_, _, err := parseBitbucketRemote("git@github.com:scrypster/huginn.git")
	if err == nil {
		t.Fatal("expected error for a non-Bitbucket remote")
	}
	if !strings.Contains(err.Error(), "not a Bitbucket Cloud URL") {
		t.Errorf("error should explain the remote isn't Bitbucket, got %q", err.Error())
	}
}

// --- checks classification ---

func TestClassifyBitbucketStatuses(t *testing.T) {
	cases := []struct {
		name   string
		states []string
		want   string
	}{
		{"empty", nil, ""},
		{"all successful", []string{"SUCCESSFUL", "SUCCESSFUL"}, "passed"},
		{"one in progress", []string{"SUCCESSFUL", "INPROGRESS"}, "pending"},
		{"one failed", []string{"SUCCESSFUL", "FAILED"}, "failed"},
		{"one stopped", []string{"STOPPED"}, "failed"},
		{"failed beats pending", []string{"INPROGRESS", "FAILED"}, "failed"},
		{"lowercase state", []string{"successful"}, "passed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var statuses []bitbucketStatus
			for _, s := range tc.states {
				statuses = append(statuses, bitbucketStatus{State: s})
			}
			if got := classifyBitbucketStatuses(statuses); got != tc.want {
				t.Errorf("classifyBitbucketStatuses(%v) = %q, want %q", tc.states, got, tc.want)
			}
		})
	}
}

// --- missing-arg error paths ---

func TestBitbucketPRCreateTool_MissingTitle(t *testing.T) {
	tool := &BitbucketPRCreateTool{}
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Fatal("expected error for missing title")
	}
	if !strings.Contains(result.Error, "title") {
		t.Errorf("error should mention 'title', got %q", result.Error)
	}
}

func TestBitbucketPRViewTool_MissingNumber(t *testing.T) {
	tool := &BitbucketPRViewTool{}
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Fatal("expected error for missing number")
	}
	if !strings.Contains(result.Error, "number") {
		t.Errorf("error should mention 'number', got %q", result.Error)
	}
}

func TestBitbucketPRChecksTool_MissingNumber(t *testing.T) {
	tool := &BitbucketPRChecksTool{}
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Fatal("expected error for missing number")
	}
}

func TestBitbucketPRCommentTool_MissingBody(t *testing.T) {
	tool := &BitbucketPRCommentTool{}
	result := tool.Execute(context.Background(), map[string]any{"number": 1})
	if !result.IsError {
		t.Fatal("expected error for missing body")
	}
	if !strings.Contains(result.Error, "body") {
		t.Errorf("error should mention 'body', got %q", result.Error)
	}
}

func TestBitbucketPRMergeTool_MissingNumber(t *testing.T) {
	tool := &BitbucketPRMergeTool{}
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Fatal("expected error for missing number")
	}
}

// --- missing-token error path ---

func TestBitbucketPRCreateTool_NoAuthAvailable(t *testing.T) {
	tool := &BitbucketPRCreateTool{bbBase: bbBase{
		remoteFunc: func(context.Context) (string, string, error) { return "ws", "repo", nil },
	}}
	result := tool.Execute(context.Background(), map[string]any{
		"title":              "x",
		"destination_branch": "main",
	})
	if !result.IsError {
		t.Fatal("expected error when no connection and no BITBUCKET_ACCESS_TOKEN are available")
	}
	if !strings.Contains(result.Error, "connect Bitbucket") {
		t.Errorf("error should be actionable ('connect Bitbucket in Settings...'), got %q", result.Error)
	}
}

func TestBitbucketPRViewTool_BadRemote(t *testing.T) {
	tool := &BitbucketPRViewTool{bbBase: bbBase{
		remoteFunc: func(context.Context) (string, string, error) {
			return "", "", errNotBitbucketRemote
		},
	}}
	result := tool.Execute(context.Background(), map[string]any{"number": 1})
	if !result.IsError {
		t.Fatal("expected error for a non-Bitbucket remote")
	}
}

// errNotBitbucketRemote is a stand-in error for tests exercising the "remote
// isn't Bitbucket" path without going through the real git-remote resolution.
var errNotBitbucketRemote = &remoteErr{"remote is not bitbucket.org"}

type remoteErr struct{ msg string }

func (e *remoteErr) Error() string { return e.msg }

// --- httptest-backed happy paths ---

// newTestClientFunc returns a BitbucketClientFunc that always hands back an
// http.Client pointed at nothing special (the test server's URL is baked
// into apiBase, not the client) — auth is irrelevant here since the fake
// server doesn't check it.
func newTestClientFunc() BitbucketClientFunc {
	return func(context.Context) (*http.Client, error) {
		return http.DefaultClient, nil
	}
}

func testRemoteFunc() func(context.Context) (string, string, error) {
	return func(context.Context) (string, string, error) { return "myws", "myrepo", nil }
}

func TestBitbucketPRCreateTool_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repositories/myws/myrepo/pullrequests" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["title"] != "Add feature" {
			t.Errorf("title = %v, want %q", body["title"], "Add feature")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":    42,
			"state": "OPEN",
			"links": map[string]any{
				"html": map[string]any{"href": "https://bitbucket.org/myws/myrepo/pull-requests/42"},
			},
		})
	}))
	defer srv.Close()

	tool := &BitbucketPRCreateTool{
		bbBase: bbBase{
			apiBase:    srv.URL,
			ClientFunc: newTestClientFunc(),
			remoteFunc: testRemoteFunc(),
		},
		DefaultBranch: "main",
	}
	result := tool.Execute(context.Background(), map[string]any{
		"title":         "Add feature",
		"source_branch": "feature/x",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Metadata == nil {
		t.Fatal("expected Metadata to carry the parsed URL")
	}
	if url, _ := result.Metadata["url"].(string); url != "https://bitbucket.org/myws/myrepo/pull-requests/42" {
		t.Errorf("Metadata[url] = %q, want the PR URL", url)
	}
	if num, _ := result.Metadata["number"].(string); num != "42" {
		t.Errorf("Metadata[number] = %q, want %q", num, "42")
	}
}

func TestBitbucketPRCreateTool_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "branch not found"},
		})
	}))
	defer srv.Close()

	tool := &BitbucketPRCreateTool{bbBase: bbBase{
		apiBase:    srv.URL,
		ClientFunc: newTestClientFunc(),
		remoteFunc: testRemoteFunc(),
	}}
	result := tool.Execute(context.Background(), map[string]any{
		"title":              "x",
		"source_branch":      "feature/x",
		"destination_branch": "main",
	})
	if !result.IsError {
		t.Fatal("expected error for a 400 response")
	}
	if !strings.Contains(result.Error, "branch not found") {
		t.Errorf("error should surface the API's message, got %q", result.Error)
	}
}

func TestBitbucketPRViewTool_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repositories/myws/myrepo/pullrequests/7" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": 7, "title": "Fix bug", "state": "OPEN"})
	}))
	defer srv.Close()

	tool := &BitbucketPRViewTool{bbBase: bbBase{
		apiBase:    srv.URL,
		ClientFunc: newTestClientFunc(),
		remoteFunc: testRemoteFunc(),
	}}
	result := tool.Execute(context.Background(), map[string]any{"number": 7})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "Fix bug") {
		t.Errorf("Output should contain the PR title, got %q", result.Output)
	}
}

func TestBitbucketPRChecksTool_Passed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/pullrequests/9"):
			json.NewEncoder(w).Encode(map[string]any{
				"id": 9, "state": "OPEN",
				"source": map[string]any{"commit": map[string]any{"hash": "abc123"}},
			})
		case strings.Contains(r.URL.Path, "/commit/abc123/statuses"):
			json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{{"state": "SUCCESSFUL", "key": "build"}},
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	tool := &BitbucketPRChecksTool{bbBase: bbBase{
		apiBase:    srv.URL,
		ClientFunc: newTestClientFunc(),
		remoteFunc: testRemoteFunc(),
	}}
	result := tool.Execute(context.Background(), map[string]any{"number": 9})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if status, _ := result.Metadata["status"].(string); status != "passed" {
		t.Errorf("Metadata[status] = %q, want %q", status, "passed")
	}
}

func TestBitbucketPRChecksTool_Pending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/pullrequests/9"):
			json.NewEncoder(w).Encode(map[string]any{
				"id": 9, "source": map[string]any{"commit": map[string]any{"hash": "abc123"}},
			})
		case strings.Contains(r.URL.Path, "/statuses"):
			json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{{"state": "INPROGRESS"}},
			})
		}
	}))
	defer srv.Close()

	tool := &BitbucketPRChecksTool{bbBase: bbBase{
		apiBase:    srv.URL,
		ClientFunc: newTestClientFunc(),
		remoteFunc: testRemoteFunc(),
	}}
	result := tool.Execute(context.Background(), map[string]any{"number": 9})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if status, _ := result.Metadata["status"].(string); status != "pending" {
		t.Errorf("Metadata[status] = %q, want %q", status, "pending")
	}
}

func TestBitbucketPRChecksTool_Failed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/pullrequests/9"):
			json.NewEncoder(w).Encode(map[string]any{
				"id": 9, "source": map[string]any{"commit": map[string]any{"hash": "abc123"}},
			})
		case strings.Contains(r.URL.Path, "/statuses"):
			json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{{"state": "FAILED"}},
			})
		}
	}))
	defer srv.Close()

	tool := &BitbucketPRChecksTool{bbBase: bbBase{
		apiBase:    srv.URL,
		ClientFunc: newTestClientFunc(),
		remoteFunc: testRemoteFunc(),
	}}
	result := tool.Execute(context.Background(), map[string]any{"number": 9})
	if !result.IsError {
		t.Fatal("expected IsError for a failed check")
	}
	if status, _ := result.Metadata["status"].(string); status != "failed" {
		t.Errorf("Metadata[status] = %q, want %q", status, "failed")
	}
}

func TestBitbucketPRCommentTool_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repositories/myws/myrepo/pullrequests/3/comments" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		content, _ := body["content"].(map[string]any)
		if content["raw"] != "nice work" {
			t.Errorf("comment body = %v, want %q", content["raw"], "nice work")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": 1})
	}))
	defer srv.Close()

	tool := &BitbucketPRCommentTool{bbBase: bbBase{
		apiBase:    srv.URL,
		ClientFunc: newTestClientFunc(),
		remoteFunc: testRemoteFunc(),
	}}
	result := tool.Execute(context.Background(), map[string]any{"number": 3, "body": "nice work"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
}

func TestBitbucketPRMergeTool_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repositories/myws/myrepo/pullrequests/5/merge" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": 5, "state": "MERGED",
			"links": map[string]any{
				"html": map[string]any{"href": "https://bitbucket.org/myws/myrepo/pull-requests/5"},
			},
		})
	}))
	defer srv.Close()

	tool := &BitbucketPRMergeTool{bbBase: bbBase{
		apiBase:    srv.URL,
		ClientFunc: newTestClientFunc(),
		remoteFunc: testRemoteFunc(),
	}}
	result := tool.Execute(context.Background(), map[string]any{"number": 5, "merge_strategy": "squash"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if num, _ := result.Metadata["number"].(string); num != "5" {
		t.Errorf("Metadata[number] = %q, want %q", num, "5")
	}
}

// --- BITBUCKET_ACCESS_TOKEN env fallback ---

func TestBitbucketPRViewTool_UsesEnvTokenFallback(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": 1})
	}))
	defer srv.Close()

	t.Setenv("BITBUCKET_ACCESS_TOKEN", "env-token-123")
	tool := &BitbucketPRViewTool{bbBase: bbBase{
		apiBase:    srv.URL,
		remoteFunc: testRemoteFunc(),
		// ClientFunc left nil: forces the env-token fallback path.
	}}
	result := tool.Execute(context.Background(), map[string]any{"number": 1})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if gotAuth != "Bearer env-token-123" {
		t.Errorf("Authorization header = %q, want Bearer env-token-123", gotAuth)
	}
}
