package connections

import "time"

type Provider string

const (
	ProviderGoogle    Provider = "google"
	ProviderSlack     Provider = "slack"
	ProviderSlackUser Provider = "slack_user"
	ProviderJira      Provider = "jira"
	ProviderGitHub    Provider = "github"
	ProviderBitbucket Provider = "bitbucket"
	ProviderDatadog   Provider = "datadog"
	ProviderSplunk    Provider = "splunk"
	ProviderPagerDuty   Provider = "pagerduty"
	ProviderNewRelic    Provider = "newrelic"
	ProviderElastic     Provider = "elastic"
	ProviderGrafana     Provider = "grafana"
	ProviderCrowdStrike Provider = "crowdstrike"
	ProviderTerraform   Provider = "terraform"
	ProviderServiceNow  Provider = "servicenow"
	ProviderNotion      Provider = "notion"
	ProviderAirtable    Provider = "airtable"
	ProviderHubSpot     Provider = "hubspot"
	ProviderZendesk     Provider = "zendesk"
	ProviderAsana       Provider = "asana"
	ProviderMonday      Provider = "monday"
)

// ConnectionType identifies what kind of credentials a connection holds.
type ConnectionType string

const (
	// ConnectionTypeOAuth covers providers that use the OAuth broker flow
	// (GitHub, Google, Slack, Jira, Bitbucket, Azure).
	ConnectionTypeOAuth ConnectionType = "oauth"

	// ConnectionTypeAPIKey covers API key / bearer token credentials
	// (AWS IAM, OpenAI, Stripe, Twilio, generic bearer).
	ConnectionTypeAPIKey ConnectionType = "api_key"

	// ConnectionTypeServiceAccount covers service account JSON files
	// (GCP service accounts).
	ConnectionTypeServiceAccount ConnectionType = "service_account"

	// ConnectionTypeDatabase covers database connection strings
	// (Postgres, MySQL, MongoDB, Redis).
	ConnectionTypeDatabase ConnectionType = "database"

	// ConnectionTypeSSH covers SSH private key credentials
	// (private servers, private Git repos).
	ConnectionTypeSSH ConnectionType = "ssh"
)

type Connection struct {
	ID               string            `json:"id"`
	Provider         Provider          `json:"provider"`
	Type             ConnectionType    `json:"type,omitempty"`
	AccountLabel     string            `json:"account_label"`
	AccountID        string            `json:"account_id"`
	Scopes           []string          `json:"scopes"`
	CreatedAt        time.Time         `json:"created_at"`
	ExpiresAt        time.Time         `json:"expires_at"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	// RefreshFailedAt is set when the last proactive token refresh failed.
	// Nil means the last refresh succeeded (or no refresh has been attempted).
	RefreshFailedAt  *time.Time        `json:"refresh_failed_at,omitempty"`
	// LastRefreshError is the error message from the most recent failed refresh.
	// Empty string means no failure on record.
	LastRefreshError string            `json:"last_refresh_error,omitempty"`
}

type AccountInfo struct {
	ID          string
	Label       string
	DisplayName string
	AvatarURL   string
}
