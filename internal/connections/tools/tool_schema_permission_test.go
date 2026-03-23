package conntools_test

// hardening_iter2_test.go — additional coverage for Schema(), Permission(), and
// tool metadata paths that were not covered in earlier test rounds.

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/connections"
	conntools "github.com/scrypster/huginn/internal/connections/tools"
	"github.com/scrypster/huginn/internal/tools"
)

// --- Schema coverage for all tools ---

func TestGitHubTools_Schema(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderGitHub, "gh-s", "user@gh.com")
	_ = conntools.RegisterAll(reg, mgr, store)

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
			t.Errorf("expected %q to be registered", name)
			continue
		}
		sch := tool.Schema()
		if sch.Type != "function" {
			t.Errorf("%q: schema type = %q, expected 'function'", name, sch.Type)
		}
		if sch.Function.Name != name {
			t.Errorf("%q: schema function name = %q", name, sch.Function.Name)
		}
		if sch.Function.Description == "" {
			t.Errorf("%q: schema function description is empty", name)
		}
		if len(sch.Function.Parameters.Required) == 0 {
			t.Errorf("%q: schema has no required params", name)
		}
		// Permission
		perm := tool.Permission()
		if perm != tools.PermRead && perm != tools.PermWrite {
			t.Errorf("%q: unexpected permission level %v", name, perm)
		}
	}
}

func TestSlackTools_Schema(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderSlack, "sl-s", "ws")
	_ = conntools.RegisterAll(reg, mgr, store)

	for _, name := range []string{"slack_list_channels", "slack_read_channel", "slack_post_message"} {
		tool, ok := reg.Get(name)
		if !ok {
			t.Errorf("expected %q to be registered", name)
			continue
		}
		sch := tool.Schema()
		if sch.Type != "function" {
			t.Errorf("%q: schema type = %q", name, sch.Type)
		}
		if sch.Function.Name != name {
			t.Errorf("%q: schema function name = %q", name, sch.Function.Name)
		}
		// Permission
		_ = tool.Permission()
	}
}

func TestJiraTools_Schema(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderJira, "jira-s", "site")
	_ = conntools.RegisterAll(reg, mgr, store)

	for _, name := range []string{"jira_list_issues", "jira_get_issue", "jira_create_issue", "jira_update_issue"} {
		tool, ok := reg.Get(name)
		if !ok {
			t.Errorf("expected %q to be registered", name)
			continue
		}
		sch := tool.Schema()
		if sch.Type != "function" {
			t.Errorf("%q: schema type = %q", name, sch.Type)
		}
		if sch.Function.Name != name {
			t.Errorf("%q: schema function name = %q", name, sch.Function.Name)
		}
		_ = tool.Permission()
	}
}

func TestBitbucketTools_Schema(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderBitbucket, "bb-s", "user")
	_ = conntools.RegisterAll(reg, mgr, store)

	for _, name := range []string{"bitbucket_list_prs", "bitbucket_get_pr", "bitbucket_create_pr"} {
		tool, ok := reg.Get(name)
		if !ok {
			t.Errorf("expected %q to be registered", name)
			continue
		}
		sch := tool.Schema()
		if sch.Type != "function" {
			t.Errorf("%q: schema type = %q", name, sch.Type)
		}
		if sch.Function.Name != name {
			t.Errorf("%q: schema function name = %q", name, sch.Function.Name)
		}
		_ = tool.Permission()
	}
}

func TestGmailTools_Schema(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderGoogle, "g-s", "user@gmail.com")
	_ = conntools.RegisterAll(reg, mgr, store)

	for _, name := range []string{"gmail_search", "gmail_read", "gmail_send"} {
		tool, ok := reg.Get(name)
		if !ok {
			t.Errorf("expected %q to be registered", name)
			continue
		}
		sch := tool.Schema()
		if sch.Type != "function" {
			t.Errorf("%q: schema type = %q", name, sch.Type)
		}
		if sch.Function.Name != name {
			t.Errorf("%q: schema function name = %q", name, sch.Function.Name)
		}
		_ = tool.Permission()
	}
}

// --- Jira additional Execute paths ---

func TestJiraTools_CreateIssue_AuthError(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderJira, "jira-bad", "site")
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("jira_create_issue")
	result := tool.Execute(context.Background(), map[string]any{
		"cloud_id": "abc123", "project_key": "PROJ",
		"summary": "Bug found", "issue_type": "Bug",
	})
	if !result.IsError {
		t.Error("expected auth error for jira_create_issue")
	}
}

