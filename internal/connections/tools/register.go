// Package conntools registers OAuth-backed integration tools into the tool registry.
// Tools are registered per-provider based on which connections exist in the store.
package conntools

import (
	"log/slog"

	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/tools"
)

// providerToolNames maps each provider to the tool names it registers.
// Must stay in sync with the registerXxxTools functions.
var providerToolNames = map[connections.Provider][]string{
	connections.ProviderGoogle: {"gmail_search", "gmail_read", "gmail_send"},
	connections.ProviderGitHub: {
		"github_list_prs", "github_get_pr", "github_create_issue",
		"github_search_code", "github_list_issues",
	},
	connections.ProviderSlack: {"slack_list_channels", "slack_read_channel", "slack_post_message"},
	connections.ProviderJira: {
		"jira_list_issues", "jira_get_issue", "jira_create_issue", "jira_update_issue",
	},
	connections.ProviderBitbucket: {"bitbucket_list_prs", "bitbucket_get_pr", "bitbucket_create_pr"},
	connections.ProviderDatadog: {
		"datadog_query_metrics", "datadog_search_logs", "datadog_list_monitors",
		"datadog_get_monitor", "datadog_create_monitor", "datadog_update_monitor",
		"datadog_mute_monitor", "datadog_list_dashboards", "datadog_query_events",
		"datadog_list_hosts", "datadog_list_slos",
	},
	connections.ProviderSplunk: {
		"splunk_search", "splunk_list_indexes", "splunk_list_saved_searches",
		"splunk_run_saved_search", "splunk_list_alerts", "splunk_list_dashboards",
	},
	connections.ProviderElastic: {
		"elastic_cluster_health", "elastic_list_indices", "elastic_search",
		"elastic_get_document", "elastic_index_document", "elastic_delete_document",
	},
	connections.ProviderGrafana: {
		"grafana_list_dashboards", "grafana_get_dashboard", "grafana_list_datasources",
		"grafana_list_alert_rules", "grafana_create_annotation",
	},
	connections.ProviderPagerDuty: {
		"pagerduty_list_incidents", "pagerduty_get_incident", "pagerduty_create_incident",
		"pagerduty_update_incident", "pagerduty_list_services", "pagerduty_list_on_calls",
	},
	connections.ProviderNewRelic: {
		"newrelic_query_nrql", "newrelic_list_entities", "newrelic_get_entity",
		"newrelic_list_alert_violations", "newrelic_list_deployments",
	},
	connections.ProviderCrowdStrike: {
		"crowdstrike_list_detections", "crowdstrike_get_detection", "crowdstrike_list_incidents",
		"crowdstrike_list_hosts", "crowdstrike_get_host",
	},
	connections.ProviderTerraform: {
		"terraform_list_workspaces", "terraform_get_workspace", "terraform_list_runs",
		"terraform_get_run", "terraform_trigger_run",
	},
	connections.ProviderServiceNow: {
		"servicenow_list_incidents", "servicenow_get_incident", "servicenow_create_incident",
		"servicenow_update_incident", "servicenow_search_records",
	},
	connections.ProviderNotion: {
		"notion_search", "notion_get_page", "notion_get_database",
		"notion_query_database", "notion_create_page",
	},
	connections.ProviderAirtable: {
		"airtable_list_bases", "airtable_list_tables", "airtable_list_records",
		"airtable_get_record", "airtable_create_record",
	},
	connections.ProviderHubSpot: {
		"hubspot_list_contacts", "hubspot_get_contact", "hubspot_list_companies",
		"hubspot_list_deals", "hubspot_create_contact",
	},
	connections.ProviderZendesk: {
		"zendesk_list_tickets", "zendesk_get_ticket", "zendesk_list_users",
		"zendesk_create_ticket", "zendesk_update_ticket",
	},
	connections.ProviderAsana: {
		"asana_list_workspaces", "asana_list_projects", "asana_list_tasks",
		"asana_get_task", "asana_create_task",
	},
	connections.ProviderMonday: {
		"monday_list_boards", "monday_get_board", "monday_list_items",
		"monday_create_item", "monday_update_item",
	},
}

// RegisterAll registers integration tools for all providers with at least one connection.
// Safe to call multiple times — unregisters first (idempotent).
func RegisterAll(reg *tools.Registry, mgr *connections.Manager, store connections.StoreInterface) error {
	conns, err := store.List()
	if err != nil {
		return err
	}
	byProvider := make(map[connections.Provider][]connections.Connection)
	for _, c := range conns {
		byProvider[c.Provider] = append(byProvider[c.Provider], c)
	}
	for provider, providerConns := range byProvider {
		if err := RegisterForProvider(reg, mgr, provider, providerConns); err != nil {
			slog.Warn("integration tools: registration failed", "provider", provider, "err", err)
		}
	}
	return nil
}

// RegisterForProvider registers integration tools for a single provider.
func RegisterForProvider(reg *tools.Registry, mgr *connections.Manager, provider connections.Provider, conns []connections.Connection) error {
	var err error
	switch provider {
	case connections.ProviderGoogle:
		err = registerGmailTools(reg, mgr, conns)
	case connections.ProviderGitHub:
		err = registerGitHubTools(reg, mgr, conns)
	case connections.ProviderSlack:
		err = registerSlackTools(reg, mgr, conns)
	case connections.ProviderJira:
		err = registerJiraTools(reg, mgr, conns)
	case connections.ProviderBitbucket:
		err = registerBitbucketTools(reg, mgr, conns)
	case connections.ProviderDatadog:
		err = registerDatadogTools(reg, mgr, conns)
	case connections.ProviderSplunk:
		err = registerSplunkTools(reg, mgr, conns)
	case connections.ProviderElastic:
		err = registerElasticTools(reg, mgr, conns)
	case connections.ProviderGrafana:
		err = registerGrafanaTools(reg, mgr, conns)
	case connections.ProviderPagerDuty:
		err = registerPagerDutyTools(reg, mgr, conns)
	case connections.ProviderNewRelic:
		err = registerNewRelicTools(reg, mgr, conns)
	case connections.ProviderCrowdStrike:
		err = registerCrowdStrikeTools(reg, mgr, conns)
	case connections.ProviderTerraform:
		err = registerTerraformTools(reg, mgr, conns)
	case connections.ProviderServiceNow:
		err = registerServiceNowTools(reg, mgr, conns)
	case connections.ProviderNotion:
		err = registerNotionTools(reg, mgr, conns)
	case connections.ProviderAirtable:
		err = registerAirtableTools(reg, mgr, conns)
	case connections.ProviderHubSpot:
		err = registerHubSpotTools(reg, mgr, conns)
	case connections.ProviderZendesk:
		err = registerZendeskTools(reg, mgr, conns)
	case connections.ProviderAsana:
		err = registerAsanaTools(reg, mgr, conns)
	case connections.ProviderMonday:
		err = registerMondayTools(reg, mgr, conns)
	}
	if err != nil {
		return err
	}
	// Tag all tools for this provider so AllSchemasForProviders can filter them.
	if names, ok := providerToolNames[provider]; ok {
		reg.TagTools(names, string(provider))
	}
	return nil
}

// resolveConnection finds the right connection for an account label.
// Empty label returns the first (default) connection.
func resolveConnection(conns []connections.Connection, label string) connections.Connection {
	if label == "" || len(conns) == 1 {
		return conns[0]
	}
	for _, c := range conns {
		if c.AccountLabel == label {
			return c
		}
	}
	return conns[0]
}
