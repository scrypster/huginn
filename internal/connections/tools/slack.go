package conntools

import (
	"context"
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

// slackProvider is a minimal stub to supply OAuthConfig to Manager.GetHTTPClient.
var slackProvider = connproviders.NewSlack("", "")

// --- slack_list_channels ---

type slackListChannelsTool struct {
	mgr          *connections.Manager
	conns        []connections.Connection
	accountsDesc string
}

func (t *slackListChannelsTool) Name() string { return "slack_list_channels" }
func (t *slackListChannelsTool) Description() string {
	return fmt.Sprintf("List Slack channels. Accounts: %s.", t.accountsDesc)
}
func (t *slackListChannelsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *slackListChannelsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "slack_list_channels",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"cursor":  {Type: "string", Description: "Pagination cursor from previous response"},
					"limit":   {Type: "integer", Description: "Max channels to return (default 100)"},
					"account": {Type: "string", Description: "Slack account label (optional)"},
				},
			},
		},
	}
}
func (t *slackListChannelsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	account, _ := args["account"].(string)
	conn := resolveConnection(t.conns, account)
	client, err := t.mgr.GetHTTPClient(ctx, conn.ID, slackProvider)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("slack_list_channels: auth: %v", err)}
	}
	params := url.Values{}
	if cursor, ok := args["cursor"].(string); ok && cursor != "" {
		params.Set("cursor", cursor)
	}
	if limit := int(floatArg(args, "limit")); limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	apiURL := "https://slack.com/api/conversations.list"
	if len(params) > 0 {
		apiURL = apiURL + "?" + params.Encode()
	}
	out, err := slackGET(ctx, client, apiURL)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- slack_read_channel ---

type slackReadChannelTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *slackReadChannelTool) Name() string        { return "slack_read_channel" }
func (t *slackReadChannelTool) Description() string { return "Read messages from a Slack channel." }
func (t *slackReadChannelTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *slackReadChannelTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "slack_read_channel",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"channel"},
				Properties: map[string]backend.ToolProperty{
					"channel": {Type: "string", Description: "Channel ID or name"},
					"limit":   {Type: "integer", Description: "Max messages to return (default 20)"},
					"account": {Type: "string", Description: "Slack account label (optional)"},
				},
			},
		},
	}
}
func (t *slackReadChannelTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	account, _ := args["account"].(string)
	conn := resolveConnection(t.conns, account)
	client, err := t.mgr.GetHTTPClient(ctx, conn.ID, slackProvider)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("slack_read_channel: auth: %v", err)}
	}
	channel, _ := args["channel"].(string)
	if channel == "" {
		return tools.ToolResult{IsError: true, Error: "slack_read_channel: channel required"}
	}
	limit := 20
	if l := int(floatArg(args, "limit")); l > 0 {
		limit = l
	}
	apiURL := fmt.Sprintf("https://slack.com/api/conversations.history?channel=%s&limit=%d",
		url.QueryEscape(channel), limit)
	out, err := slackGET(ctx, client, apiURL)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- slack_post_message ---

type slackPostMessageTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *slackPostMessageTool) Name() string        { return "slack_post_message" }
func (t *slackPostMessageTool) Description() string { return "Post a message to a Slack channel. Requires user approval." }
func (t *slackPostMessageTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *slackPostMessageTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "slack_post_message",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"channel", "text"},
				Properties: map[string]backend.ToolProperty{
					"channel":   {Type: "string", Description: "Channel ID or name"},
					"text":      {Type: "string", Description: "Message text"},
					"thread_ts": {Type: "string", Description: "Thread timestamp to reply in (optional)"},
					"account":   {Type: "string", Description: "Slack account label (optional)"},
				},
			},
		},
	}
}
func (t *slackPostMessageTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	account, _ := args["account"].(string)
	conn := resolveConnection(t.conns, account)
	client, err := t.mgr.GetHTTPClient(ctx, conn.ID, slackProvider)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("slack_post_message: auth: %v", err)}
	}
	channel, _ := args["channel"].(string)
	text, _ := args["text"].(string)
	if channel == "" || text == "" {
		return tools.ToolResult{IsError: true, Error: "slack_post_message: channel and text are required"}
	}
	payload := map[string]any{"channel": channel, "text": text}
	if ts, ok := args["thread_ts"].(string); ok && ts != "" {
		payload["thread_ts"] = ts
	}
	out, err := slackPOST(ctx, client, "https://slack.com/api/chat.postMessage", payload)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerSlackTools registers slack_list_channels, slack_read_channel, slack_post_message.
func registerSlackTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{"slack_list_channels", "slack_read_channel", "slack_post_message"}
	for _, n := range names {
		reg.Unregister(n)
	}

	var labels []string
	for _, c := range conns {
		labels = append(labels, c.AccountLabel)
	}
	accountsDesc := strings.Join(labels, ", ")

	strictInject(reg, &slackListChannelsTool{mgr: mgr, conns: conns, accountsDesc: accountsDesc})
	strictInject(reg, &slackReadChannelTool{mgr: mgr, conns: conns})
	strictInject(reg, &slackPostMessageTool{mgr: mgr, conns: conns})
	return nil
}

// --- Slack API helpers ---

func slackGET(ctx context.Context, client *http.Client, apiURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("slack: build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("slack: %w", err)
	}
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("slack: decode: %w", err)
	}
	if ok, _ := result["ok"].(bool); !ok {
		errMsg, _ := result["error"].(string)
		return "", fmt.Errorf("slack: API error: %s", errMsg)
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

func slackPOST(ctx context.Context, client *http.Client, apiURL string, payload any) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("slack: marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(string(data)))
	if err != nil {
		return "", fmt.Errorf("slack: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("slack: %w", err)
	}
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("slack: decode: %w", err)
	}
	if ok, _ := result["ok"].(bool); !ok {
		errMsg, _ := result["error"].(string)
		return "", fmt.Errorf("slack: API error: %s", errMsg)
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}
