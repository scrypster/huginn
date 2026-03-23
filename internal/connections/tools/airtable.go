package conntools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/tools"
)

// airtableDo performs an authenticated Airtable API request.
func airtableDo(ctx context.Context, method, apiURL, apiKey string, body io.Reader) (string, error) {
	req, err := http.NewRequestWithContext(ctx, method, apiURL, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
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

// airtableCreds extracts the API key from a connection.
func airtableCreds(mgr *connections.Manager, conn connections.Connection) (string, error) {
	creds, err := mgr.GetCredentials(conn.ID)
	if err != nil {
		return "", err
	}
	return creds["api_key"], nil
}

// --- airtable_list_bases ---

type airtableListBasesTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *airtableListBasesTool) Name() string        { return "airtable_list_bases" }
func (t *airtableListBasesTool) Description() string { return "List all accessible Airtable bases." }
func (t *airtableListBasesTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *airtableListBasesTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "airtable_list_bases",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:       "object",
				Properties: map[string]backend.ToolProperty{},
			},
		},
	}
}
func (t *airtableListBasesTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	apiKey, err := airtableCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "airtable_list_bases: auth: " + err.Error()}
	}
	out, err := airtableDo(ctx, "GET", "https://api.airtable.com/v0/meta/bases", apiKey, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- airtable_list_tables ---

type airtableListTablesTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *airtableListTablesTool) Name() string        { return "airtable_list_tables" }
func (t *airtableListTablesTool) Description() string { return "List tables in an Airtable base." }
func (t *airtableListTablesTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *airtableListTablesTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "airtable_list_tables",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"base_id"},
				Properties: map[string]backend.ToolProperty{
					"base_id": {Type: "string", Description: "The Airtable base ID"},
				},
			},
		},
	}
}
func (t *airtableListTablesTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	apiKey, err := airtableCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "airtable_list_tables: auth: " + err.Error()}
	}
	baseID, ok := args["base_id"].(string)
	if !ok || baseID == "" {
		return tools.ToolResult{IsError: true, Error: "airtable_list_tables: base_id is required"}
	}
	apiURL := fmt.Sprintf("https://api.airtable.com/v0/meta/bases/%s/tables", baseID)
	out, err := airtableDo(ctx, "GET", apiURL, apiKey, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- airtable_list_records ---

type airtableListRecordsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *airtableListRecordsTool) Name() string        { return "airtable_list_records" }
func (t *airtableListRecordsTool) Description() string { return "List records in an Airtable table." }
func (t *airtableListRecordsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *airtableListRecordsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "airtable_list_records",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"base_id", "table_name"},
				Properties: map[string]backend.ToolProperty{
					"base_id":        {Type: "string", Description: "The Airtable base ID"},
					"table_name":     {Type: "string", Description: "The table name or ID"},
					"max_records":    {Type: "integer", Description: "Maximum number of records to return"},
					"filter_formula": {Type: "string", Description: "Airtable formula to filter records"},
				},
			},
		},
	}
}
func (t *airtableListRecordsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	apiKey, err := airtableCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "airtable_list_records: auth: " + err.Error()}
	}
	baseID, _ := args["base_id"].(string)
	tableName, _ := args["table_name"].(string)
	if baseID == "" || tableName == "" {
		return tools.ToolResult{IsError: true, Error: "airtable_list_records: base_id and table_name are required"}
	}

	params := url.Values{}
	if maxRecords := int(floatArg(args, "max_records")); maxRecords > 0 {
		params.Set("maxRecords", strconv.Itoa(maxRecords))
	}
	if formula, ok := args["filter_formula"].(string); ok && formula != "" {
		params.Set("filterByFormula", formula)
	}

	apiURL := fmt.Sprintf("https://api.airtable.com/v0/%s/%s", baseID, url.PathEscape(tableName))
	if len(params) > 0 {
		apiURL += "?" + params.Encode()
	}
	out, err := airtableDo(ctx, "GET", apiURL, apiKey, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- airtable_get_record ---

type airtableGetRecordTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *airtableGetRecordTool) Name() string        { return "airtable_get_record" }
func (t *airtableGetRecordTool) Description() string { return "Get a single Airtable record by ID." }
func (t *airtableGetRecordTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *airtableGetRecordTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "airtable_get_record",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"base_id", "table_name", "record_id"},
				Properties: map[string]backend.ToolProperty{
					"base_id":    {Type: "string", Description: "The Airtable base ID"},
					"table_name": {Type: "string", Description: "The table name or ID"},
					"record_id":  {Type: "string", Description: "The record ID"},
				},
			},
		},
	}
}
func (t *airtableGetRecordTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	apiKey, err := airtableCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "airtable_get_record: auth: " + err.Error()}
	}
	baseID, _ := args["base_id"].(string)
	tableName, _ := args["table_name"].(string)
	recordID, _ := args["record_id"].(string)
	if baseID == "" || tableName == "" || recordID == "" {
		return tools.ToolResult{IsError: true, Error: "airtable_get_record: base_id, table_name, and record_id are required"}
	}
	apiURL := fmt.Sprintf("https://api.airtable.com/v0/%s/%s/%s", baseID, url.PathEscape(tableName), recordID)
	out, err := airtableDo(ctx, "GET", apiURL, apiKey, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- airtable_create_record ---

type airtableCreateRecordTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *airtableCreateRecordTool) Name() string        { return "airtable_create_record" }
func (t *airtableCreateRecordTool) Description() string { return "Create a new record in an Airtable table. Requires user approval." }
func (t *airtableCreateRecordTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *airtableCreateRecordTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "airtable_create_record",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"base_id", "table_name", "fields"},
				Properties: map[string]backend.ToolProperty{
					"base_id":    {Type: "string", Description: "The Airtable base ID"},
					"table_name": {Type: "string", Description: "The table name or ID"},
					"fields":     {Type: "object", Description: "Record fields as key-value pairs"},
				},
			},
		},
	}
}
func (t *airtableCreateRecordTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	apiKey, err := airtableCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "airtable_create_record: auth: " + err.Error()}
	}
	baseID, _ := args["base_id"].(string)
	tableName, _ := args["table_name"].(string)
	fields := args["fields"]
	if baseID == "" || tableName == "" || fields == nil {
		return tools.ToolResult{IsError: true, Error: "airtable_create_record: base_id, table_name, and fields are required"}
	}

	payload := map[string]any{
		"fields": fields,
	}
	bodyBytes, _ := json.Marshal(payload)
	apiURL := fmt.Sprintf("https://api.airtable.com/v0/%s/%s", baseID, url.PathEscape(tableName))
	out, err := airtableDo(ctx, "POST", apiURL, apiKey, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerAirtableTools registers all 5 Airtable tools.
func registerAirtableTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{
		"airtable_list_bases", "airtable_list_tables", "airtable_list_records",
		"airtable_get_record", "airtable_create_record",
	}
	for _, n := range names {
		reg.Unregister(n)
	}
	strictInject(reg, &airtableListBasesTool{mgr: mgr, conns: conns})
	strictInject(reg, &airtableListTablesTool{mgr: mgr, conns: conns})
	strictInject(reg, &airtableListRecordsTool{mgr: mgr, conns: conns})
	strictInject(reg, &airtableGetRecordTool{mgr: mgr, conns: conns})
	strictInject(reg, &airtableCreateRecordTool{mgr: mgr, conns: conns})
	return nil
}
