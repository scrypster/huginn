package conntools_test

// hardening_iter1_test.go — coverage improvement for connections/tools.
// Tests schema, name, description, permission, and auth-error paths without
// making real API calls.

import (
	"context"
	"testing"

	"golang.org/x/oauth2"

	"github.com/scrypster/huginn/internal/connections"
	conntools "github.com/scrypster/huginn/internal/connections/tools"
	"github.com/scrypster/huginn/internal/tools"
)

// newConnectedStore creates a store with a connection for the given provider.
func newConnectedStore(t *testing.T, provider connections.Provider, id, label string) (*connections.Store, *connections.Manager) {
	t.Helper()
	dir := t.TempDir()
	store, err := connections.NewStore(dir + "/conns.json")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Add(connections.Connection{
		ID:           id,
		Provider:     provider,
		AccountLabel: label,
	}); err != nil {
		t.Fatalf("store.Add: %v", err)
	}
	secrets := connections.NewMemoryStore()
	mgr := connections.NewManager(store, secrets, "http://localhost:9999/oauth/callback")
	return store, mgr
}

// newConnectedStoreWithToken creates a store + manager + token. We need to pass
// secrets separately since Manager doesn't expose it.
func newConnectedStoreWithToken(t *testing.T, provider connections.Provider, id, label string) (*connections.Store, *connections.Manager, connections.SecretStore) {
	t.Helper()
	// We need access to the secrets store to add a token.
	dir := t.TempDir()
	storeFile := dir + "/conns.json"
	store2, _ := connections.NewStore(storeFile)
	store2.Add(connections.Connection{
		ID:           id,
		Provider:     provider,
		AccountLabel: label,
	})

	secrets := connections.NewMemoryStore()
	mgr2 := connections.NewManager(store2, secrets, "http://localhost:9999/oauth/callback")

	// Store a dummy token so GetHTTPClient doesn't fail at the token fetch stage
	tok := &oauth2.Token{
		AccessToken: "dummy-access-token",
		TokenType:   "Bearer",
	}
	if err := secrets.StoreToken(id, tok); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}

	return store2, mgr2, secrets
}

// ─── resolveConnection (via RegisterForProvider + Execute auth-error path) ───

// verifySchemaFields checks that a schema has required fields set.
func verifySchemaFields(t *testing.T, sch interface{ Name() string; Schema() any }, wantName string) {
	t.Helper()
	if sch.Name() != wantName {
		t.Errorf("expected Name()=%q got %q", wantName, sch.Name())
	}
}

// ─── GitHub tools ─────────────────────────────────────────────────────────────

func TestGitHubTools_SchemaAndMeta(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderGitHub, "gh-1", "user@gh.com")
	if err := conntools.RegisterAll(reg, mgr, store); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	toolNames := []string{
		"github_list_prs",
		"github_get_pr",
		"github_create_issue",
		"github_search_code",
		"github_list_issues",
	}
	for _, name := range toolNames {
		tool, ok := reg.Get(name)
		if !ok {
			t.Errorf("expected tool %q to be registered", name)
			continue
		}
		if tool.Name() != name {
			t.Errorf("%q: Name() = %q", name, tool.Name())
		}
		if tool.Description() == "" {
			t.Errorf("%q: Description() is empty", name)
		}
		schema := tool.Schema()
		if schema.Function.Name != name {
			t.Errorf("%q: schema function name = %q", name, schema.Function.Name)
		}
	}
}

func TestGitHubTools_ExecuteAuthError(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderGitHub, "gh-bad", "user@gh.com")
	if err := conntools.RegisterAll(reg, mgr, store); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	tool, _ := reg.Get("github_list_prs")
	result := tool.Execute(context.Background(), map[string]any{
		"owner": "myorg",
		"repo":  "myrepo",
	})
	// Auth will fail because there are no real tokens.
	if !result.IsError {
		t.Error("expected error when auth token is not available")
	}
}

