package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/scrypster/huginn/internal/connections/catalog"
)

// buildCredentialValidatorRegistry constructs the process-wide registry that
// maps catalog provider IDs to their connectivity validators.
//
// Each wrapper explicitly extracts named keys from the fields map and passes
// them to the existing per-provider validate functions.  This makes the
// mapping clear, prevents silent zero-value misuse if a field is renamed in
// the catalog, and keeps the validate functions unchanged (no new deps).
//
// Providers whose authentication is handled outside this system (OAuth, SSH,
// databases, Muninn, and bespoke forms such as slack_bot, jira_sa, linear,
// gitlab, discord, vercel, and stripe) are intentionally absent.
func buildCredentialValidatorRegistry() *catalog.Registry {
	r := catalog.NewRegistry()

	// ── Observability ─────────────────────────────────────────────────────────

	r.Register("datadog", catalog.ValidatorFunc(func(ctx context.Context, f map[string]string) error {
		return validateDatadogCredentials(ctx, f["url"], f["api_key"], f["app_key"])
	}))

	r.Register("splunk", catalog.ValidatorFunc(func(ctx context.Context, f map[string]string) error {
		return validateSplunkCredentials(ctx, f["url"], f["token"])
	}))

	r.Register("pagerduty", catalog.ValidatorFunc(func(ctx context.Context, f map[string]string) error {
		return validatePagerDutyCredentials(ctx, f["api_token"], "")
	}))

	r.Register("newrelic", catalog.ValidatorFunc(func(ctx context.Context, f map[string]string) error {
		return validateNewRelicCredentials(ctx, f["api_key"], "")
	}))

	r.Register("elastic", catalog.ValidatorFunc(func(ctx context.Context, f map[string]string) error {
		return validateElasticCredentials(ctx, f["url"], f["api_key"])
	}))

	r.Register("grafana", catalog.ValidatorFunc(func(ctx context.Context, f map[string]string) error {
		return validateGrafanaCredentials(ctx, f["url"], f["token"])
	}))

	r.Register("crowdstrike", catalog.ValidatorFunc(func(ctx context.Context, f map[string]string) error {
		return validateCrowdStrikeCredentials(ctx, f["client_id"], f["client_secret"], "")
	}))

	// ── Cloud ─────────────────────────────────────────────────────────────────

	r.Register("terraform", catalog.ValidatorFunc(func(ctx context.Context, f map[string]string) error {
		return validateTerraformCredentials(ctx, f["token"], "")
	}))

	// ── Productivity ──────────────────────────────────────────────────────────

	r.Register("servicenow", catalog.ValidatorFunc(func(ctx context.Context, f map[string]string) error {
		return validateServiceNowCredentials(ctx, f["instance_url"], f["username"], f["password"])
	}))

	r.Register("notion", catalog.ValidatorFunc(func(ctx context.Context, f map[string]string) error {
		return validateNotionCredentials(ctx, f["token"])
	}))

	r.Register("airtable", catalog.ValidatorFunc(func(ctx context.Context, f map[string]string) error {
		return validateAirtableCredentials(ctx, f["api_key"])
	}))

	r.Register("hubspot", catalog.ValidatorFunc(func(ctx context.Context, f map[string]string) error {
		return validateHubSpotCredentials(ctx, f["token"])
	}))

	r.Register("zendesk", catalog.ValidatorFunc(func(ctx context.Context, f map[string]string) error {
		return validateZendeskCredentials(ctx, f["subdomain"], f["email"], f["token"])
	}))

	r.Register("asana", catalog.ValidatorFunc(func(ctx context.Context, f map[string]string) error {
		return validateAsanaCredentials(ctx, f["token"])
	}))

	r.Register("monday", catalog.ValidatorFunc(func(ctx context.Context, f map[string]string) error {
		return validateMondayCredentials(ctx, f["token"])
	}))

	return r
}

// ── Per-provider validate functions ───────────────────────────────────────────
// These are the network-level credential probes used by both the generic
// catalog handlers (via buildCredentialValidatorRegistry) and, historically,
// the now-deleted per-provider bespoke handlers.

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

// elasticEncodeAPIKey returns the base64-encoded form of an Elastic API key.
// If the value contains a colon it is in "id:api_key" raw form and must be
// base64-encoded per the Elastic ApiKey authentication scheme.
// Pre-encoded keys are passed through unchanged.
func elasticEncodeAPIKey(apiKey string) string {
	if strings.Contains(apiKey, ":") {
		return base64.StdEncoding.EncodeToString([]byte(apiKey))
	}
	return apiKey
}

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
