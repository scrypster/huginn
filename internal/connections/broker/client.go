package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// TokenStorer loads the machine JWT. Matches relay.TokenStorer interface.
type TokenStorer interface {
	Load() (string, error)
	IsRegistered() bool
}

// Client calls the HuginnCloud OAuth broker to start an OAuth flow.
type Client struct {
	brokerURL  string
	tokenStore TokenStorer
	httpClient *http.Client
}

// NewClient creates a broker Client.
// brokerURL should be "https://oauth.huginncloud.com" (no trailing slash).
// tokenStore is optional; if omitted, machine-JWT-authenticated endpoints are unavailable.
func NewClient(brokerURL string, tokenStore ...TokenStorer) *Client {
	c := &Client{
		brokerURL:  brokerURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
	if len(tokenStore) > 0 {
		c.tokenStore = tokenStore[0]
	}
	return c
}

type startRequest struct {
	Provider            string `json:"provider"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	Port                int    `json:"port"`
}

type startResponse struct {
	AuthURL string `json:"auth_url"`
	State   string `json:"state"`
	Error   string `json:"error,omitempty"`
}

// Start POSTs to /oauth/start and returns the auth URL to open in the browser.
// provider is the provider name (e.g. "github").
// relayChallenge is the base64url-encoded SHA-256 relay secret (43 chars).
// port is the local server port (e.g. 8477).
func (c *Client) Start(ctx context.Context, provider, relayChallenge string, port int) (string, error) {
	machineJWT, err := c.tokenStore.Load()
	if err != nil {
		return "", fmt.Errorf("broker: load machine JWT: %w", err)
	}

	body, err := json.Marshal(startRequest{
		Provider:            provider,
		CodeChallenge:       relayChallenge,
		CodeChallengeMethod: "S256",
		Port:                port,
	})
	if err != nil {
		return "", fmt.Errorf("broker: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.brokerURL+"/oauth/start", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("broker: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+machineJWT)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("broker: request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("broker: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("broker: server returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result startResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("broker: parse response: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("broker: %s", result.Error)
	}
	if result.AuthURL == "" {
		return "", fmt.Errorf("broker: empty auth_url in response")
	}
	return result.AuthURL, nil
}

type startCloudFlowRequest struct {
	Provider string `json:"provider"`
	RelayKey string `json:"relay_key"` // base64url-encoded 32-byte key
}

type startCloudFlowResponse struct {
	AuthURL string `json:"auth_url"`
	Error   string `json:"error,omitempty"`
}

// StartCloudFlow calls POST /oauth/start with relay_key and returns the auth URL
// the cloud app should open in a popup. This is the cloud-UI variant of Start.
// Unlike Start, it does not open a local port — tokens are delivered via relay JWT
// through the cloud app WebSocket.
func (c *Client) StartCloudFlow(ctx context.Context, provider, relayKey string) (string, error) {
	machineJWT, err := c.tokenStore.Load()
	if err != nil {
		return "", fmt.Errorf("broker: load machine JWT: %w", err)
	}

	body, err := json.Marshal(startCloudFlowRequest{
		Provider: provider,
		RelayKey: relayKey,
	})
	if err != nil {
		return "", fmt.Errorf("broker: marshal cloud flow request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.brokerURL+"/oauth/start", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("broker: create cloud flow request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+machineJWT)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("broker: cloud flow request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("broker: read cloud flow response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("broker: cloud flow returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result startCloudFlowResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("broker: parse cloud flow response: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("broker: %s", result.Error)
	}
	if result.AuthURL == "" {
		return "", fmt.Errorf("broker: empty auth_url in cloud flow response")
	}
	return result.AuthURL, nil
}

type refreshRequest struct {
	Provider       string `json:"provider"`
	RefreshToken   string `json:"refresh_token"`
	RelayChallenge string `json:"relay_challenge"`
}

type refreshResponse struct {
	Token string `json:"token"`
	Error string `json:"error,omitempty"`
}

// Refresh POSTs to /oauth/refresh and returns a RelayResult with new credentials.
// provider is the provider name (e.g. "github"). refreshToken is the current refresh token.
// relayChallenge is the base64url-encoded 32-byte HMAC key used to verify the response JWT.
func (c *Client) Refresh(ctx context.Context, provider, refreshToken, relayChallenge string) (*RelayResult, error) {
	body, err := json.Marshal(refreshRequest{
		Provider:       provider,
		RefreshToken:   refreshToken,
		RelayChallenge: relayChallenge,
	})
	if err != nil {
		return nil, fmt.Errorf("broker: marshal refresh request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.brokerURL+"/oauth/refresh", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("broker: create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("broker: refresh request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("broker: read refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("broker: refresh returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result refreshResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("broker: parse refresh response: %w", err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("broker: refresh error: %s", result.Error)
	}
	if result.Token == "" {
		return nil, fmt.Errorf("broker: empty token in refresh response")
	}

	return ParseRelayJWT(result.Token, relayChallenge)
}
