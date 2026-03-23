package providers

// Slack OAuth 2.0 user token (Huginn Impersonator app).
// Acts on behalf of the authorizing user — messages appear as them, not as the bot.
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

// SlackUserProvider implements IntegrationProvider for Slack user-token OAuth2.
type SlackUserProvider struct {
	clientID     string
	clientSecret string
}

// NewSlackUser creates a new SlackUserProvider.
func NewSlackUser(clientID, clientSecret string) *SlackUserProvider {
	return &SlackUserProvider{clientID: clientID, clientSecret: clientSecret}
}

func (s *SlackUserProvider) Name() connections.Provider { return connections.ProviderSlackUser }
func (s *SlackUserProvider) DisplayName() string        { return "Slack (as you)" }

func (s *SlackUserProvider) OAuthConfig(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     s.clientID,
		ClientSecret: s.clientSecret,
		RedirectURL:  redirectURL,
		Scopes: []string{
			"canvases:read", "canvases:write",
			"channels:history", "channels:read", "channels:write", "channels:write.invites",
			"chat:write",
			"dnd:read", "dnd:write",
			"email",
			"files:read", "files:write",
			"groups:history", "groups:read", "groups:write", "groups:write.invites",
			"identify", "identity.basic", "identity.email", "identity.team",
			"im:history", "im:read", "im:write",
			"links:read", "links:write",
			"lists:read", "lists:write",
			"mpim:history", "mpim:read", "mpim:write",
			"pins:read", "pins:write",
			"profile",
			"reactions:read", "reactions:write",
			"reminders:read", "reminders:write",
			"search:read", "search:read.files", "search:read.im", "search:read.mpim",
			"search:read.private", "search:read.public", "search:read.users",
			"stars:read", "stars:write",
			"usergroups:read", "usergroups:write",
			"users.profile:read", "users.profile:write",
			"users:read", "users:write",
		},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://slack.com/oauth/v2/authorize",
			TokenURL: "https://slack.com/api/oauth.v2.access",
		},
	}
}

func (s *SlackUserProvider) GetAccountInfo(ctx context.Context, client *http.Client) (*connections.AccountInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/auth.test",
		strings.NewReader(url.Values{}.Encode()))
	if err != nil {
		return nil, fmt.Errorf("slack_user: build auth.test request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("slack_user: auth.test: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		OK     bool   `json:"ok"`
		Error  string `json:"error"`
		UserID string `json:"user_id"`
		User   string `json:"user"`
		Team   string `json:"team"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("slack_user: decode auth.test: %w", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("slack_user: auth.test error: %s", result.Error)
	}
	label := fmt.Sprintf("%s@%s", result.User, result.Team)
	return &connections.AccountInfo{
		ID:          result.UserID,
		Label:       label,
		DisplayName: fmt.Sprintf("%s (%s)", result.User, result.Team),
		AvatarURL:   "",
	}, nil
}
