package conntools_test

// tier2_success_test.go — Success-path HTTP tests for all 15 tier-2 providers.
// Each test spins up an httptest.Server, wires it via redirectingTransport,
// and asserts that the first/simplest tool for each provider returns a non-error
// result containing recognisable content from the mock response.
//
// NOTE: These tests mutate http.DefaultTransport via buildManagerWithAPIKeyCreds.
// Do NOT add t.Parallel() to any test in this file — parallel execution would
// race on the shared global and cause intermittent test failures.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/connections"
	conntools "github.com/scrypster/huginn/internal/connections/tools"
	"github.com/scrypster/huginn/internal/tools"
)

// buildManagerWithAPIKeyCreds creates a Manager with API key credentials stored
// and redirects all HTTP calls to serverURL via DefaultTransport.
func buildManagerWithAPIKeyCreds(t *testing.T, provider connections.Provider, connID, label string, metadata map[string]string, creds map[string]string, serverURL string) (*connections.Manager, []connections.Connection) {
	t.Helper()
	dir := t.TempDir()
	store, err := connections.NewStore(filepath.Join(dir, "conns.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	conn := connections.Connection{
		ID:           connID,
		Provider:     provider,
		AccountLabel: label,
		Metadata:     metadata,
	}
	if err := store.Add(conn); err != nil {
		t.Fatalf("store.Add: %v", err)
	}
	secrets := connections.NewMemoryStore()
	if err := secrets.StoreCredentials(connID, creds); err != nil {
		t.Fatalf("StoreCredentials: %v", err)
	}
	mgr := connections.NewManager(store, secrets, "http://localhost:9999/oauth/callback")

	orig := http.DefaultTransport
	http.DefaultTransport = &redirectingTransport{target: serverURL, inner: orig}
	t.Cleanup(func() { http.DefaultTransport = orig })

	return mgr, []connections.Connection{conn}
}

// ─── Datadog ──────────────────────────────────────────────────────────────────

func TestDatadog_QueryMetrics_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]any{
			"series": []map[string]any{
				{"metric": "system.cpu.user", "pointlist": [][]float64{{1700000000, 42.5}}},
			},
		})
	}))
	defer srv.Close()

	mgr, conns := buildManagerWithAPIKeyCreds(t,
		connections.ProviderDatadog, "dd-1", "prod",
		map[string]string{"url": srv.URL},
		map[string]string{"api_key": "test-api-key", "app_key": "test-app-key"},
		srv.URL,
	)

	reg := tools.NewRegistry()
	if err := conntools.RegisterForProvider(reg, mgr, connections.ProviderDatadog, conns); err != nil {
		t.Fatalf("RegisterForProvider: %v", err)
	}
	tool, ok := reg.Get("datadog_query_metrics")
	if !ok {
		t.Fatal("tool datadog_query_metrics not registered")
	}

	result := tool.Execute(context.Background(), map[string]any{
		"query": "avg:system.cpu.user{*}",
		"from":  float64(1700000000),
		"to":    float64(1700003600),
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "system.cpu.user") {
		t.Errorf("expected output to contain metric name, got: %s", result.Output)
	}
}

// ─── Splunk ───────────────────────────────────────────────────────────────────

func TestSplunk_Search_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]any{
			"results": []map[string]any{
				{"_raw": "error in main.go line 42", "index": "main"},
			},
		})
	}))
	defer srv.Close()

	mgr, conns := buildManagerWithAPIKeyCreds(t,
		connections.ProviderSplunk, "splunk-1", "prod",
		map[string]string{"url": srv.URL},
		map[string]string{"token": "test-splunk-token"},
		srv.URL,
	)

	reg := tools.NewRegistry()
	if err := conntools.RegisterForProvider(reg, mgr, connections.ProviderSplunk, conns); err != nil {
		t.Fatalf("RegisterForProvider: %v", err)
	}
	tool, ok := reg.Get("splunk_search")
	if !ok {
		t.Fatal("tool splunk_search not registered")
	}

	result := tool.Execute(context.Background(), map[string]any{
		"query": "index=main error",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "error in main.go") {
		t.Errorf("expected output to contain event data, got: %s", result.Output)
	}
}

// ─── Elastic ──────────────────────────────────────────────────────────────────

