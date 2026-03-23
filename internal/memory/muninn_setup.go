package memory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// MuninnSetupClient performs admin operations against a MuninnDB instance
// using root credentials. Used only during agent setup, not for regular memory ops.
type MuninnSetupClient struct {
	endpoint   string // REST API endpoint, e.g. "http://localhost:8475"
	httpClient *http.Client
}

// NewMuninnSetupClient creates a client for the given MuninnDB REST API endpoint
// (e.g. "http://localhost:8475"). Login calls are automatically routed to the
// Web UI server (default :8476) since that is where MuninnDB's auth endpoint lives.
func NewMuninnSetupClient(endpoint string) *MuninnSetupClient {
	return &MuninnSetupClient{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// uiEndpoint derives the MuninnDB Web UI endpoint from the REST API endpoint.
// MuninnDB runs a REST API (default :8475) and a Web UI (:8476) on adjacent ports.
// Login happens via the Web UI server; admin vault/key ops happen via the REST API.
func uiEndpoint(restEndpoint string) string {
	u, err := url.Parse(restEndpoint)
	if err != nil {
		return restEndpoint
	}
	host := u.Hostname()
	port := u.Port()
	if port == "8475" {
		u.Host = host + ":8476"
	}
	return u.String()
}

// Probe checks whether a MuninnDB instance is reachable at the given REST endpoint
// without requiring credentials. Returns true if any HTTP response is received.
func Probe(endpoint string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(uiEndpoint(endpoint))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

// Login authenticates with MuninnDB root credentials via the Web UI server.
// Returns the muninn_session cookie value on success.
func (c *MuninnSetupClient) Login(username, password string) (string, error) {
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		return "", fmt.Errorf("muninn setup: marshal login body: %w", err)
	}
	loginURL := uiEndpoint(c.endpoint) + "/api/auth/login"
	resp, err := c.httpClient.Post(loginURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("muninn setup: login request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("muninn setup: login: server returned %d", resp.StatusCode)
	}
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "muninn_session" {
			return cookie.Value, nil
		}
	}
	return "", fmt.Errorf("muninn setup: login: no muninn_session cookie in response")
}

// CreateVault creates a vault in MuninnDB via the admin REST API.
// sessionCookie is the value returned by Login.
func (c *MuninnSetupClient) CreateVault(sessionCookie, vaultName string) error {
	body, err := json.Marshal(map[string]any{
		"name":   vaultName,
		"public": false,
	})
	if err != nil {
		return fmt.Errorf("muninn setup: marshal vault body: %w", err)
	}
	req, err := http.NewRequest(http.MethodPut, c.endpoint+"/api/admin/vaults/config", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("muninn setup: create vault request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "muninn_session", Value: sessionCookie})

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("muninn setup: create vault: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		// Vault already exists — that's fine, we'll just create a key for it.
		return nil
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("muninn setup: create vault: server returned %d", resp.StatusCode)
	}
	return nil
}

// CreateVaultAndKey creates a vault in MuninnDB (if it doesn't already exist) and
// generates a full-mode mk_... API key scoped to that vault.
// sessionCookie is the value returned by Login.
// Returns the mk_... token string which should be stored in GlobalConfig.VaultTokens.
func (c *MuninnSetupClient) CreateVaultAndKey(sessionCookie, vaultName, label string) (string, error) {
	// Ensure the vault exists first.
	if err := c.CreateVault(sessionCookie, vaultName); err != nil {
		return "", err
	}

	body, err := json.Marshal(map[string]string{
		"vault": vaultName,
		"label": label,
		"mode":  "full",
	})
	if err != nil {
		return "", fmt.Errorf("muninn setup: marshal key body: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.endpoint+"/api/admin/keys", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("muninn setup: create key request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "muninn_session", Value: sessionCookie})

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("muninn setup: create key: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("muninn setup: create key: server returned %d", resp.StatusCode)
	}
	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("muninn setup: create key: decode response: %w", err)
	}
	if result.Token == "" {
		return "", fmt.Errorf("muninn setup: create key: empty token in response")
	}
	return result.Token, nil
}
