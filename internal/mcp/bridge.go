package mcp

import (
	"context"
	"errors"
	"strings"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// gatedCaller is an interface over the circuit-breaker call path so that
// MCPToolAdapter can be tested independently of ServerManager.
type gatedCaller interface {
	CallToolGated(ctx context.Context, ms *managedServer, name string, args map[string]any) (*MCPToolCallResult, error)
}

type MCPToolAdapter struct {
	client *MCPClient
	tool   MCPTool
	// gate and ms are optional; when non-nil, Execute uses the circuit-breaker
	// path instead of calling client.CallTool directly.
	gate gatedCaller
	ms   *managedServer
}

// NewMCPToolAdapter creates an adapter that calls the MCP server directly,
// without a circuit breaker.  Kept for use in tests and non-managed paths.
func NewMCPToolAdapter(client *MCPClient, tool MCPTool) *MCPToolAdapter {
	return &MCPToolAdapter{client: client, tool: tool}
}

// NewMCPToolAdapterGated creates an adapter that routes calls through the
// ServerManager circuit breaker.  Use this for all tools registered via
// StartAll / watchServer reconnect.
func NewMCPToolAdapterGated(client *MCPClient, tool MCPTool, gate gatedCaller, ms *managedServer) *MCPToolAdapter {
	return &MCPToolAdapter{client: client, tool: tool, gate: gate, ms: ms}
}

func (a *MCPToolAdapter) Name() string {
	return a.tool.Name
}

func (a *MCPToolAdapter) Description() string {
	return a.tool.Description
}

func (a *MCPToolAdapter) Permission() tools.PermissionLevel {
	return tools.PermWrite
}

func (a *MCPToolAdapter) Schema() backend.Tool {
	props := make(map[string]backend.ToolProperty)
	for k, v := range a.tool.InputSchema.Properties {
		props[k] = backend.ToolProperty{
			Type:        v.Type,
			Description: v.Description,
		}
	}
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        a.tool.Name,
			Description: a.tool.Description,
			Parameters: backend.ToolParameters{
				Type:       a.tool.InputSchema.Type,
				Properties: props,
				Required:   a.tool.InputSchema.Required,
			},
		},
	}
}

func (a *MCPToolAdapter) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	var (
		result *MCPToolCallResult
		err    error
	)
	if a.gate != nil && a.ms != nil {
		result, err = a.gate.CallToolGated(ctx, a.ms, a.tool.Name, args)
	} else {
		result, err = a.client.CallTool(ctx, a.tool.Name, args)
	}
	if err != nil {
		if errors.Is(err, ErrCircuitOpen) {
			return tools.ToolResult{IsError: true, Error: "error: mcp server temporarily unavailable (circuit open)"}
		}
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	var parts []string
	for _, c := range result.Content {
		if c.Type == "text" && c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	combined := strings.Join(parts, "\n")
	if result.IsError {
		return tools.ToolResult{IsError: true, Error: combined, Output: combined}
	}
	return tools.ToolResult{Output: combined}
}

var _ tools.Tool = (*MCPToolAdapter)(nil)
