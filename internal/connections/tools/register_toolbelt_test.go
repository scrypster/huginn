package conntools_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/connections"
	conntools "github.com/scrypster/huginn/internal/connections/tools"
	"github.com/scrypster/huginn/internal/tools"
)

func TestRegisterAll_TagsGitHubProvider(t *testing.T) {
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

	githubTools := []string{
		"github_list_prs", "github_get_pr", "github_create_issue",
		"github_search_code", "github_list_issues",
	}
	for _, name := range githubTools {
		got := reg.ProviderFor(name)
		if got != string(connections.ProviderGitHub) {
			t.Errorf("ProviderFor(%q) = %q, want %q", name, got, string(connections.ProviderGitHub))
		}
	}
}

func TestRegisterAll_TagsGoogleProvider(t *testing.T) {
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

	googleTools := []string{"gmail_search", "gmail_read", "gmail_send"}
	for _, name := range googleTools {
		got := reg.ProviderFor(name)
		if got != string(connections.ProviderGoogle) {
			t.Errorf("ProviderFor(%q) = %q, want %q", name, got, string(connections.ProviderGoogle))
		}
	}
}

func TestRegisterAll_TagsSlackProvider(t *testing.T) {
	reg := tools.NewRegistry()
	store := newTestStore(t)
	mgr := newTestManager(store)

	if err := store.Add(connections.Connection{
		ID:           "sl-1",
		Provider:     connections.ProviderSlack,
		AccountLabel: "user@workspace",
	}); err != nil {
		t.Fatalf("store.Add: %v", err)
	}

	if err := conntools.RegisterAll(reg, mgr, store); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	slackTools := []string{"slack_list_channels", "slack_read_channel", "slack_post_message"}
	for _, name := range slackTools {
		got := reg.ProviderFor(name)
		if got != string(connections.ProviderSlack) {
			t.Errorf("ProviderFor(%q) = %q, want %q", name, got, string(connections.ProviderSlack))
		}
	}
}

func TestRegisterAll_NoTagForUnregisteredProvider(t *testing.T) {
	reg := tools.NewRegistry()
	store := newTestStore(t)
	mgr := newTestManager(store)

	// Empty store — no providers registered
	if err := conntools.RegisterAll(reg, mgr, store); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	// No tool should have a provider tag
	for _, name := range []string{"github_list_prs", "gmail_search", "slack_post_message"} {
		if got := reg.ProviderFor(name); got != "" {
			t.Errorf("ProviderFor(%q) = %q, want empty string for unregistered provider", name, got)
		}
	}
}

func TestRegisterForProvider_TagsJiraTools(t *testing.T) {
	reg := tools.NewRegistry()
	store := newTestStore(t)
	mgr := newTestManager(store)

	conns := []connections.Connection{{
		ID:           "jira-1",
		Provider:     connections.ProviderJira,
		AccountLabel: "mysite",
	}}

	if err := conntools.RegisterForProvider(reg, mgr, connections.ProviderJira, conns); err != nil {
		t.Fatalf("RegisterForProvider: %v", err)
	}

	jiraTools := []string{"jira_list_issues", "jira_get_issue", "jira_create_issue", "jira_update_issue"}
	for _, name := range jiraTools {
		got := reg.ProviderFor(name)
		if got != string(connections.ProviderJira) {
			t.Errorf("ProviderFor(%q) = %q, want %q", name, got, string(connections.ProviderJira))
		}
	}
}
