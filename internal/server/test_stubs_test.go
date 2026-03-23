package server

import (
	"context"
	"net/http"

	"github.com/scrypster/huginn/internal/connections"
	"golang.org/x/oauth2"
)

// stubProvider is a minimal connections.IntegrationProvider for tests.
type stubProvider struct {
	name connections.Provider
}

func (p *stubProvider) Name() connections.Provider { return p.name }
func (p *stubProvider) DisplayName() string        { return string(p.name) }
func (p *stubProvider) OAuthConfig(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  redirectURL,
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://example.com/oauth/authorize",
			TokenURL: "https://example.com/oauth/token",
		},
	}
}
func (p *stubProvider) GetAccountInfo(_ context.Context, _ *http.Client) (*connections.AccountInfo, error) {
	return &connections.AccountInfo{ID: "stub-user", Label: "stubuser"}, nil
}

// mockBrokerClient is a BrokerClient that captures the relay challenge for inspection.
type mockBrokerClient struct {
	authURL      string
	gotChallenge string
}

func (m *mockBrokerClient) Start(_ context.Context, provider, relayChallenge string, port int) (string, error) {
	m.gotChallenge = relayChallenge
	return m.authURL, nil
}

func (m *mockBrokerClient) StartCloudFlow(_ context.Context, _, _ string) (string, error) {
	return m.authURL, nil
}
