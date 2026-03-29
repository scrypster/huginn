package server

import (
	"context"

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
