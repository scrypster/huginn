package providers

// Slack OAuth 2.0 workspace-scoped tokens.
// Scopes: channels:read, chat:write, files:read, users:read
// GetAccountInfo: POST https://slack.com/api/auth.test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/oauth2"

	"github.com/scrypster/huginn/internal/connections"
)

// SlackProvider implements IntegrationProvider for Slack OAuth2.
type SlackProvider struct {
	clientID     string
	clientSecret string
}

// NewSlack creates a new SlackProvider.
func NewSlack(clientID, clientSecret string) *SlackProvider {
	return &SlackProvider{clientID: clientID, clientSecret: clientSecret}
}

func (s *SlackProvider) Name() connections.Provider { return connections.ProviderSlack }
func (s *SlackProvider) DisplayName() string        { return "Slack" }

func (s *SlackProvider) OAuthConfig(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     s.clientID,
		ClientSecret: s.clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{"channels:read", "chat:write", "files:read", "users:read"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://slack.com/oauth/v2/authorize",
			TokenURL: "https://slack.com/api/oauth.v2.access",
		},
	}
}

func (s *SlackProvider) GetAccountInfo(ctx context.Context, client *http.Client) (*connections.AccountInfo, error) {
	// Slack auth.test is a POST with no body (token is in Authorization header via client)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/auth.test",
		strings.NewReader(url.Values{}.Encode()))
	if err != nil {
		return nil, fmt.Errorf("slack: build auth.test request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("slack: auth.test: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		OK     bool   `json:"ok"`
		Error  string `json:"error"`
		UserID string `json:"user_id"`
		User   string `json:"user"`
		Team   string `json:"team"`
		TeamID string `json:"team_id"`
		URL    string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("slack: decode auth.test: %w", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("slack: auth.test error: %s", result.Error)
	}
	// Label is "user@workspace" for multi-account disambiguation
	label := fmt.Sprintf("%s@%s", result.User, result.Team)
	return &connections.AccountInfo{
		ID:          result.UserID,
		Label:       label,
		DisplayName: fmt.Sprintf("%s (%s)", result.User, result.Team),
		AvatarURL:   "",
	}, nil
}
