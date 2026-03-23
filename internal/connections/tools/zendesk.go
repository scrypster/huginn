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

// zendeskDo performs an authenticated Zendesk API request using Basic Auth with email/token scheme.
func zendeskDo(ctx context.Context, method, apiURL, email, token string, body io.Reader) (string, error) {
	req, err := http.NewRequestWithContext(ctx, method, apiURL, body)
	if err != nil {
		return "", err
	}
	// Zendesk API token auth: email/token:{token}
	req.SetBasicAuth(email+"/token", token)
	req.Header.Set("Accept", "application/json")
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

// zendeskCreds extracts subdomain, email, and token from a connection.
func zendeskCreds(mgr *connections.Manager, conn connections.Connection) (subdomain, email, token string, err error) {
	creds, err := mgr.GetCredentials(conn.ID)
	if err != nil {
		return "", "", "", err
	}
	return conn.Metadata["subdomain"], conn.Metadata["email"], creds["token"], nil
}

// --- zendesk_list_tickets ---

type zendeskListTicketsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *zendeskListTicketsTool) Name() string        { return "zendesk_list_tickets" }
func (t *zendeskListTicketsTool) Description() string { return "List Zendesk tickets." }
func (t *zendeskListTicketsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *zendeskListTicketsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "zendesk_list_tickets",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"limit": {Type: "integer", Description: "Maximum number of results (default 25)"},
				},
			},
		},
	}
}
func (t *zendeskListTicketsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	subdomain, email, token, err := zendeskCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "zendesk_list_tickets: auth: " + err.Error()}
	}

	limit := int(floatArg(args, "limit"))
	if limit <= 0 {
		limit = 25
	}

	params := url.Values{}
	params.Set("per_page", strconv.Itoa(limit))
	apiURL := fmt.Sprintf("https://%s.zendesk.com/api/v2/tickets?%s", subdomain, params.Encode())
	out, err := zendeskDo(ctx, "GET", apiURL, email, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- zendesk_get_ticket ---

type zendeskGetTicketTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *zendeskGetTicketTool) Name() string        { return "zendesk_get_ticket" }
func (t *zendeskGetTicketTool) Description() string { return "Get a Zendesk ticket by ID." }
func (t *zendeskGetTicketTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *zendeskGetTicketTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "zendesk_get_ticket",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"ticket_id"},
				Properties: map[string]backend.ToolProperty{
					"ticket_id": {Type: "string", Description: "The Zendesk ticket ID"},
				},
			},
		},
	}
}
func (t *zendeskGetTicketTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	subdomain, email, token, err := zendeskCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "zendesk_get_ticket: auth: " + err.Error()}
	}
	ticketID, ok := args["ticket_id"].(string)
	if !ok || ticketID == "" {
		return tools.ToolResult{IsError: true, Error: "zendesk_get_ticket: ticket_id is required"}
	}
	apiURL := fmt.Sprintf("https://%s.zendesk.com/api/v2/tickets/%s", subdomain, ticketID)
	out, err := zendeskDo(ctx, "GET", apiURL, email, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- zendesk_list_users ---

type zendeskListUsersTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *zendeskListUsersTool) Name() string        { return "zendesk_list_users" }
func (t *zendeskListUsersTool) Description() string { return "List Zendesk users." }
func (t *zendeskListUsersTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *zendeskListUsersTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "zendesk_list_users",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"limit": {Type: "integer", Description: "Maximum number of results (default 25)"},
				},
			},
		},
	}
}
func (t *zendeskListUsersTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	subdomain, email, token, err := zendeskCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "zendesk_list_users: auth: " + err.Error()}
	}

	limit := int(floatArg(args, "limit"))
	if limit <= 0 {
		limit = 25
	}

	params := url.Values{}
	params.Set("per_page", strconv.Itoa(limit))
	apiURL := fmt.Sprintf("https://%s.zendesk.com/api/v2/users?%s", subdomain, params.Encode())
	out, err := zendeskDo(ctx, "GET", apiURL, email, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- zendesk_create_ticket ---

type zendeskCreateTicketTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *zendeskCreateTicketTool) Name() string        { return "zendesk_create_ticket" }
func (t *zendeskCreateTicketTool) Description() string { return "Create a new Zendesk ticket. Requires user approval." }
func (t *zendeskCreateTicketTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *zendeskCreateTicketTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "zendesk_create_ticket",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"subject", "description"},
				Properties: map[string]backend.ToolProperty{
					"subject":     {Type: "string", Description: "Ticket subject"},
					"description": {Type: "string", Description: "Ticket description / initial comment body"},
					"priority":    {Type: "string", Description: "Ticket priority: low, normal, high, or urgent"},
				},
			},
		},
	}
}
func (t *zendeskCreateTicketTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	subdomain, email, token, err := zendeskCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "zendesk_create_ticket: auth: " + err.Error()}
	}

	subject, _ := args["subject"].(string)
	description, _ := args["description"].(string)
	if subject == "" || description == "" {
		return tools.ToolResult{IsError: true, Error: "zendesk_create_ticket: subject and description are required"}
	}

	ticketData := map[string]any{
		"subject": subject,
		"comment": map[string]any{"body": description},
	}
	if priority, ok := args["priority"].(string); ok && priority != "" {
		ticketData["priority"] = priority
	}

	payload := map[string]any{"ticket": ticketData}
	bodyBytes, _ := json.Marshal(payload)
	apiURL := fmt.Sprintf("https://%s.zendesk.com/api/v2/tickets", subdomain)
	out, err := zendeskDo(ctx, "POST", apiURL, email, token, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- zendesk_update_ticket ---

type zendeskUpdateTicketTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *zendeskUpdateTicketTool) Name() string        { return "zendesk_update_ticket" }
func (t *zendeskUpdateTicketTool) Description() string { return "Update a Zendesk ticket. Requires user approval." }
func (t *zendeskUpdateTicketTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *zendeskUpdateTicketTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "zendesk_update_ticket",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"ticket_id", "fields"},
				Properties: map[string]backend.ToolProperty{
					"ticket_id": {Type: "string", Description: "The Zendesk ticket ID"},
					"fields":    {Type: "object", Description: "Ticket fields to update (e.g. {\"status\": \"solved\", \"priority\": \"high\"})"},
				},
			},
		},
	}
}
func (t *zendeskUpdateTicketTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	subdomain, email, token, err := zendeskCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "zendesk_update_ticket: auth: " + err.Error()}
	}

	ticketID, _ := args["ticket_id"].(string)
	fields := args["fields"]
	if ticketID == "" || fields == nil {
		return tools.ToolResult{IsError: true, Error: "zendesk_update_ticket: ticket_id and fields are required"}
	}

	payload := map[string]any{"ticket": fields}
	bodyBytes, _ := json.Marshal(payload)
	apiURL := fmt.Sprintf("https://%s.zendesk.com/api/v2/tickets/%s", subdomain, ticketID)
	out, err := zendeskDo(ctx, "PUT", apiURL, email, token, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerZendeskTools registers all 5 Zendesk tools.
func registerZendeskTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{
		"zendesk_list_tickets", "zendesk_get_ticket", "zendesk_list_users",
		"zendesk_create_ticket", "zendesk_update_ticket",
	}
	for _, n := range names {
		reg.Unregister(n)
	}
	strictInject(reg, &zendeskListTicketsTool{mgr: mgr, conns: conns})
	strictInject(reg, &zendeskGetTicketTool{mgr: mgr, conns: conns})
	strictInject(reg, &zendeskListUsersTool{mgr: mgr, conns: conns})
	strictInject(reg, &zendeskCreateTicketTool{mgr: mgr, conns: conns})
	strictInject(reg, &zendeskUpdateTicketTool{mgr: mgr, conns: conns})
	return nil
}
