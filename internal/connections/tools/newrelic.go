package conntools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/tools"
)

// newrelicDo performs a GraphQL query against New Relic NerdGraph API.
func newrelicDo(ctx context.Context, apiKey, query string, variables map[string]any) (string, error) {
	bodyData := map[string]any{"query": query}
	if variables != nil {
		bodyData["variables"] = variables
	}
	bodyBytes, _ := json.Marshal(bodyData)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.newrelic.com/graphql", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Api-Key", apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("network: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, data)
	}
	return string(data), nil
}

// newrelicCreds extracts the API key and account ID from a connection.
func newrelicCreds(mgr *connections.Manager, conn connections.Connection) (apiKey, accountID string, err error) {
	creds, err := mgr.GetCredentials(conn.ID)
	if err != nil {
		return "", "", err
	}
	return creds["api_key"], conn.Metadata["account_id"], nil
}

// --- newrelic_query_nrql ---

type newrelicQueryNRQLTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *newrelicQueryNRQLTool) Name() string        { return "newrelic_query_nrql" }
func (t *newrelicQueryNRQLTool) Description() string { return "Run a NRQL query against New Relic." }
func (t *newrelicQueryNRQLTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *newrelicQueryNRQLTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "newrelic_query_nrql",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"account_id", "nrql"},
				Properties: map[string]backend.ToolProperty{
					"account_id": {Type: "integer", Description: "New Relic account ID"},
					"nrql":       {Type: "string", Description: "The NRQL query to execute"},
				},
			},
		},
	}
}
func (t *newrelicQueryNRQLTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	apiKey, defaultAcctID, err := newrelicCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "newrelic_query_nrql: auth: " + err.Error()}
	}

	acctIDFloat, _ := args["account_id"].(float64)
	acctID := int(acctIDFloat)
	if acctID == 0 && defaultAcctID != "" {
		acctID, _ = strconv.Atoi(strings.TrimSpace(defaultAcctID))
	}
	if acctID == 0 {
		return tools.ToolResult{IsError: true, Error: "newrelic_query_nrql: account_id is required"}
	}

	nrql, _ := args["nrql"].(string)
	if nrql == "" {
		return tools.ToolResult{IsError: true, Error: "newrelic_query_nrql: nrql is required"}
	}

	query := fmt.Sprintf(`query($accountId:Int!,$nrql:Nrql!){actor{account(id:$accountId){nrql(query:$nrql){results}}}}`)
	variables := map[string]any{
		"accountId": acctID,
		"nrql":      nrql,
	}

	out, err := newrelicDo(ctx, apiKey, query, variables)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- newrelic_list_entities ---

type newrelicListEntitiesTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *newrelicListEntitiesTool) Name() string        { return "newrelic_list_entities" }
func (t *newrelicListEntitiesTool) Description() string { return "List New Relic entities by name or type." }
func (t *newrelicListEntitiesTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *newrelicListEntitiesTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "newrelic_list_entities",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"name":        {Type: "string", Description: "Filter entities by name"},
					"entity_type": {Type: "string", Description: "Filter by entity type (e.g., APPLICATION, SERVICE, HOST)"},
				},
			},
		},
	}
}
func (t *newrelicListEntitiesTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	apiKey, _, err := newrelicCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "newrelic_list_entities: auth: " + err.Error()}
	}

	name, _ := args["name"].(string)
	entityType, _ := args["entity_type"].(string)

	// Build NerdGraph entitySearch query string
	var parts []string
	if name != "" {
		parts = append(parts, fmt.Sprintf("name LIKE '%%%s%%'", strings.ReplaceAll(name, "'", "''")))
	}
	if entityType != "" {
		parts = append(parts, fmt.Sprintf("type = '%s'", strings.ReplaceAll(entityType, "'", "''")))
	}
	searchQuery := strings.Join(parts, " AND ")

	gql := `query($q:String){actor{entitySearch(query:$q){results{entities{guid name entityType alertSeverity}}}}}`
	variables := map[string]any{"q": searchQuery}

	out, err := newrelicDo(ctx, apiKey, gql, variables)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- newrelic_get_entity ---

type newrelicGetEntityTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *newrelicGetEntityTool) Name() string        { return "newrelic_get_entity" }
func (t *newrelicGetEntityTool) Description() string { return "Get details of a New Relic entity by GUID." }
func (t *newrelicGetEntityTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *newrelicGetEntityTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "newrelic_get_entity",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"guid"},
				Properties: map[string]backend.ToolProperty{
					"guid": {Type: "string", Description: "The entity GUID"},
				},
			},
		},
	}
}
func (t *newrelicGetEntityTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	apiKey, _, err := newrelicCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "newrelic_get_entity: auth: " + err.Error()}
	}

	guid, _ := args["guid"].(string)
	if guid == "" {
		return tools.ToolResult{IsError: true, Error: "newrelic_get_entity: guid is required"}
	}

	query := fmt.Sprintf(`query($guid:EntityGuid!){actor{entity(guid:$guid){guid name entityType ... on ApmApplicationEntity{runningAgentVersions{minVersion}}}}}`)
	variables := map[string]any{
		"guid": guid,
	}

	out, err := newrelicDo(ctx, apiKey, query, variables)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- newrelic_list_alert_violations ---

type newrelicListAlertViolationsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *newrelicListAlertViolationsTool) Name() string        { return "newrelic_list_alert_violations" }
func (t *newrelicListAlertViolationsTool) Description() string { return "List New Relic alert violations." }
func (t *newrelicListAlertViolationsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *newrelicListAlertViolationsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "newrelic_list_alert_violations",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"account_id"},
				Properties: map[string]backend.ToolProperty{
					"account_id": {Type: "integer", Description: "New Relic account ID"},
				},
			},
		},
	}
}
func (t *newrelicListAlertViolationsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	apiKey, defaultAcctID, err := newrelicCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "newrelic_list_alert_violations: auth: " + err.Error()}
	}

	acctIDFloat, _ := args["account_id"].(float64)
	acctID := int(acctIDFloat)
	if acctID == 0 && defaultAcctID != "" {
		acctID, _ = strconv.Atoi(strings.TrimSpace(defaultAcctID))
	}
	if acctID == 0 {
		return tools.ToolResult{IsError: true, Error: "newrelic_list_alert_violations: account_id is required"}
	}

	query := fmt.Sprintf(`query($accountId:Int!){actor{account(id:$accountId){alerts{nrqlConditions{nextCursor nrqlConditions{id name enabled}}}}}}`)
	variables := map[string]any{
		"accountId": acctID,
	}

	out, err := newrelicDo(ctx, apiKey, query, variables)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- newrelic_list_deployments ---

type newrelicListDeploymentsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *newrelicListDeploymentsTool) Name() string        { return "newrelic_list_deployments" }
func (t *newrelicListDeploymentsTool) Description() string { return "List New Relic deployments for an application." }
func (t *newrelicListDeploymentsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *newrelicListDeploymentsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "newrelic_list_deployments",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"account_id"},
				Properties: map[string]backend.ToolProperty{
					"account_id": {Type: "integer", Description: "New Relic account ID"},
					"app_id":     {Type: "integer", Description: "Application ID (optional)"},
				},
			},
		},
	}
}
func (t *newrelicListDeploymentsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	apiKey, defaultAcctID, err := newrelicCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "newrelic_list_deployments: auth: " + err.Error()}
	}

	acctIDFloat, _ := args["account_id"].(float64)
	acctID := int(acctIDFloat)
	if acctID == 0 && defaultAcctID != "" {
		acctID, _ = strconv.Atoi(strings.TrimSpace(defaultAcctID))
	}
	if acctID == 0 {
		return tools.ToolResult{IsError: true, Error: "newrelic_list_deployments: account_id is required"}
	}

	appIDFloat, _ := args["app_id"].(float64)
	appID := int(appIDFloat)

	query := fmt.Sprintf(`query($accountId:Int!,$appId:Int){actor{account(id:$accountId){deployments(appId:$appId){results{version description timestamp}}}}}`)
	variables := map[string]any{
		"accountId": acctID,
	}
	if appID > 0 {
		variables["appId"] = appID
	}

	out, err := newrelicDo(ctx, apiKey, query, variables)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerNewRelicTools registers all 5 New Relic tools.
func registerNewRelicTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{
		"newrelic_query_nrql", "newrelic_list_entities", "newrelic_get_entity",
		"newrelic_list_alert_violations", "newrelic_list_deployments",
	}
	for _, n := range names {
		reg.Unregister(n)
	}
	strictInject(reg, &newrelicQueryNRQLTool{mgr: mgr, conns: conns})
	strictInject(reg, &newrelicListEntitiesTool{mgr: mgr, conns: conns})
	strictInject(reg, &newrelicGetEntityTool{mgr: mgr, conns: conns})
	strictInject(reg, &newrelicListAlertViolationsTool{mgr: mgr, conns: conns})
	strictInject(reg, &newrelicListDeploymentsTool{mgr: mgr, conns: conns})
	return nil
}
