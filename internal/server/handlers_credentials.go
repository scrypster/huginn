package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/scrypster/huginn/internal/connections"
)

// handleSaveDatadogCredentials stores Datadog API key credentials.
// POST /api/v1/credentials/datadog
// Body: {"url":"https://api.datadoghq.com","api_key":"...","app_key":"...","label":"prod"}
// Response: {"id":"<conn-uuid>","provider":"datadog","account_label":"prod"}
func (s *Server) handleSaveDatadogCredentials(w http.ResponseWriter, r *http.Request) {
	if s.connMgr == nil {
		jsonError(w, 503, "connections not configured")
		return
	}
	var req struct {
		URL    string `json:"url"`
		APIKey string `json:"api_key"`
		AppKey string `json:"app_key"`
		Label  string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if req.URL == "" {
		req.URL = "https://api.datadoghq.com"
	}
	if req.APIKey == "" {
		jsonError(w, 400, "api_key is required")
		return
	}
	if req.AppKey == "" {
		jsonError(w, 400, "app_key is required")
		return
	}
	if req.Label == "" {
		req.Label = "Datadog"
	}

	// Validate by hitting the Datadog validate endpoint
	if err := validateDatadogCredentials(r.Context(), req.URL, req.APIKey, req.AppKey); err != nil {
		jsonError(w, 400, "credential validation failed: "+err.Error())
		return
	}

	conn, err := s.connMgr.StoreAPIKeyConnection(
		connections.ProviderDatadog,
		req.Label,
		map[string]string{"url": req.URL},
		map[string]string{"api_key": req.APIKey, "app_key": req.AppKey},
	)
	if err != nil {
		jsonError(w, 500, "store credentials: "+err.Error())
		return
	}
	jsonOK(w, map[string]string{
		"id":            conn.ID,
		"provider":      string(conn.Provider),
		"account_label": conn.AccountLabel,
	})
}

// handleTestDatadogCredentials probes Datadog credentials without saving.
// POST /api/v1/credentials/datadog/test
// Body: {"url":"https://api.datadoghq.com","api_key":"...","app_key":"..."}
// Response: {"ok":true} or {"ok":false,"error":"..."}
// Always returns HTTP 200 (check "ok" field).
func (s *Server) handleTestDatadogCredentials(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL    string `json:"url"`
		APIKey string `json:"api_key"`
		AppKey string `json:"app_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": "invalid request"})
		return
	}
	if req.URL == "" {
		req.URL = "https://api.datadoghq.com"
	}
	if err := validateDatadogCredentials(r.Context(), req.URL, req.APIKey, req.AppKey); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

// validateDatadogCredentials hits GET /api/v1/validate to verify credentials.
func validateDatadogCredentials(ctx context.Context, baseURL, apiKey, appKey string) error {
	if err := validateBaseURL(baseURL); err != nil {
		return fmt.Errorf("datadog: base URL: %w", err)
	}
	reqURL := fmt.Sprintf("%s/api/v1/validate", baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("DD-API-KEY", apiKey)
	req.Header.Set("DD-APPLICATION-KEY", appKey)

	client := safeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		slog.Warn("datadog: credential validation error",
			"status", resp.StatusCode,
			"body_preview", string(body[:min(len(body), 200)]))
		return fmt.Errorf("validation failed (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// handleSaveSplunkCredentials stores Splunk bearer token credentials.
// POST /api/v1/credentials/splunk
// Body: {"url":"https://your-org.splunkcloud.com","token":"...","label":"prod"}
func (s *Server) handleSaveSplunkCredentials(w http.ResponseWriter, r *http.Request) {
	if s.connMgr == nil {
		jsonError(w, 503, "connections not configured")
		return
	}
	var req struct {
		URL   string `json:"url"`
		Token string `json:"token"`
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if req.URL == "" {
		jsonError(w, 400, "url is required")
		return
	}
	if req.Token == "" {
		jsonError(w, 400, "token is required")
		return
	}
	if req.Label == "" {
		req.Label = "Splunk"
	}

	// Validate by hitting the Splunk server info endpoint
	if err := validateSplunkCredentials(r.Context(), req.URL, req.Token); err != nil {
		jsonError(w, 400, "credential validation failed: "+err.Error())
		return
	}

	conn, err := s.connMgr.StoreAPIKeyConnection(
		connections.ProviderSplunk,
		req.Label,
		map[string]string{"url": req.URL},
		map[string]string{"token": req.Token},
	)
	if err != nil {
		jsonError(w, 500, "store credentials: "+err.Error())
		return
	}
	jsonOK(w, map[string]string{
		"id":            conn.ID,
		"provider":      string(conn.Provider),
		"account_label": conn.AccountLabel,
	})
}

// handleTestSplunkCredentials probes Splunk credentials without saving.
// POST /api/v1/credentials/splunk/test
func (s *Server) handleTestSplunkCredentials(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": "invalid request"})
		return
	}
	if err := validateSplunkCredentials(r.Context(), req.URL, req.Token); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

// validateSplunkCredentials hits the Splunk server info endpoint to verify the token.
func validateSplunkCredentials(ctx context.Context, baseURL, token string) error {
	if err := validateBaseURL(baseURL); err != nil {
		return fmt.Errorf("splunk: base URL: %w", err)
	}
	reqURL := fmt.Sprintf("%s/services/server/info?output_mode=json&count=1", baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := safeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		slog.Warn("splunk: credential validation error",
			"status", resp.StatusCode,
			"body_preview", string(body[:min(len(body), 200)]))
		return fmt.Errorf("validation failed (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// validatePagerDutyCredentials validates PagerDuty API token via the /users/me endpoint.
func validatePagerDutyCredentials(ctx context.Context, apiToken, baseURL string) error {
	if baseURL == "" {
		baseURL = "https://api.pagerduty.com"
	}
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/users/me", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token token="+apiToken)
	req.Header.Set("Accept", "application/vnd.pagerduty+json;version=2")
	client := safeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		slog.Warn("pagerduty: credential validation error",
			"status", resp.StatusCode,
			"body_preview", string(body[:min(len(body), 200)]))
		return fmt.Errorf("validation failed (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// POST /api/v1/credentials/pagerduty
// Body: {"api_token":"...","label":"My PagerDuty"}
func (s *Server) handleSavePagerDutyCredentials(w http.ResponseWriter, r *http.Request) {
	if s.connMgr == nil {
		jsonError(w, 503, "connections not configured")
		return
	}
	var req struct {
		APIToken string `json:"api_token"`
		Label    string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if req.APIToken == "" {
		jsonError(w, 400, "api_token is required")
		return
	}
	if req.Label == "" {
		req.Label = "PagerDuty"
	}
	if err := validatePagerDutyCredentials(r.Context(), req.APIToken, ""); err != nil {
		jsonError(w, 400, "credential validation failed: "+err.Error())
		return
	}
	conn, err := s.connMgr.StoreAPIKeyConnection(
		connections.ProviderPagerDuty, req.Label, nil,
		map[string]string{"api_token": req.APIToken},
	)
	if err != nil {
		jsonError(w, 500, "store credentials: "+err.Error())
		return
	}
	jsonOK(w, map[string]string{"id": conn.ID, "provider": string(conn.Provider), "account_label": conn.AccountLabel})
}

// POST /api/v1/credentials/pagerduty/test
func (s *Server) handleTestPagerDutyCredentials(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIToken string `json:"api_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": "invalid JSON: " + err.Error()})
		return
	}
	if err := validatePagerDutyCredentials(r.Context(), req.APIToken, ""); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

