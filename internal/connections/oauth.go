package connections

import (
	"context"
	"net/http"

	"golang.org/x/oauth2"
)

// IntegrationProvider is implemented by each OAuth provider (Google, Slack, etc.).
// It supplies the provider identity, OAuth2 config, and a way to fetch the
// authenticated account's display info after the first token exchange.
type IntegrationProvider interface {
	// Name returns the canonical Provider constant for this integration.
	Name() Provider

	// DisplayName returns the human-readable name shown in the UI.
	DisplayName() string

	// OAuthConfig returns the oauth2.Config for this provider.
	// redirectURL is the callback URL registered with the provider
	// (e.g. "http://localhost:PORT/oauth/callback").
	OAuthConfig(redirectURL string) *oauth2.Config

	// GetAccountInfo fetches the authenticated user's account details
	// using the freshly-exchanged token. client is pre-authorised.
	GetAccountInfo(ctx context.Context, client *http.Client) (*AccountInfo, error)
}
