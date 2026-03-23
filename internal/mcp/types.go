package mcp

import (
	"encoding/json"
	"fmt"
)

const ProtocolVersion = "2024-11-05"

var ClientInfo = map[string]string{"name": "huginn", "version": "0.3.0"}

type Request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

type MCPToolProperty struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type MCPInputSchema struct {
	Type       string                     `json:"type"`
	Properties map[string]MCPToolProperty `json:"properties,omitempty"`
	Required   []string                   `json:"required,omitempty"`
}

type MCPTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema MCPInputSchema `json:"inputSchema"`
}

type MCPToolsListResult struct {
	Tools []MCPTool `json:"tools"`
}

type MCPToolCallResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

type MCPContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type initializeParams struct {
	ProtocolVersion string            `json:"protocolVersion"`
	Capabilities    map[string]any    `json:"capabilities"`
	ClientInfo      map[string]string `json:"clientInfo"`
}

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}