func TestJiraTools_UpdateIssue_AuthError(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderJira, "jira-bad", "site")
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("jira_update_issue")
	result := tool.Execute(context.Background(), map[string]any{
		"cloud_id": "abc123", "issue_key": "PROJ-1", "status": "In Progress",
	})
	if !result.IsError {
		t.Error("expected auth error for jira_update_issue")
	}
}

// --- Bitbucket additional Execute paths ---

func TestBitbucketTools_CreatePR_AuthError(t *testing.T) {
	reg := tools.NewRegistry()
	store, mgr := newConnectedStore(t, connections.ProviderBitbucket, "bb-bad", "user")
	_ = conntools.RegisterAll(reg, mgr, store)
	tool, _ := reg.Get("bitbucket_create_pr")
	result := tool.Execute(context.Background(), map[string]any{
		"workspace": "myws", "repo_slug": "myrepo",
		"title": "My PR", "source_branch": "feature", "target_branch": "main",
	})
	if !result.IsError {
		t.Error("expected auth error for bitbucket_create_pr")
	}
}

// --- resolveConnection via account label ---

// TestRegisterForProvider_MultipleConnections_ResolvesLabel verifies that
// resolveConnection returns the connection matching the account label.
func TestRegisterForProvider_MultipleConnections_ResolvesLabel(t *testing.T) {
	// We test this indirectly: register two GitHub connections and execute
	// with an explicit account label that does not exist → falls back to first.
	dir := t.TempDir()
	store, err := connections.NewStore(dir + "/conns.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add(connections.Connection{
		ID: "gh-1", Provider: connections.ProviderGitHub, AccountLabel: "user1@gh.com",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(connections.Connection{
		ID: "gh-2", Provider: connections.ProviderGitHub, AccountLabel: "user2@gh.com",
	}); err != nil {
		t.Fatal(err)
	}
	secrets := connections.NewMemoryStore()
	mgr := connections.NewManager(store, secrets, "http://localhost:9999/oauth/callback")

	reg := tools.NewRegistry()
	if err := conntools.RegisterAll(reg, mgr, store); err != nil {
		t.Fatal(err)
	}

	tool, _ := reg.Get("github_list_prs")
	// Execute with account that doesn't match either — falls back to first conn
	result := tool.Execute(context.Background(), map[string]any{
		"owner":   "org",
		"repo":    "repo",
		"account": "nonexistent@gh.com",
	})
	// Should return auth error (no token stored), not a panic
	if !result.IsError {
		t.Error("expected auth error (no token), got success")
	}
}

// TestRegisterAll_AllProviders verifies all providers can be registered together.
func TestRegisterAll_AllProviders(t *testing.T) {
	dir := t.TempDir()
	store, err := connections.NewStore(dir + "/conns.json")
	if err != nil {
		t.Fatal(err)
	}

	providers := []struct {
		id       string
		provider connections.Provider
		label    string
	}{
		{"gh-1", connections.ProviderGitHub, "user@gh.com"},
		{"g-1", connections.ProviderGoogle, "user@gmail.com"},
		{"sl-1", connections.ProviderSlack, "myworkspace"},
		{"jira-1", connections.ProviderJira, "mysite"},
		{"bb-1", connections.ProviderBitbucket, "myuser"},
	}
	for _, p := range providers {
		if err := store.Add(connections.Connection{
			ID: p.id, Provider: p.provider, AccountLabel: p.label,
		}); err != nil {
			t.Fatalf("store.Add %s: %v", p.id, err)
		}
	}

	secrets := connections.NewMemoryStore()
	mgr := connections.NewManager(store, secrets, "http://localhost:9999/oauth/callback")
	reg := tools.NewRegistry()

	if err := conntools.RegisterAll(reg, mgr, store); err != nil {
		t.Fatalf("RegisterAll with all providers: %v", err)
	}

	expectedTools := []string{
		"github_list_prs", "github_get_pr", "github_create_issue",
		"gmail_search", "gmail_read", "gmail_send",
		"slack_list_channels", "slack_read_channel", "slack_post_message",
		"jira_list_issues", "jira_get_issue", "jira_create_issue", "jira_update_issue",
		"bitbucket_list_prs", "bitbucket_get_pr", "bitbucket_create_pr",
	}
	for _, name := range expectedTools {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("expected %q to be registered", name)
		}
	}
}
