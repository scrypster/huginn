package conntools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/connections"
	connproviders "github.com/scrypster/huginn/internal/connections/providers"
	"github.com/scrypster/huginn/internal/tools"
)

// gmailProvider is a minimal IntegrationProvider stub used only to supply OAuthConfig
// to Manager.GetHTTPClient. Credentials are empty — only scopes/endpoint matter for
// token refresh; actual credentials are held in the SecretStore.
var gmailProvider = connproviders.NewGoogle("", "", []string{"gmail", "calendar", "drive", "docs", "sheets", "contacts"})

// --- Tool structs ---

type gmailSearchTool struct {
	mgr         *connections.Manager
	conns       []connections.Connection
	accountsDesc string
}

func (t *gmailSearchTool) Name() string { return "gmail_search" }
func (t *gmailSearchTool) Description() string {
	return fmt.Sprintf("Search Gmail messages. Accounts: %s. Use 'account' param to select.", t.accountsDesc)
}
func (t *gmailSearchTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *gmailSearchTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "gmail_search",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"query"},
				Properties: map[string]backend.ToolProperty{
					"query":       {Type: "string", Description: "Gmail search query (e.g. 'from:boss@company.com subject:urgent')"},
					"max_results": {Type: "integer", Description: "Max emails to return (default 10)"},
					"account":     {Type: "string", Description: "Account email (optional, defaults to first)"},
				},
			},
		},
	}
}
func (t *gmailSearchTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	account, _ := args["account"].(string)
	conn := resolveConnection(t.conns, account)
	client, err := t.mgr.GetHTTPClient(ctx, conn.ID, gmailProvider)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("gmail_search: auth: %v", err)}
	}
	query, _ := args["query"].(string)
	maxResults := 10
	if v, ok := args["max_results"].(float64); ok {
		maxResults = int(v)
	}
	out, err := searchGmail(ctx, client, query, maxResults)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

type gmailReadTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *gmailReadTool) Name() string        { return "gmail_read" }
func (t *gmailReadTool) Description() string { return "Read a Gmail message by ID. Returns headers and body text." }
func (t *gmailReadTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *gmailReadTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "gmail_read",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"message_id"},
				Properties: map[string]backend.ToolProperty{
					"message_id": {Type: "string", Description: "Gmail message ID"},
					"account":    {Type: "string", Description: "Account email (optional, defaults to first)"},
				},
			},
		},
	}
}
func (t *gmailReadTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	account, _ := args["account"].(string)
	conn := resolveConnection(t.conns, account)
	client, err := t.mgr.GetHTTPClient(ctx, conn.ID, gmailProvider)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("gmail_read: auth: %v", err)}
	}
	msgID, _ := args["message_id"].(string)
	if msgID == "" {
		return tools.ToolResult{IsError: true, Error: "gmail_read: message_id required"}
	}
	out, err := readGmail(ctx, client, msgID)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

type gmailSendTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *gmailSendTool) Name() string        { return "gmail_send" }
func (t *gmailSendTool) Description() string { return "Send an email via Gmail. Requires user approval." }
func (t *gmailSendTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *gmailSendTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "gmail_send",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"to", "subject", "body"},
				Properties: map[string]backend.ToolProperty{
					"to":          {Type: "string", Description: "Recipient email address"},
					"subject":     {Type: "string", Description: "Email subject"},
					"body":        {Type: "string", Description: "Email body (plain text)"},
					"reply_to_id": {Type: "string", Description: "Message ID to reply to (optional)"},
					"account":     {Type: "string", Description: "Account email (optional, defaults to first)"},
				},
			},
		},
	}
}
func (t *gmailSendTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	account, _ := args["account"].(string)
	conn := resolveConnection(t.conns, account)
	client, err := t.mgr.GetHTTPClient(ctx, conn.ID, gmailProvider)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("gmail_send: auth: %v", err)}
	}
	to, _ := args["to"].(string)
	subject, _ := args["subject"].(string)
	body, _ := args["body"].(string)
	if to == "" || subject == "" {
		return tools.ToolResult{IsError: true, Error: "gmail_send: to and subject are required"}
	}
	out, err := sendGmail(ctx, client, to, subject, body)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerGmailTools registers gmail_search, gmail_read, gmail_send tools.
func registerGmailTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	// Unregister first (idempotent)
	reg.Unregister("gmail_search")
	reg.Unregister("gmail_read")
	reg.Unregister("gmail_send")

	var labels []string
	for _, c := range conns {
		labels = append(labels, c.AccountLabel)
	}
	accountsDesc := strings.Join(labels, ", ")

	strictInject(reg, &gmailSearchTool{mgr: mgr, conns: conns, accountsDesc: accountsDesc})
	strictInject(reg, &gmailReadTool{mgr: mgr, conns: conns})
	strictInject(reg, &gmailSendTool{mgr: mgr, conns: conns})
	return nil
}

// --- API helpers ---

func searchGmail(ctx context.Context, client *http.Client, query string, maxResults int) (string, error) {
	reqURL := fmt.Sprintf("https://gmail.googleapis.com/gmail/v1/users/me/messages?q=%s&maxResults=%d",
		url.QueryEscape(query), maxResults)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("gmail search: build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gmail search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gmail search: HTTP %d", resp.StatusCode)
	}
	var result struct {
		Messages []struct {
			ID       string `json:"id"`
			ThreadID string `json:"threadId"`
		} `json:"messages"`
		ResultSizeEstimate int `json:"resultSizeEstimate"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("gmail search: decode: %w", err)
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

func readGmail(ctx context.Context, client *http.Client, msgID string) (string, error) {
	reqURL := fmt.Sprintf("https://gmail.googleapis.com/gmail/v1/users/me/messages/%s?format=full", msgID)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("gmail read: build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gmail read: %w", err)
	}
	defer resp.Body.Close()
	var msg map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		return "", fmt.Errorf("gmail read: decode: %w", err)
	}
	out, _ := json.MarshalIndent(msg, "", "  ")
	return string(out), nil
}

func sendGmail(ctx context.Context, client *http.Client, to, subject, body string) (string, error) {
	// Build RFC 2822 message
	raw := fmt.Sprintf("To: %s\r\nSubject: %s\r\n\r\n%s", to, subject, body)
	encoded := base64.URLEncoding.EncodeToString([]byte(raw))
	payload := map[string]string{"raw": encoded}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("gmail send: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://gmail.googleapis.com/gmail/v1/users/me/messages/send",
		bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("gmail send: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gmail send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gmail send: HTTP %d", resp.StatusCode)
	}
	return `{"sent": true}`, nil
}