func TestGitHubTools_GetPR_AuthError(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderGitHub, "gh-bad", "user@gh.com")
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("github_get_pr")
	result := tool.Execute(context.Background(), map[string]any{
		"owner": "org", "repo": "repo", "number": float64(1),
	})
	if !result.IsError {
		t.Error("expected auth error for github_get_pr")
	}
}

func TestGitHubTools_CreateIssue_AuthError(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderGitHub, "gh-bad", "user@gh.com")
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("github_create_issue")
	result := tool.Execute(context.Background(), map[string]any{
		"owner": "org", "repo": "repo", "title": "bug",
	})
	if !result.IsError {
		t.Error("expected auth error for github_create_issue")
	}
}

func TestGitHubTools_SearchCode_AuthError(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderGitHub, "gh-bad", "user@gh.com")
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("github_search_code")
	result := tool.Execute(context.Background(), map[string]any{
		"query": "foo",
	})
	if !result.IsError {
		t.Error("expected auth error for github_search_code")
	}
}

func TestGitHubTools_ListIssues_AuthError(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderGitHub, "gh-bad", "user@gh.com")
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("github_list_issues")
	result := tool.Execute(context.Background(), map[string]any{
		"owner": "org", "repo": "repo",
	})
	if !result.IsError {
		t.Error("expected auth error for github_list_issues")
	}
}

// ─── Slack tools ──────────────────────────────────────────────────────────────

func TestSlackTools_SchemaAndMeta(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderSlack, "sl-1", "workspace")
	if err := conntools.RegisterAll(reg, mgr, store); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	for _, name := range []string{"slack_list_channels", "slack_read_channel", "slack_post_message"} {
		tool, ok := reg.Get(name)
		if !ok {
			t.Errorf("expected %q to be registered", name)
			continue
		}
		if tool.Name() != name {
			t.Errorf("%q: Name()=%q", name, tool.Name())
		}
		if tool.Description() == "" {
			t.Errorf("%q: Description() is empty", name)
		}
		schema := tool.Schema()
		if schema.Function.Name != name {
			t.Errorf("%q: schema function name=%q", name, schema.Function.Name)
		}
	}
}

func TestSlackTools_ListChannels_AuthError(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderSlack, "sl-bad", "ws")
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("slack_list_channels")
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected auth error for slack_list_channels")
	}
}

func TestSlackTools_ReadChannel_AuthError(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderSlack, "sl-bad", "ws")
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("slack_read_channel")
	result := tool.Execute(context.Background(), map[string]any{"channel": "C123"})
	if !result.IsError {
		t.Error("expected auth error for slack_read_channel")
	}
}

func TestSlackTools_PostMessage_AuthError(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderSlack, "sl-bad", "ws")
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("slack_post_message")
	result := tool.Execute(context.Background(), map[string]any{
		"channel": "C123", "text": "hello",
	})
	if !result.IsError {
		t.Error("expected auth error for slack_post_message")
	}
}

func TestSlackTools_ReadChannel_MissingChannel(t *testing.T) {
	// Even before auth fails, missing channel should return an error.
	// We test this via the auth path — any error (auth or validation) is acceptable.
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderSlack, "sl-bad", "ws")
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("slack_read_channel")
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for slack_read_channel with no channel")
	}
}

// ─── Jira tools ───────────────────────────────────────────────────────────────

func TestJiraTools_SchemaAndMeta(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderJira, "jira-1", "mysite")
	if err := conntools.RegisterAll(reg, mgr, store); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	for _, name := range []string{"jira_list_issues", "jira_get_issue", "jira_create_issue", "jira_update_issue"} {
		tool, ok := reg.Get(name)
		if !ok {
			t.Errorf("expected %q to be registered", name)
			continue
		}
		if tool.Name() != name {
			t.Errorf("%q: Name()=%q", name, tool.Name())
		}
		if tool.Description() == "" {
			t.Errorf("%q: Description() is empty", name)
		}
	}
}

func TestJiraTools_ListIssues_AuthError(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderJira, "jira-bad", "site")
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("jira_list_issues")
	result := tool.Execute(context.Background(), map[string]any{
		"project": "PROJ", "site_url": "https://example.atlassian.net",
	})
	if !result.IsError {
		t.Error("expected auth error for jira_list_issues")
	}
}

