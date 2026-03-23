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

// hubspotDo performs an authenticated HubSpot API request.
func hubspotDo(ctx context.Context, method, apiURL, token string, body io.Reader) (string, error) {
	req, err := http.NewRequestWithContext(ctx, method, apiURL, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
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

// hubspotCreds extracts the token from a connection.
func hubspotCreds(mgr *connections.Manager, conn connections.Connection) (string, error) {
	creds, err := mgr.GetCredentials(conn.ID)
	if err != nil {
		return "", err
	}
	return creds["token"], nil
}

// --- hubspot_list_contacts ---

type hubspotListContactsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *hubspotListContactsTool) Name() string        { return "hubspot_list_contacts" }
func (t *hubspotListContactsTool) Description() string { return "List HubSpot contacts." }
func (t *hubspotListContactsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *hubspotListContactsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "hubspot_list_contacts",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"limit":      {Type: "integer", Description: "Maximum number of results (default 10)"},
					"properties": {Type: "string", Description: "Comma-separated list of properties to return"},
				},
			},
		},
	}
}
func (t *hubspotListContactsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := hubspotCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "hubspot_list_contacts: auth: " + err.Error()}
	}

	limit := int(floatArg(args, "limit"))
	if limit <= 0 {
		limit = 10
	}

	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	if props, ok := args["properties"].(string); ok && props != "" {
		params.Set("properties", props)
	}

	apiURL := "https://api.hubapi.com/crm/v3/objects/contacts?" + params.Encode()
	out, err := hubspotDo(ctx, "GET", apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- hubspot_get_contact ---

type hubspotGetContactTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *hubspotGetContactTool) Name() string        { return "hubspot_get_contact" }
func (t *hubspotGetContactTool) Description() string { return "Get a HubSpot contact by ID." }
func (t *hubspotGetContactTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *hubspotGetContactTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "hubspot_get_contact",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"contact_id"},
				Properties: map[string]backend.ToolProperty{
					"contact_id": {Type: "string", Description: "The HubSpot contact ID"},
				},
			},
		},
	}
}
func (t *hubspotGetContactTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := hubspotCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "hubspot_get_contact: auth: " + err.Error()}
	}
	contactID, ok := args["contact_id"].(string)
	if !ok || contactID == "" {
		return tools.ToolResult{IsError: true, Error: "hubspot_get_contact: contact_id is required"}
	}
	apiURL := fmt.Sprintf("https://api.hubapi.com/crm/v3/objects/contacts/%s", contactID)
	out, err := hubspotDo(ctx, "GET", apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- hubspot_list_companies ---

type hubspotListCompaniesTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *hubspotListCompaniesTool) Name() string        { return "hubspot_list_companies" }
func (t *hubspotListCompaniesTool) Description() string { return "List HubSpot companies." }
func (t *hubspotListCompaniesTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *hubspotListCompaniesTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "hubspot_list_companies",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"limit": {Type: "integer", Description: "Maximum number of results (default 10)"},
				},
			},
		},
	}
}
func (t *hubspotListCompaniesTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := hubspotCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "hubspot_list_companies: auth: " + err.Error()}
	}

	limit := int(floatArg(args, "limit"))
	if limit <= 0 {
		limit = 10
	}

	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	apiURL := "https://api.hubapi.com/crm/v3/objects/companies?" + params.Encode()
	out, err := hubspotDo(ctx, "GET", apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- hubspot_list_deals ---

type hubspotListDealsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *hubspotListDealsTool) Name() string        { return "hubspot_list_deals" }
func (t *hubspotListDealsTool) Description() string { return "List HubSpot deals." }
func (t *hubspotListDealsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *hubspotListDealsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "hubspot_list_deals",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"limit": {Type: "integer", Description: "Maximum number of results (default 10)"},
				},
			},
		},
	}
}
func (t *hubspotListDealsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := hubspotCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "hubspot_list_deals: auth: " + err.Error()}
	}

	limit := int(floatArg(args, "limit"))
	if limit <= 0 {
		limit = 10
	}

	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	apiURL := "https://api.hubapi.com/crm/v3/objects/deals?" + params.Encode()
	out, err := hubspotDo(ctx, "GET", apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- hubspot_create_contact ---

type hubspotCreateContactTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *hubspotCreateContactTool) Name() string        { return "hubspot_create_contact" }
func (t *hubspotCreateContactTool) Description() string { return "Create a new HubSpot contact. Requires user approval." }
func (t *hubspotCreateContactTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *hubspotCreateContactTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "hubspot_create_contact",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"email"},
				Properties: map[string]backend.ToolProperty{
					"email":     {Type: "string", Description: "Contact email address"},
					"firstname": {Type: "string", Description: "Contact first name"},
					"lastname":  {Type: "string", Description: "Contact last name"},
					"phone":     {Type: "string", Description: "Contact phone number"},
				},
			},
		},
	}
}
func (t *hubspotCreateContactTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := hubspotCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "hubspot_create_contact: auth: " + err.Error()}
	}

	email, _ := args["email"].(string)
	if email == "" {
		return tools.ToolResult{IsError: true, Error: "hubspot_create_contact: email is required"}
	}

	properties := map[string]any{
		"email": email,
	}
	if firstname, ok := args["firstname"].(string); ok && firstname != "" {
		properties["firstname"] = firstname
	}
	if lastname, ok := args["lastname"].(string); ok && lastname != "" {
		properties["lastname"] = lastname
	}
	if phone, ok := args["phone"].(string); ok && phone != "" {
		properties["phone"] = phone
	}

	payload := map[string]any{"properties": properties}
	bodyBytes, _ := json.Marshal(payload)
	out, err := hubspotDo(ctx, "POST", "https://api.hubapi.com/crm/v3/objects/contacts", token, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerHubSpotTools registers all 5 HubSpot tools.
func registerHubSpotTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{
		"hubspot_list_contacts", "hubspot_get_contact", "hubspot_list_companies",
		"hubspot_list_deals", "hubspot_create_contact",
	}
	for _, n := range names {
		reg.Unregister(n)
	}
	strictInject(reg, &hubspotListContactsTool{mgr: mgr, conns: conns})
	strictInject(reg, &hubspotGetContactTool{mgr: mgr, conns: conns})
	strictInject(reg, &hubspotListCompaniesTool{mgr: mgr, conns: conns})
	strictInject(reg, &hubspotListDealsTool{mgr: mgr, conns: conns})
	strictInject(reg, &hubspotCreateContactTool{mgr: mgr, conns: conns})
	return nil
}
