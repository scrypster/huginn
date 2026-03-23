package conntools_test

// tool_metadata_test.go exercises Description(), Permission(), and Schema()
// on every registered integration tool without making real HTTP calls.
// Coverage target: bring connections/tools above 65%.

import (
	"testing"

	"github.com/scrypster/huginn/internal/connections"
	conntools "github.com/scrypster/huginn/internal/connections/tools"
	"github.com/scrypster/huginn/internal/tools"
)

// allProviders lists every provider that has tools registered.
var allProviders = []struct {
	provider connections.Provider
	connType connections.ConnectionType
}{
	{connections.ProviderGoogle, connections.ConnectionTypeOAuth},
	{connections.ProviderGitHub, connections.ConnectionTypeOAuth},
	{connections.ProviderSlack, connections.ConnectionTypeOAuth},
	{connections.ProviderJira, connections.ConnectionTypeOAuth},
	{connections.ProviderBitbucket, connections.ConnectionTypeOAuth},
	{connections.ProviderDatadog, connections.ConnectionTypeAPIKey},
	{connections.ProviderSplunk, connections.ConnectionTypeAPIKey},
	{connections.ProviderElastic, connections.ConnectionTypeAPIKey},
	{connections.ProviderGrafana, connections.ConnectionTypeAPIKey},
	{connections.ProviderPagerDuty, connections.ConnectionTypeAPIKey},
	{connections.ProviderNewRelic, connections.ConnectionTypeAPIKey},
	{connections.ProviderCrowdStrike, connections.ConnectionTypeAPIKey},
	{connections.ProviderTerraform, connections.ConnectionTypeAPIKey},
	{connections.ProviderServiceNow, connections.ConnectionTypeAPIKey},
	{connections.ProviderNotion, connections.ConnectionTypeAPIKey},
	{connections.ProviderAirtable, connections.ConnectionTypeAPIKey},
	{connections.ProviderHubSpot, connections.ConnectionTypeAPIKey},
	{connections.ProviderZendesk, connections.ConnectionTypeAPIKey},
	{connections.ProviderAsana, connections.ConnectionTypeAPIKey},
	{connections.ProviderMonday, connections.ConnectionTypeAPIKey},
}

// TestToolMetadata_AllProviders verifies that every registered tool satisfies
// the tools.Tool interface contract: non-empty Name/Description, valid Schema name,
// and valid PermissionLevel. No HTTP calls are made.
func TestToolMetadata_AllProviders(t *testing.T) {
	for _, tc := range allProviders {
		tc := tc
		t.Run(string(tc.provider), func(t *testing.T) {
			store := newTestStore(t)
			mgr := newTestManager(store)

			conn := connections.Connection{
				ID:           "test-" + string(tc.provider),
				Provider:     tc.provider,
				Type:         tc.connType,
				AccountLabel: string(tc.provider) + " test",
			}
			if err := store.Add(conn); err != nil {
				t.Fatalf("Add: %v", err)
			}

			reg := tools.NewRegistry()
			if err := conntools.RegisterForProvider(reg, mgr, tc.provider, []connections.Connection{conn}); err != nil {
				t.Fatalf("RegisterForProvider(%s): %v", tc.provider, err)
			}

			allTools := reg.All()
			if len(allTools) == 0 {
				t.Fatalf("no tools registered for provider %s", tc.provider)
			}

			for _, tool := range allTools {
				name := tool.Name()
				if name == "" {
					t.Errorf("tool has empty Name()")
				}
				desc := tool.Description()
				if desc == "" {
					t.Errorf("tool %q has empty Description()", name)
				}
				perm := tool.Permission()
				if perm < tools.PermRead || perm > tools.PermExec {
					t.Errorf("tool %q has invalid Permission() %d", name, perm)
				}
				schema := tool.Schema()
				if schema.Function.Name != name {
					t.Errorf("tool %q Schema().Function.Name = %q, want %q", name, schema.Function.Name, name)
				}
			}
		})
	}
}