func TestElastic_ClusterHealth_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]any{
			"cluster_name": "my-cluster",
			"status":       "green",
			"number_of_nodes": 3,
		})
	}))
	defer srv.Close()

	mgr, conns := buildManagerWithAPIKeyCreds(t,
		connections.ProviderElastic, "elastic-1", "prod",
		map[string]string{"url": srv.URL},
		map[string]string{"api_key": "test-elastic-key"},
		srv.URL,
	)

	reg := tools.NewRegistry()
	if err := conntools.RegisterForProvider(reg, mgr, connections.ProviderElastic, conns); err != nil {
		t.Fatalf("RegisterForProvider: %v", err)
	}
	tool, ok := reg.Get("elastic_cluster_health")
	if !ok {
		t.Fatal("tool elastic_cluster_health not registered")
	}

	result := tool.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "my-cluster") {
		t.Errorf("expected output to contain cluster name, got: %s", result.Output)
	}
}

// ─── Grafana ──────────────────────────────────────────────────────────────────

func TestGrafana_ListDashboards_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, []map[string]any{
			{"uid": "dash-abc", "title": "My Dashboard", "type": "dash-db"},
		})
	}))
	defer srv.Close()

	mgr, conns := buildManagerWithAPIKeyCreds(t,
		connections.ProviderGrafana, "grafana-1", "prod",
		map[string]string{"url": srv.URL},
		map[string]string{"token": "test-grafana-token"},
		srv.URL,
	)

	reg := tools.NewRegistry()
	if err := conntools.RegisterForProvider(reg, mgr, connections.ProviderGrafana, conns); err != nil {
		t.Fatalf("RegisterForProvider: %v", err)
	}
	tool, ok := reg.Get("grafana_list_dashboards")
	if !ok {
		t.Fatal("tool grafana_list_dashboards not registered")
	}

	result := tool.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "My Dashboard") {
		t.Errorf("expected output to contain dashboard title, got: %s", result.Output)
	}
}

// ─── PagerDuty ────────────────────────────────────────────────────────────────

func TestPagerDuty_ListIncidents_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]any{
			"incidents": []map[string]any{
				{"id": "P123456", "title": "Database down", "status": "triggered"},
			},
			"total": 1,
		})
	}))
	defer srv.Close()

	mgr, conns := buildManagerWithAPIKeyCreds(t,
		connections.ProviderPagerDuty, "pd-1", "prod",
		map[string]string{},
		map[string]string{"api_token": "test-pd-token"},
		srv.URL,
	)

	reg := tools.NewRegistry()
	if err := conntools.RegisterForProvider(reg, mgr, connections.ProviderPagerDuty, conns); err != nil {
		t.Fatalf("RegisterForProvider: %v", err)
	}
	tool, ok := reg.Get("pagerduty_list_incidents")
	if !ok {
		t.Fatal("tool pagerduty_list_incidents not registered")
	}

	result := tool.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "Database down") {
		t.Errorf("expected output to contain incident title, got: %s", result.Output)
	}
}

// ─── New Relic ────────────────────────────────────────────────────────────────