// validateNewRelicCredentials validates New Relic API key via GraphQL query.
func validateNewRelicCredentials(ctx context.Context, apiKey, baseURL string) error {
	if baseURL == "" {
		baseURL = "https://api.newrelic.com"
	}
	body := strings.NewReader(`{"query":"{ actor { user { name } } }"}`)
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/graphql", body)
	if err != nil {
		return err
	}
	req.Header.Set("Api-Key", apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := safeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		slog.Warn("newrelic: credential validation error",
			"status", resp.StatusCode,
			"body_preview", string(data[:min(len(data), 200)]))
		return fmt.Errorf("validation failed (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// POST /api/v1/credentials/newrelic
// Body: {"api_key":"NRAK-...","account_id":"12345","label":"My New Relic"}
func (s *Server) handleSaveNewRelicCredentials(w http.ResponseWriter, r *http.Request) {
	if s.connMgr == nil {
		jsonError(w, 503, "connections not configured")
		return
	}
	var req struct {
		APIKey    string `json:"api_key"`
		AccountID string `json:"account_id"`
		Label     string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if req.APIKey == "" {
		jsonError(w, 400, "api_key is required")
		return
	}
	if req.Label == "" {
		req.Label = "New Relic"
	}
	if err := validateNewRelicCredentials(r.Context(), req.APIKey, ""); err != nil {
		jsonError(w, 400, "credential validation failed: "+err.Error())
		return
	}
	meta := map[string]string{}
	if req.AccountID != "" {
		meta["account_id"] = req.AccountID
	}
	conn, err := s.connMgr.StoreAPIKeyConnection(
		connections.ProviderNewRelic, req.Label, meta,
		map[string]string{"api_key": req.APIKey},
	)
	if err != nil {
		jsonError(w, 500, "store credentials: "+err.Error())
		return
	}
	jsonOK(w, map[string]string{"id": conn.ID, "provider": string(conn.Provider), "account_label": conn.AccountLabel})
}

// POST /api/v1/credentials/newrelic/test
func (s *Server) handleTestNewRelicCredentials(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": "invalid JSON: " + err.Error()})
		return
	}
	if err := validateNewRelicCredentials(r.Context(), req.APIKey, ""); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

// elasticEncodeAPIKey returns the base64-encoded form of an Elastic API key.
// If the value contains a colon it is in "id:api_key" raw form and must be base64-encoded
// per the Elastic ApiKey authentication scheme. Pre-encoded keys are passed through unchanged.
func elasticEncodeAPIKey(apiKey string) string {
	if strings.Contains(apiKey, ":") {
		return base64.StdEncoding.EncodeToString([]byte(apiKey))
	}
	return apiKey
}

// validateElasticCredentials validates Elastic API key via the /_cluster/health endpoint.
func validateElasticCredentials(ctx context.Context, baseURL, apiKey string) error {
	if err := validateBaseURL(baseURL); err != nil {
		return fmt.Errorf("elastic: base URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/_cluster/health", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "ApiKey "+elasticEncodeAPIKey(apiKey))
	client := safeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		slog.Warn("elastic: credential validation error",
			"status", resp.StatusCode,
			"body_preview", string(body[:min(len(body), 200)]))
		return fmt.Errorf("validation failed (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// POST /api/v1/credentials/elastic
// Body: {"url":"https://my-cluster.es.us-east-1.aws.elastic.cloud:9243","api_key":"...","label":"My Elastic"}
func (s *Server) handleSaveElasticCredentials(w http.ResponseWriter, r *http.Request) {
	if s.connMgr == nil {
		jsonError(w, 503, "connections not configured")
		return
	}
	var req struct {
		URL    string `json:"url"`
		APIKey string `json:"api_key"`
		Label  string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if req.URL == "" {
		jsonError(w, 400, "url is required")
		return
	}
	if req.APIKey == "" {
		jsonError(w, 400, "api_key is required")
		return
	}
	if req.Label == "" {
		req.Label = "Elastic"
	}
	if err := validateElasticCredentials(r.Context(), req.URL, req.APIKey); err != nil {
		jsonError(w, 400, "credential validation failed: "+err.Error())
		return
	}
	conn, err := s.connMgr.StoreAPIKeyConnection(
		connections.ProviderElastic, req.Label,
		map[string]string{"url": req.URL},
		map[string]string{"api_key": req.APIKey},
	)
	if err != nil {
		jsonError(w, 500, "store credentials: "+err.Error())
		return
	}
	jsonOK(w, map[string]string{"id": conn.ID, "provider": string(conn.Provider), "account_label": conn.AccountLabel})
}

// POST /api/v1/credentials/elastic/test
func (s *Server) handleTestElasticCredentials(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL    string `json:"url"`
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": "invalid JSON: " + err.Error()})
		return
	}
	if err := validateElasticCredentials(r.Context(), req.URL, req.APIKey); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

// validateGrafanaCredentials validates Grafana token via the /api/org endpoint.
func validateGrafanaCredentials(ctx context.Context, baseURL, token string) error {
	if err := validateBaseURL(baseURL); err != nil {
		return fmt.Errorf("grafana: base URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/api/org", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := safeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		slog.Warn("grafana: credential validation error",
			"status", resp.StatusCode,
			"body_preview", string(body[:min(len(body), 200)]))
		return fmt.Errorf("validation failed (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// POST /api/v1/credentials/grafana
// Body: {"url":"https://myorg.grafana.net","token":"glsa_...","label":"My Grafana"}
func (s *Server) handleSaveGrafanaCredentials(w http.ResponseWriter, r *http.Request) {
	if s.connMgr == nil {
		jsonError(w, 503, "connections not configured")
		return
	}
	var req struct {
		URL   string `json:"url"`
		Token string `json:"token"`
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if req.URL == "" {
		jsonError(w, 400, "url is required")
		return
	}
	if req.Token == "" {
		jsonError(w, 400, "token is required")
		return
	}
	if req.Label == "" {
		req.Label = "Grafana"
	}
	if err := validateGrafanaCredentials(r.Context(), req.URL, req.Token); err != nil {
		jsonError(w, 400, "credential validation failed: "+err.Error())
		return
	}
	conn, err := s.connMgr.StoreAPIKeyConnection(
		connections.ProviderGrafana, req.Label,
		map[string]string{"url": req.URL},
		map[string]string{"token": req.Token},
	)
	if err != nil {
		jsonError(w, 500, "store credentials: "+err.Error())
		return
	}
	jsonOK(w, map[string]string{"id": conn.ID, "provider": string(conn.Provider), "account_label": conn.AccountLabel})
}

// POST /api/v1/credentials/grafana/test
func (s *Server) handleTestGrafanaCredentials(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": "invalid JSON: " + err.Error()})
		return
	}
	if err := validateGrafanaCredentials(r.Context(), req.URL, req.Token); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

// validateCrowdStrikeCredentials validates CrowdStrike credentials via OAuth2 token endpoint.
func validateCrowdStrikeCredentials(ctx context.Context, clientID, clientSecret, baseURL string) error {
	if baseURL == "" {
		baseURL = "https://api.crowdstrike.com"
	}
	body := strings.NewReader("client_id=" + url.QueryEscape(clientID) + "&client_secret=" + url.QueryEscape(clientSecret))
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/oauth2/token", body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := safeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 201 {
		slog.Warn("crowdstrike: credential validation error",
			"status", resp.StatusCode,
			"body_preview", string(data[:min(len(data), 200)]))
		return fmt.Errorf("validation failed (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// POST /api/v1/credentials/crowdstrike
// Body: {"client_id":"...","client_secret":"...","label":"My CrowdStrike"}
func (s *Server) handleSaveCrowdStrikeCredentials(w http.ResponseWriter, r *http.Request) {
	if s.connMgr == nil {
		jsonError(w, 503, "connections not configured")
		return
	}
	var req struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		Label        string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if req.ClientID == "" {
		jsonError(w, 400, "client_id is required")
		return
	}
	if req.ClientSecret == "" {
		jsonError(w, 400, "client_secret is required")
		return
	}
	if req.Label == "" {
		req.Label = "CrowdStrike"
	}
	if err := validateCrowdStrikeCredentials(r.Context(), req.ClientID, req.ClientSecret, ""); err != nil {
		jsonError(w, 400, "credential validation failed: "+err.Error())
		return
	}
	conn, err := s.connMgr.StoreAPIKeyConnection(
		connections.ProviderCrowdStrike, req.Label, nil,
		map[string]string{"client_id": req.ClientID, "client_secret": req.ClientSecret},
	)
	if err != nil {
		jsonError(w, 500, "store credentials: "+err.Error())
		return
	}
	jsonOK(w, map[string]string{"id": conn.ID, "provider": string(conn.Provider), "account_label": conn.AccountLabel})
}

// POST /api/v1/credentials/crowdstrike/test
func (s *Server) handleTestCrowdStrikeCredentials(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": "invalid JSON: " + err.Error()})
		return
	}
	if err := validateCrowdStrikeCredentials(r.Context(), req.ClientID, req.ClientSecret, ""); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

// validateTerraformCredentials validates Terraform Cloud token via /api/v2/account/details.
func validateTerraformCredentials(ctx context.Context, token, baseURL string) error {
	if baseURL == "" {
		baseURL = "https://app.terraform.io"
	}
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/api/v2/account/details", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/vnd.api+json")
	client := safeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		slog.Warn("terraform: credential validation error",
			"status", resp.StatusCode,
			"body_preview", string(body[:min(len(body), 200)]))
		return fmt.Errorf("validation failed (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// POST /api/v1/credentials/terraform
// Body: {"token":"...","label":"My Terraform Cloud"}
func (s *Server) handleSaveTerraformCredentials(w http.ResponseWriter, r *http.Request) {
	if s.connMgr == nil {
		jsonError(w, 503, "connections not configured")
		return
	}
	var req struct {
		Token string `json:"token"`
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if req.Token == "" {
		jsonError(w, 400, "token is required")
		return
	}
	if req.Label == "" {
		req.Label = "Terraform Cloud"
	}
	if err := validateTerraformCredentials(r.Context(), req.Token, ""); err != nil {
		jsonError(w, 400, "credential validation failed: "+err.Error())
		return
	}
	conn, err := s.connMgr.StoreAPIKeyConnection(
		connections.ProviderTerraform, req.Label, nil,
		map[string]string{"token": req.Token},
	)
	if err != nil {
		jsonError(w, 500, "store credentials: "+err.Error())
		return
	}
	jsonOK(w, map[string]string{"id": conn.ID, "provider": string(conn.Provider), "account_label": conn.AccountLabel})
}

// POST /api/v1/credentials/terraform/test
func (s *Server) handleTestTerraformCredentials(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": "invalid JSON: " + err.Error()})
		return
	}
	if err := validateTerraformCredentials(r.Context(), req.Token, ""); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

// validateServiceNowCredentials validates ServiceNow credentials via basic auth.
func validateServiceNowCredentials(ctx context.Context, instanceURL, username, password string) error {
	if err := validateBaseURL(instanceURL); err != nil {
		return fmt.Errorf("servicenow: base URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", instanceURL+"/api/now/table/sys_user?sysparm_limit=1", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(username, password)
	req.Header.Set("Accept", "application/json")
	client := safeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		slog.Warn("servicenow: credential validation error",
			"status", resp.StatusCode,
			"body_preview", string(body[:min(len(body), 200)]))
		return fmt.Errorf("validation failed (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// POST /api/v1/credentials/servicenow
// Body: {"instance_url":"https://dev12345.service-now.com","username":"admin","password":"...","label":"My ServiceNow"}
func (s *Server) handleSaveServiceNowCredentials(w http.ResponseWriter, r *http.Request) {
	if s.connMgr == nil {
		jsonError(w, 503, "connections not configured")
		return
	}
	var req struct {
		InstanceURL string `json:"instance_url"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		Label       string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if req.InstanceURL == "" {
		jsonError(w, 400, "instance_url is required")
		return
	}
	if req.Username == "" {
		jsonError(w, 400, "username is required")
		return
	}
	if req.Password == "" {
		jsonError(w, 400, "password is required")
		return
	}
	if req.Label == "" {
		req.Label = "ServiceNow"
	}
	if err := validateServiceNowCredentials(r.Context(), req.InstanceURL, req.Username, req.Password); err != nil {
		jsonError(w, 400, "credential validation failed: "+err.Error())
		return
	}
	conn, err := s.connMgr.StoreAPIKeyConnection(
		connections.ProviderServiceNow, req.Label,
		map[string]string{"instance_url": req.InstanceURL, "username": req.Username},
		map[string]string{"password": req.Password},
	)
	if err != nil {
		jsonError(w, 500, "store credentials: "+err.Error())
		return
	}
	jsonOK(w, map[string]string{"id": conn.ID, "provider": string(conn.Provider), "account_label": conn.AccountLabel})
}

// POST /api/v1/credentials/servicenow/test
func (s *Server) handleTestServiceNowCredentials(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InstanceURL string `json:"instance_url"`
		Username    string `json:"username"`
		Password    string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": "invalid JSON: " + err.Error()})
		return
	}
	if err := validateServiceNowCredentials(r.Context(), req.InstanceURL, req.Username, req.Password); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

// validateNotionCredentials validates a Notion integration token via GET /v1/users/me.
func validateNotionCredentials(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.notion.com/v1/users/me", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Notion-Version", "2022-06-28")
	client := safeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		slog.Warn("notion: credential validation error",
			"status", resp.StatusCode,
			"body_preview", string(body[:min(len(body), 200)]))
		return fmt.Errorf("validation failed (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// POST /api/v1/credentials/notion
// Body: {"token":"secret_...","label":"My Notion"}
func (s *Server) handleSaveNotionCredentials(w http.ResponseWriter, r *http.Request) {
	if s.connMgr == nil {
		jsonError(w, 503, "connections not configured")
		return
	}
	var req struct {
		Token string `json:"token"`
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if req.Token == "" {
		jsonError(w, 400, "token is required")
		return
	}
	if req.Label == "" {
		req.Label = "Notion"
	}
	if err := validateNotionCredentials(r.Context(), req.Token); err != nil {
		jsonError(w, 400, "credential validation failed: "+err.Error())
		return
	}
	conn, err := s.connMgr.StoreAPIKeyConnection(
		connections.ProviderNotion, req.Label, nil,
		map[string]string{"token": req.Token},
	)
	if err != nil {
		jsonError(w, 500, "store credentials: "+err.Error())
		return
	}
	jsonOK(w, map[string]string{"id": conn.ID, "provider": string(conn.Provider), "account_label": conn.AccountLabel})
}

// POST /api/v1/credentials/notion/test
func (s *Server) handleTestNotionCredentials(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": "invalid JSON: " + err.Error()})
		return
	}
	if err := validateNotionCredentials(r.Context(), req.Token); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

// validateAirtableCredentials validates an Airtable API key via GET /v0/meta/whoami.
func validateAirtableCredentials(ctx context.Context, apiKey string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.airtable.com/v0/meta/whoami", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	client := safeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		slog.Warn("airtable: credential validation error",
			"status", resp.StatusCode,
			"body_preview", string(body[:min(len(body), 200)]))
		return fmt.Errorf("validation failed (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// POST /api/v1/credentials/airtable
// Body: {"api_key":"...","label":"My Airtable"}
func (s *Server) handleSaveAirtableCredentials(w http.ResponseWriter, r *http.Request) {
	if s.connMgr == nil {
		jsonError(w, 503, "connections not configured")
		return
	}
	var req struct {
		APIKey string `json:"api_key"`
		Label  string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if req.APIKey == "" {
		jsonError(w, 400, "api_key is required")
		return
	}
	if req.Label == "" {
		req.Label = "Airtable"
	}
	if err := validateAirtableCredentials(r.Context(), req.APIKey); err != nil {
		jsonError(w, 400, "credential validation failed: "+err.Error())
		return
	}
	conn, err := s.connMgr.StoreAPIKeyConnection(
		connections.ProviderAirtable, req.Label, nil,
		map[string]string{"api_key": req.APIKey},
	)
	if err != nil {
		jsonError(w, 500, "store credentials: "+err.Error())
		return
	}
	jsonOK(w, map[string]string{"id": conn.ID, "provider": string(conn.Provider), "account_label": conn.AccountLabel})
}

// POST /api/v1/credentials/airtable/test
func (s *Server) handleTestAirtableCredentials(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": "invalid JSON: " + err.Error()})
		return
	}
	if err := validateAirtableCredentials(r.Context(), req.APIKey); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

// validateHubSpotCredentials validates a HubSpot private app token via GET /crm/v3/objects/contacts.
func validateHubSpotCredentials(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.hubapi.com/crm/v3/objects/contacts?limit=1", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := safeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		slog.Warn("hubspot: credential validation error",
			"status", resp.StatusCode,
			"body_preview", string(body[:min(len(body), 200)]))
		return fmt.Errorf("validation failed (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// POST /api/v1/credentials/hubspot
// Body: {"token":"...","label":"My HubSpot"}
func (s *Server) handleSaveHubSpotCredentials(w http.ResponseWriter, r *http.Request) {
	if s.connMgr == nil {
		jsonError(w, 503, "connections not configured")
		return
	}
	var req struct {
		Token string `json:"token"`
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if req.Token == "" {
		jsonError(w, 400, "token is required")
		return
	}
	if req.Label == "" {
		req.Label = "HubSpot"
	}
	if err := validateHubSpotCredentials(r.Context(), req.Token); err != nil {
		jsonError(w, 400, "credential validation failed: "+err.Error())
		return
	}
	conn, err := s.connMgr.StoreAPIKeyConnection(
		connections.ProviderHubSpot, req.Label, nil,
		map[string]string{"token": req.Token},
	)
	if err != nil {
		jsonError(w, 500, "store credentials: "+err.Error())
		return
	}
	jsonOK(w, map[string]string{"id": conn.ID, "provider": string(conn.Provider), "account_label": conn.AccountLabel})
}

// POST /api/v1/credentials/hubspot/test
func (s *Server) handleTestHubSpotCredentials(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": "invalid JSON: " + err.Error()})
		return
	}
	if err := validateHubSpotCredentials(r.Context(), req.Token); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

// validateZendeskCredentials validates Zendesk credentials via GET /api/v2/users/me using Basic Auth.
func validateZendeskCredentials(ctx context.Context, subdomain, email, token string) error {
	if err := validateSubdomain(subdomain); err != nil {
		return fmt.Errorf("zendesk: %w", err)
	}
	reqURL := fmt.Sprintf("https://%s.zendesk.com/api/v2/users/me", subdomain)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(email+"/token", token)
	req.Header.Set("Accept", "application/json")
	client := safeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		slog.Warn("zendesk: credential validation error",
			"status", resp.StatusCode,
			"body_preview", string(body[:min(len(body), 200)]))
		return fmt.Errorf("validation failed (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// POST /api/v1/credentials/zendesk
// Body: {"subdomain":"yourcompany","email":"user@example.com","token":"...","label":"My Zendesk"}
func (s *Server) handleSaveZendeskCredentials(w http.ResponseWriter, r *http.Request) {
	if s.connMgr == nil {
		jsonError(w, 503, "connections not configured")
		return
	}
	var req struct {
		Subdomain string `json:"subdomain"`
		Email     string `json:"email"`
		Token     string `json:"token"`
		Label     string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if req.Subdomain == "" {
		jsonError(w, 400, "subdomain is required")
		return
	}
	if req.Email == "" {
		jsonError(w, 400, "email is required")
		return
	}
	if req.Token == "" {
		jsonError(w, 400, "token is required")
		return
	}
	if req.Label == "" {
		req.Label = "Zendesk"
	}
	if err := validateZendeskCredentials(r.Context(), req.Subdomain, req.Email, req.Token); err != nil {
		jsonError(w, 400, "credential validation failed: "+err.Error())
		return
	}
	conn, err := s.connMgr.StoreAPIKeyConnection(
		connections.ProviderZendesk, req.Label,
		map[string]string{"subdomain": req.Subdomain, "email": req.Email},
		map[string]string{"token": req.Token},
	)
	if err != nil {
		jsonError(w, 500, "store credentials: "+err.Error())
		return
	}
	jsonOK(w, map[string]string{"id": conn.ID, "provider": string(conn.Provider), "account_label": conn.AccountLabel})
}

// POST /api/v1/credentials/zendesk/test
func (s *Server) handleTestZendeskCredentials(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Subdomain string `json:"subdomain"`
		Email     string `json:"email"`
		Token     string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": "invalid JSON: " + err.Error()})
		return
	}
	if err := validateZendeskCredentials(r.Context(), req.Subdomain, req.Email, req.Token); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

// validateAsanaCredentials validates an Asana personal access token via GET /api/1.0/users/me.
func validateAsanaCredentials(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://app.asana.com/api/1.0/users/me", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := safeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		slog.Warn("asana: credential validation error",
			"status", resp.StatusCode,
			"body_preview", string(body[:min(len(body), 200)]))
		return fmt.Errorf("validation failed (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// POST /api/v1/credentials/asana
// Body: {"token":"...","label":"My Asana"}
func (s *Server) handleSaveAsanaCredentials(w http.ResponseWriter, r *http.Request) {
	if s.connMgr == nil {
		jsonError(w, 503, "connections not configured")
		return
	}
	var req struct {
		Token string `json:"token"`
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if req.Token == "" {
		jsonError(w, 400, "token is required")
		return
	}
	if req.Label == "" {
		req.Label = "Asana"
	}
	if err := validateAsanaCredentials(r.Context(), req.Token); err != nil {
		jsonError(w, 400, "credential validation failed: "+err.Error())
		return
	}
	conn, err := s.connMgr.StoreAPIKeyConnection(
		connections.ProviderAsana, req.Label, nil,
		map[string]string{"token": req.Token},
	)
	if err != nil {
		jsonError(w, 500, "store credentials: "+err.Error())
		return
	}
	jsonOK(w, map[string]string{"id": conn.ID, "provider": string(conn.Provider), "account_label": conn.AccountLabel})
}

// POST /api/v1/credentials/asana/test
func (s *Server) handleTestAsanaCredentials(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": "invalid JSON: " + err.Error()})
		return
	}
	if err := validateAsanaCredentials(r.Context(), req.Token); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

// validateMondayCredentials validates a Monday.com API token via GraphQL POST /v2.
func validateMondayCredentials(ctx context.Context, token string) error {
	body := strings.NewReader(`{"query":"{ me { name } }"}`)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.monday.com/v2", body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")
	client := safeHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		slog.Warn("monday: credential validation error",
			"status", resp.StatusCode,
			"body_preview", string(data[:min(len(data), 200)]))
		return fmt.Errorf("validation failed (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// POST /api/v1/credentials/monday
// Body: {"token":"...","label":"My Monday.com"}
func (s *Server) handleSaveMondayCredentials(w http.ResponseWriter, r *http.Request) {
	if s.connMgr == nil {
		jsonError(w, 503, "connections not configured")
		return
	}
	var req struct {
		Token string `json:"token"`
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if req.Token == "" {
		jsonError(w, 400, "token is required")
		return
	}
	if req.Label == "" {
		req.Label = "Monday.com"
	}
	if err := validateMondayCredentials(r.Context(), req.Token); err != nil {
		jsonError(w, 400, "credential validation failed: "+err.Error())
		return
	}
	conn, err := s.connMgr.StoreAPIKeyConnection(
		connections.ProviderMonday, req.Label, nil,
		map[string]string{"token": req.Token},
	)
	if err != nil {
		jsonError(w, 500, "store credentials: "+err.Error())
		return
	}
	jsonOK(w, map[string]string{"id": conn.ID, "provider": string(conn.Provider), "account_label": conn.AccountLabel})
}

// POST /api/v1/credentials/monday/test
func (s *Server) handleTestMondayCredentials(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": "invalid JSON: " + err.Error()})
		return
	}
	if err := validateMondayCredentials(r.Context(), req.Token); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}
