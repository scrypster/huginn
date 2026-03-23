package providers

// Atlassian OAuth 2.0 (3LO)
// Auth URL: https://auth.atlassian.com/authorize
// Token URL: https://auth.atlassian.com/oauth/token
// Scopes: read:jira-work write:jira-work read:jira-user offline_access
// GetAccountInfo: GET https://api.atlassian.com/oauth/token/accessible-resources → cloudId

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"

	"github.com/scrypster/huginn/internal/connections"
)

// JiraProvider implements IntegrationProvider for Atlassian Jira OAuth2 (3LO).
type JiraProvider struct {
	clientID     string
	clientSecret string
}

// NewJira creates a new JiraProvider.
func NewJira(clientID, clientSecret string) *JiraProvider {
	return &JiraProvider{clientID: clientID, clientSecret: clientSecret}
}

func (j *JiraProvider) Name() connections.Provider { return connections.ProviderJira }
func (j *JiraProvider) DisplayName() string        { return "Jira" }

func (j *JiraProvider) OAuthConfig(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     j.clientID,
		ClientSecret: j.clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{"read:jira-work", "write:jira-work", "read:jira-user", "offline_access"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://auth.atlassian.com/authorize",
			TokenURL: "https://auth.atlassian.com/oauth/token",
		},
	}
}

func (j *JiraProvider) GetAccountInfo(ctx context.Context, client *http.Client) (*connections.AccountInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.atlassian.com/oauth/token/accessible-resources", nil)
	if err != nil {
		return nil, fmt.Errorf("jira: build accessible-resources request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jira: accessible-resources: %w", err)
	}
	defer resp.Body.Close()
	var resources []struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		URL       string `json:"url"`
		AvatarURL string `json:"avatarUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&resources); err != nil {
		return nil, fmt.Errorf("jira: decode accessible-resources: %w", err)
	}
	if len(resources) == 0 {
		return nil, fmt.Errorf("jira: no accessible resources found")
	}
	// Use the first accessible resource (cloud site) as the account identity.
	r := resources[0]
	return &connections.AccountInfo{
		ID:          r.ID,
		Label:       r.Name,
		DisplayName: r.Name,
		AvatarURL:   r.AvatarURL,
	}, nil
}