func TestNewRelic_QueryNRQL_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]any{
			"data": map[string]any{
				"actor": map[string]any{
					"account": map[string]any{
						"nrql": map[string]any{
							"results": []map[string]any{
								{"average.duration": 42.5},
							},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	mgr, conns := buildManagerWithAPIKeyCreds(t,
		connections.ProviderNewRelic, "nr-1", "prod",
		map[string]string{"account_id": "12345"},
		map[string]string{"api_key": "test-nr-key"},
		srv.URL,
	)

	reg := tools.NewRegistry()
	if err := conntools.RegisterForProvider(reg, mgr, connections.ProviderNewRelic, conns); err != nil {
		t.Fatalf("RegisterForProvider: %v", err)
	}
	tool, ok := reg.Get("newrelic_query_nrql")
	if !ok {
		t.Fatal("tool newrelic_query_nrql not registered")
	}

	result := tool.Execute(context.Background(), map[string]any{
		"account_id": float64(12345),
		"nrql":       "SELECT average(duration) FROM Transaction SINCE 1 hour ago",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "average.duration") {
		t.Errorf("expected output to contain query result, got: %s", result.Output)
	}
}

// ─── CrowdStrike ──────────────────────────────────────────────────────────────
// CrowdStrike makes TWO requests: first POST /oauth2/token, then the actual API.
// The mock server handles both paths.

func TestCrowdStrike_ListDetections_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			// CrowdStrike token endpoint expects HTTP 201
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"access_token":"mock-cs-token","token_type":"bearer","expires_in":1799}`))
			return
		}
		jsonResponse(w, 200, map[string]any{
			"meta": map[string]any{"query_time": 0.001},
			"resources": []string{"ldt:abc123:1234567890"},
		})
	}))
	defer srv.Close()

	mgr, conns := buildManagerWithAPIKeyCreds(t,
		connections.ProviderCrowdStrike, "cs-1", "prod",
		map[string]string{},
		map[string]string{"client_id": "test-client-id", "client_secret": "test-client-secret"},
		srv.URL,
	)

	reg := tools.NewRegistry()
	if err := conntools.RegisterForProvider(reg, mgr, connections.ProviderCrowdStrike, conns); err != nil {
		t.Fatalf("RegisterForProvider: %v", err)
	}
	tool, ok := reg.Get("crowdstrike_list_detections")
	if !ok {
		t.Fatal("tool crowdstrike_list_detections not registered")
	}

	result := tool.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "resources") {
		t.Errorf("expected output to contain resources, got: %s", result.Output)
	}
}

// ─── Terraform ────────────────────────────────────────────────────────────────

func TestTerraform_ListWorkspaces_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]any{
			"data": []map[string]any{
				{
					"id":   "ws-abc123",
					"type": "workspaces",
					"attributes": map[string]any{
						"name": "production",
					},
				},
			},
		})
	}))
	defer srv.Close()

	mgr, conns := buildManagerWithAPIKeyCreds(t,
		connections.ProviderTerraform, "tf-1", "prod",
		map[string]string{"organization": "my-org"},
		map[string]string{"token": "test-tf-token"},
		srv.URL,
	)

	reg := tools.NewRegistry()
	if err := conntools.RegisterForProvider(reg, mgr, connections.ProviderTerraform, conns); err != nil {
		t.Fatalf("RegisterForProvider: %v", err)
	}
	tool, ok := reg.Get("terraform_list_workspaces")
	if !ok {
		t.Fatal("tool terraform_list_workspaces not registered")
	}

	result := tool.Execute(context.Background(), map[string]any{
		"organization": "my-org",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "production") {
		t.Errorf("expected output to contain workspace name, got: %s", result.Output)
	}
}

// ─── ServiceNow ───────────────────────────────────────────────────────────────

func TestServiceNow_ListIncidents_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]any{
			"result": []map[string]any{
				{
					"sys_id":            "abc123",
					"short_description": "Server unreachable",
					"state":             "1",
				},
			},
		})
	}))
	defer srv.Close()

	mgr, conns := buildManagerWithAPIKeyCreds(t,
		connections.ProviderServiceNow, "snow-1", "prod",
		map[string]string{"instance_url": srv.URL, "username": "admin"},
		map[string]string{"password": "test-password"},
		srv.URL,
	)

	reg := tools.NewRegistry()
	if err := conntools.RegisterForProvider(reg, mgr, connections.ProviderServiceNow, conns); err != nil {
		t.Fatalf("RegisterForProvider: %v", err)
	}
	tool, ok := reg.Get("servicenow_list_incidents")
	if !ok {
		t.Fatal("tool servicenow_list_incidents not registered")
	}

	result := tool.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "Server unreachable") {
		t.Errorf("expected output to contain incident description, got: %s", result.Output)
	}
}

// ─── Notion ───────────────────────────────────────────────────────────────────

func TestNotion_Search_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]any{
			"object": "list",
			"results": []map[string]any{
				{
					"object": "page",
					"id":     "page-abc123",
					"properties": map[string]any{
						"title": map[string]any{},
					},
				},
			},
			"has_more": false,
		})
	}))
	defer srv.Close()

	mgr, conns := buildManagerWithAPIKeyCreds(t,
		connections.ProviderNotion, "notion-1", "prod",
		map[string]string{},
		map[string]string{"token": "test-notion-token"},
		srv.URL,
	)

	reg := tools.NewRegistry()
	if err := conntools.RegisterForProvider(reg, mgr, connections.ProviderNotion, conns); err != nil {
		t.Fatalf("RegisterForProvider: %v", err)
	}
	tool, ok := reg.Get("notion_search")
	if !ok {
		t.Fatal("tool notion_search not registered")
	}

	result := tool.Execute(context.Background(), map[string]any{
		"query": "meeting notes",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "page-abc123") {
		t.Errorf("expected output to contain page id, got: %s", result.Output)
	}
}

// ─── Airtable ─────────────────────────────────────────────────────────────────

func TestAirtable_ListBases_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]any{
			"bases": []map[string]any{
				{"id": "appABC123", "name": "Project Tracker", "permissionLevel": "create"},
			},
		})
	}))
	defer srv.Close()

	mgr, conns := buildManagerWithAPIKeyCreds(t,
		connections.ProviderAirtable, "airtable-1", "prod",
		map[string]string{},
		map[string]string{"api_key": "test-airtable-key"},
		srv.URL,
	)

	reg := tools.NewRegistry()
	if err := conntools.RegisterForProvider(reg, mgr, connections.ProviderAirtable, conns); err != nil {
		t.Fatalf("RegisterForProvider: %v", err)
	}
	tool, ok := reg.Get("airtable_list_bases")
	if !ok {
		t.Fatal("tool airtable_list_bases not registered")
	}

	result := tool.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "Project Tracker") {
		t.Errorf("expected output to contain base name, got: %s", result.Output)
	}
}

// ─── HubSpot ──────────────────────────────────────────────────────────────────

func TestHubSpot_ListContacts_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]any{
			"results": []map[string]any{
				{
					"id": "101",
					"properties": map[string]any{
						"email":     "alice@example.com",
						"firstname": "Alice",
						"lastname":  "Smith",
					},
				},
			},
			"total": 1,
		})
	}))
	defer srv.Close()

	mgr, conns := buildManagerWithAPIKeyCreds(t,
		connections.ProviderHubSpot, "hs-1", "prod",
		map[string]string{},
		map[string]string{"token": "test-hs-token"},
		srv.URL,
	)

	reg := tools.NewRegistry()
	if err := conntools.RegisterForProvider(reg, mgr, connections.ProviderHubSpot, conns); err != nil {
		t.Fatalf("RegisterForProvider: %v", err)
	}
	tool, ok := reg.Get("hubspot_list_contacts")
	if !ok {
		t.Fatal("tool hubspot_list_contacts not registered")
	}

	result := tool.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "alice@example.com") {
		t.Errorf("expected output to contain contact email, got: %s", result.Output)
	}
}

// ─── Zendesk ──────────────────────────────────────────────────────────────────
// Zendesk builds URLs using the subdomain: https://<subdomain>.zendesk.com/...
// The redirectingTransport rewrites the entire host, so the subdomain is ignored.

func TestZendesk_ListTickets_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]any{
			"tickets": []map[string]any{
				{"id": 1, "subject": "Login broken", "status": "open"},
			},
			"count": 1,
		})
	}))
	defer srv.Close()

	mgr, conns := buildManagerWithAPIKeyCreds(t,
		connections.ProviderZendesk, "zd-1", "prod",
		map[string]string{"subdomain": "mycompany", "email": "admin@mycompany.com"},
		map[string]string{"token": "test-zd-token"},
		srv.URL,
	)

	reg := tools.NewRegistry()
	if err := conntools.RegisterForProvider(reg, mgr, connections.ProviderZendesk, conns); err != nil {
		t.Fatalf("RegisterForProvider: %v", err)
	}
	tool, ok := reg.Get("zendesk_list_tickets")
	if !ok {
		t.Fatal("tool zendesk_list_tickets not registered")
	}

	result := tool.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "Login broken") {
		t.Errorf("expected output to contain ticket subject, got: %s", result.Output)
	}
}

// ─── Asana ────────────────────────────────────────────────────────────────────

func TestAsana_ListWorkspaces_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]any{
			"data": []map[string]any{
				{"gid": "12345678", "name": "My Workspace"},
			},
		})
	}))
	defer srv.Close()

	mgr, conns := buildManagerWithAPIKeyCreds(t,
		connections.ProviderAsana, "asana-1", "prod",
		map[string]string{},
		map[string]string{"token": "test-asana-token"},
		srv.URL,
	)

	reg := tools.NewRegistry()
	if err := conntools.RegisterForProvider(reg, mgr, connections.ProviderAsana, conns); err != nil {
		t.Fatalf("RegisterForProvider: %v", err)
	}
	tool, ok := reg.Get("asana_list_workspaces")
	if !ok {
		t.Fatal("tool asana_list_workspaces not registered")
	}

	result := tool.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "My Workspace") {
		t.Errorf("expected output to contain workspace name, got: %s", result.Output)
	}
}

// ─── Monday ───────────────────────────────────────────────────────────────────

func TestMonday_ListBoards_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]any{
			"data": map[string]any{
				"boards": []map[string]any{
					{"id": "111222333", "name": "Sprint Board", "description": "Q1 sprint"},
				},
			},
		})
	}))
	defer srv.Close()

	mgr, conns := buildManagerWithAPIKeyCreds(t,
		connections.ProviderMonday, "monday-1", "prod",
		map[string]string{},
		map[string]string{"token": "test-monday-token"},
		srv.URL,
	)

	reg := tools.NewRegistry()
	if err := conntools.RegisterForProvider(reg, mgr, connections.ProviderMonday, conns); err != nil {
		t.Fatalf("RegisterForProvider: %v", err)
	}
	tool, ok := reg.Get("monday_list_boards")
	if !ok {
		t.Fatal("tool monday_list_boards not registered")
	}

	result := tool.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "Sprint Board") {
		t.Errorf("expected output to contain board name, got: %s", result.Output)
	}
}
