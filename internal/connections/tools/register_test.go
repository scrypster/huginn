package conntools_test

import (
	"os"
	"testing"

	"github.com/scrypster/huginn/internal/connections"
	conntools "github.com/scrypster/huginn/internal/connections/tools"
	"github.com/scrypster/huginn/internal/tools"
)

func newTestStore(t *testing.T) *connections.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := connections.NewStore(dir + "/conns.json")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func newTestManager(store *connections.Store) *connections.Manager {
	secrets := connections.NewMemoryStore()
	return connections.NewManager(store, secrets, "http://localhost:9999/oauth/callback")
}

func TestRegisterAll_EmptyStore(t *testing.T) {
	reg := tools.NewRegistry()
	store := newTestStore(t)
	mgr := newTestManager(store)

	err := conntools.RegisterAll(reg, mgr, store)
	if err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}
	// No tools should be registered for empty store
	for _, name := range []string{"gmail_search", "github_list_prs", "slack_list_channels",
		"jira_list_issues", "bitbucket_list_prs"} {
		if _, ok := reg.Get(name); ok {
			t.Errorf("expected %q to not be registered for empty store", name)
		}
	}
}

func TestRegisterAll_WithGitHubConnection(t *testing.T) {
	reg := tools.NewRegistry()
	store := newTestStore(t)
	mgr := newTestManager(store)

	if err := store.Add(connections.Connection{
		ID:           "gh-1",
		Provider:     connections.ProviderGitHub,
		AccountLabel: "user@github.com",
	}); err != nil {
		t.Fatalf("store.Add: %v", err)
	}

	if err := conntools.RegisterAll(reg, mgr, store); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	for _, name := range []string{"github_list_prs", "github_get_pr", "github_create_issue",
		"github_search_code", "github_list_issues"} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("%q should be registered when GitHub connection exists", name)
		}
	}
	// Gmail tools should NOT be registered
	if _, ok := reg.Get("gmail_search"); ok {
		t.Error("gmail_search should not be registered when only GitHub connection exists")
	}
}

func TestRegisterAll_WithGoogleConnection(t *testing.T) {
	reg := tools.NewRegistry()
	store := newTestStore(t)
	mgr := newTestManager(store)

	if err := store.Add(connections.Connection{
		ID:           "g-1",
		Provider:     connections.ProviderGoogle,
		AccountLabel: "user@gmail.com",
	}); err != nil {
		t.Fatalf("store.Add: %v", err)
	}

	if err := conntools.RegisterAll(reg, mgr, store); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	for _, name := range []string{"gmail_search", "gmail_read", "gmail_send"} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("%q should be registered when Google connection exists", name)
		}
	}
}

func TestRegisterAll_WithSlackConnection(t *testing.T) {
	reg := tools.NewRegistry()
	store := newTestStore(t)
	mgr := newTestManager(store)

	if err := store.Add(connections.Connection{
		ID:           "sl-1",
		Provider:     connections.ProviderSlack,
		AccountLabel: "user@myworkspace",
	}); err != nil {
		t.Fatalf("store.Add: %v", err)
	}

	if err := conntools.RegisterAll(reg, mgr, store); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	for _, name := range []string{"slack_list_channels", "slack_read_channel", "slack_post_message"} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("%q should be registered when Slack connection exists", name)
		}
	}
}

func TestRegisterAll_WithJiraConnection(t *testing.T) {
	reg := tools.NewRegistry()
	store := newTestStore(t)
	mgr := newTestManager(store)

	if err := store.Add(connections.Connection{
		ID:           "jira-1",
		Provider:     connections.ProviderJira,
		AccountLabel: "mysite",
	}); err != nil {
		t.Fatalf("store.Add: %v", err)
	}

	if err := conntools.RegisterAll(reg, mgr, store); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	for _, name := range []string{"jira_list_issues", "jira_get_issue", "jira_create_issue", "jira_update_issue"} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("%q should be registered when Jira connection exists", name)
		}
	}
}

func TestRegisterAll_WithBitbucketConnection(t *testing.T) {
	reg := tools.NewRegistry()
	store := newTestStore(t)
	mgr := newTestManager(store)

	if err := store.Add(connections.Connection{
		ID:           "bb-1",
		Provider:     connections.ProviderBitbucket,
		AccountLabel: "myuser",
	}); err != nil {
		t.Fatalf("store.Add: %v", err)
	}

	if err := conntools.RegisterAll(reg, mgr, store); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	for _, name := range []string{"bitbucket_list_prs", "bitbucket_get_pr", "bitbucket_create_pr"} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("%q should be registered when Bitbucket connection exists", name)
		}
	}
}

func TestRegisterAll_IdempotentCalledTwice(t *testing.T) {
	reg := tools.NewRegistry()
	store := newTestStore(t)
	mgr := newTestManager(store)

	if err := store.Add(connections.Connection{
		ID:           "gh-1",
		Provider:     connections.ProviderGitHub,
		AccountLabel: "u@g.com",
	}); err != nil {
		t.Fatalf("store.Add: %v", err)
	}

	// Call twice — should not panic or duplicate
	if err := conntools.RegisterAll(reg, mgr, store); err != nil {
		t.Fatalf("first RegisterAll: %v", err)
	}
	if err := conntools.RegisterAll(reg, mgr, store); err != nil {
		t.Fatalf("second RegisterAll: %v", err)
	}

	if _, ok := reg.Get("github_list_prs"); !ok {
		t.Error("github_list_prs should be registered after idempotent calls")
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
