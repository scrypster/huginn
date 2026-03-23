package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/scrypster/huginn/internal/connections"
)

// GoogleScopes maps product names to their OAuth2 scopes.
// Scopes must match those approved in the Google Cloud OAuth consent screen.
var GoogleScopes = map[string][]string{
	"gmail":    {"https://www.googleapis.com/auth/gmail.readonly", "https://www.googleapis.com/auth/gmail.send"},
	"calendar": {"https://www.googleapis.com/auth/calendar.readonly"},
	"drive":    {"https://www.googleapis.com/auth/drive.file"},
	"docs":     {"https://www.googleapis.com/auth/documents"},
	"sheets":   {"https://www.googleapis.com/auth/spreadsheets"},
	"contacts": {"https://www.googleapis.com/auth/contacts.readonly"},
}

// GoogleProvider implements IntegrationProvider for Google OAuth2.
type GoogleProvider struct {
	clientID     string
	clientSecret string
	products     []string // e.g. ["gmail", "drive"]
}

// NewGoogle creates a new GoogleProvider for the given products.
func NewGoogle(clientID, clientSecret string, products []string) *GoogleProvider {
	return &GoogleProvider{
		clientID:     clientID,
		clientSecret: clientSecret,
		products:     products,
	}
}

func (g *GoogleProvider) Name() connections.Provider { return connections.ProviderGoogle }
func (g *GoogleProvider) DisplayName() string        { return "Google" }

func (g *GoogleProvider) OAuthConfig(redirectURL string) *oauth2.Config {
	var scopes []string
	scopes = append(scopes,
		"https://www.googleapis.com/auth/userinfo.email",
	)
	for _, product := range g.products {
		if s, ok := GoogleScopes[product]; ok {
			scopes = append(scopes, s...)
		}
	}
	return &oauth2.Config{
		ClientID:     g.clientID,
		ClientSecret: g.clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       scopes,
		Endpoint:     google.Endpoint,
	}
}

func (g *GoogleProvider) GetAccountInfo(ctx context.Context, client *http.Client) (*connections.AccountInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return nil, fmt.Errorf("google: build userinfo request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google: userinfo: %w", err)
	}
	defer resp.Body.Close()
	var info struct {
		ID      string `json:"id"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("google: decode userinfo: %w", err)
	}
	return &connections.AccountInfo{
		ID:          info.ID,
		Label:       info.Email,
		DisplayName: info.Name,
		AvatarURL:   info.Picture,
	}, nil
}
