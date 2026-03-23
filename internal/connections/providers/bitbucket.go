package providers

// Bitbucket OAuth 2.0
// Auth URL: https://bitbucket.org/site/oauth2/authorize
// Scopes: repository pullrequest account

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"

	"github.com/scrypster/huginn/internal/connections"
)

// BitbucketProvider implements IntegrationProvider for Bitbucket OAuth2.
type BitbucketProvider struct {
	clientID     string
	clientSecret string
}

// NewBitbucket creates a new BitbucketProvider.
func NewBitbucket(clientID, clientSecret string) *BitbucketProvider {
	return &BitbucketProvider{clientID: clientID, clientSecret: clientSecret}
}

func (b *BitbucketProvider) Name() connections.Provider { return connections.ProviderBitbucket }
func (b *BitbucketProvider) DisplayName() string        { return "Bitbucket" }

func (b *BitbucketProvider) OAuthConfig(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     b.clientID,
		ClientSecret: b.clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{"repository", "pullrequest", "account"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://bitbucket.org/site/oauth2/authorize",
			TokenURL: "https://bitbucket.org/site/oauth2/access_token",
		},
	}
}

func (b *BitbucketProvider) GetAccountInfo(ctx context.Context, client *http.Client) (*connections.AccountInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.bitbucket.org/2.0/user", nil)
	if err != nil {
		return nil, fmt.Errorf("bitbucket: build user request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bitbucket: user: %w", err)
	}
	defer resp.Body.Close()
	var user struct {
		UUID        string `json:"uuid"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Links       struct {
			Avatar struct {
				Href string `json:"href"`
			} `json:"avatar"`
		} `json:"links"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("bitbucket: decode user: %w", err)
	}
	return &connections.AccountInfo{
		ID:          user.UUID,
		Label:       user.Username,
		DisplayName: user.DisplayName,
		AvatarURL:   user.Links.Avatar.Href,
	}, nil
}