func TestJiraTools_GetIssue_AuthError(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderJira, "jira-bad", "site")
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("jira_get_issue")
	result := tool.Execute(context.Background(), map[string]any{
		"issue_key": "PROJ-1", "site_url": "https://example.atlassian.net",
	})
	if !result.IsError {
		t.Error("expected auth error for jira_get_issue")
	}
}

// ─── Bitbucket tools ──────────────────────────────────────────────────────────

func TestBitbucketTools_SchemaAndMeta(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderBitbucket, "bb-1", "myuser")
	if err := conntools.RegisterAll(reg, mgr, store); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	for _, name := range []string{"bitbucket_list_prs", "bitbucket_get_pr", "bitbucket_create_pr"} {
		tool, ok := reg.Get(name)
		if !ok {
			t.Errorf("expected %q to be registered", name)
			continue
		}
		if tool.Name() != name {
			t.Errorf("%q: Name()=%q", name, tool.Name())
		}
		if tool.Description() == "" {
			t.Errorf("%q: Description() is empty", name)
		}
	}
}

func TestBitbucketTools_ListPRs_AuthError(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderBitbucket, "bb-bad", "user")
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("bitbucket_list_prs")
	result := tool.Execute(context.Background(), map[string]any{
		"workspace": "myws", "repo_slug": "myrepo",
	})
	if !result.IsError {
		t.Error("expected auth error for bitbucket_list_prs")
	}
}

func TestBitbucketTools_GetPR_AuthError(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderBitbucket, "bb-bad", "user")
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("bitbucket_get_pr")
	result := tool.Execute(context.Background(), map[string]any{
		"workspace": "myws", "repo_slug": "myrepo", "pr_id": float64(1),
	})
	if !result.IsError {
		t.Error("expected auth error for bitbucket_get_pr")
	}
}

// ─── Gmail tools ──────────────────────────────────────────────────────────────

func TestGmailTools_SchemaAndMeta(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderGoogle, "g-1", "user@gmail.com")
	if err := conntools.RegisterAll(reg, mgr, store); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	for _, name := range []string{"gmail_search", "gmail_read", "gmail_send"} {
		tool, ok := reg.Get(name)
		if !ok {
			t.Errorf("expected %q to be registered", name)
			continue
		}
		if tool.Name() != name {
			t.Errorf("%q: Name()=%q", name, tool.Name())
		}
		if tool.Description() == "" {
			t.Errorf("%q: Description() is empty", name)
		}
	}
}

func TestGmailTools_Search_AuthError(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderGoogle, "g-bad", "u@gmail.com")
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("gmail_search")
	result := tool.Execute(context.Background(), map[string]any{"query": "from:me"})
	if !result.IsError {
		t.Error("expected auth error for gmail_search")
	}
}

func TestGmailTools_Read_AuthError(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderGoogle, "g-bad", "u@gmail.com")
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("gmail_read")
	result := tool.Execute(context.Background(), map[string]any{"message_id": "abc123"})
	if !result.IsError {
		t.Error("expected auth error for gmail_read")
	}
}

func TestGmailTools_Send_AuthError(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderGoogle, "g-bad", "u@gmail.com")
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("gmail_send")
	result := tool.Execute(context.Background(), map[string]any{
		"to": "bob@example.com", "subject": "hi", "body": "hello",
	})
	if !result.IsError {
		t.Error("expected auth error for gmail_send")
	}
}

// ─── RegisterForProvider — unknown provider is a no-op ───────────────────────

func TestRegisterForProvider_UnknownProvider_NoOp(t *testing.T) {
	reg := tools.NewRegistry()
	_, mgr := newConnectedStore(t, connections.ProviderSlack, "sl-1", "ws")
	err := conntools.RegisterForProvider(reg, mgr, "unknown_provider", []connections.Connection{
		{ID: "x-1", Provider: "unknown_provider"},
	})
	if err != nil {
		t.Errorf("unexpected error for unknown provider: %v", err)
	}
}
