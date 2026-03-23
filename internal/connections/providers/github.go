package providers

// GitHub OAuth App flow (v1.0). No refresh tokens — ExpiresAt.IsZero() when stored.
// Scopes: repo, read:org, read:user

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"

	"github.com/scrypster/huginn/internal/connections"
)

// GitHubProvider implements IntegrationProvider for GitHub OAuth.
type GitHubProvider struct {
	clientID     string
	clientSecret string
}

// NewGitHub creates a new GitHubProvider.
func NewGitHub(clientID, clientSecret string) *GitHubProvider {
	return &GitHubProvider{clientID: clientID, clientSecret: clientSecret}
}

func (g *GitHubProvider) Name() connections.Provider { return connections.ProviderGitHub }
func (g *GitHubProvider) DisplayName() string        { return "GitHub" }

func (g *GitHubProvider) OAuthConfig(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     g.clientID,
		ClientSecret: g.clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{"repo", "read:org", "read:user"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://github.com/login/oauth/authorize",
			TokenURL: "https://github.com/login/oauth/access_token",
		},
	}
}

func (g *GitHubProvider) GetAccountInfo(ctx context.Context, client *http.Client) (*connections.AccountInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	if err != nil {
		return nil, fmt.Errorf("github: build user request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: user: %w", err)
	}
	defer resp.Body.Close()
	var user struct {
		ID        int    `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("github: decode user: %w", err)
	}
	return &connections.AccountInfo{
		ID:          fmt.Sprintf("%d", user.ID),
		Label:       user.Login,
		DisplayName: user.Name,
		AvatarURL:   user.AvatarURL,
	}, nil
}
