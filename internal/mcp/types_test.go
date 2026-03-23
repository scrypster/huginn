package mcp_test

import (
	"encoding/json"
	"testing"

	"github.com/scrypster/huginn/internal/mcp"
)

func TestRequest_Serialization(t *testing.T) {
	req := mcp.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  map[string]any{"protocolVersion": "2024-11-05"},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	json.Unmarshal(data, &got)
	if got["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc: got %v", got["jsonrpc"])
	}
	if got["method"] != "initialize" {
		t.Errorf("method: got %v", got["method"])
	}
}

func TestRequest_NoParamsOmitted(t *testing.T) {
	req := mcp.Request{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "notifications/initialized",
	}
	data, _ := json.Marshal(req)
	var got map[string]any
	json.Unmarshal(data, &got)
	if _, ok := got["params"]; ok {
		t.Error("params should be omitted when nil")
	}
}

func TestResponse_SuccessDeserialization(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}`
	var resp mcp.Response
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ID != 1 {
		t.Errorf("id: %d", resp.ID)
	}
	if resp.Error != nil {
		t.Error("expected no error")
	}
}

func TestResponse_ErrorDeserialization(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"server error"}}`
	var resp mcp.Response
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error == nil {
		t.Error("expected error")
	}
	if resp.Error.Code != -32000 {
		t.Errorf("error code: expected -32000, got %d", resp.Error.Code)
	}
	if resp.Error.Message != "server error" {
		t.Errorf("error message: expected 'server error', got %q", resp.Error.Message)
	}
}

func TestRPCError_String(t *testing.T) {
	err := &mcp.RPCError{
		Code:    -32000,
		Message: "custom error",
	}
	errStr := err.Error()
	if errStr != "JSON-RPC error -32000: custom error" {
		t.Errorf("unexpected error string: %s", errStr)
	}
}

func TestMCPTool_Serialization(t *testing.T) {
	tool := mcp.MCPTool{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: mcp.MCPInputSchema{
			Type: "object",
			Properties: map[string]mcp.MCPToolProperty{
				"param1": {
					Type:        "string",
					Description: "A parameter",
				},
			},
			Required: []string{"param1"},
		},
	}

	data, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var recovered mcp.MCPTool
	if err := json.Unmarshal(data, &recovered); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if recovered.Name != tool.Name {
		t.Errorf("name: expected %q, got %q", tool.Name, recovered.Name)
	}
	if recovered.Description != tool.Description {
		t.Errorf("description: expected %q, got %q", tool.Description, recovered.Description)
	}
	if len(recovered.InputSchema.Properties) != 1 {
		t.Errorf("expected 1 property, got %d", len(recovered.InputSchema.Properties))
	}
}

func TestMCPContent_Serialization(t *testing.T) {
	content := mcp.MCPContent{
		Type: "text",
		Text: "Hello, World!",
	}

	data, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var recovered mcp.MCPContent
	if err := json.Unmarshal(data, &recovered); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if recovered.Type != content.Type {
		t.Errorf("type: expected %q, got %q", content.Type, recovered.Type)
	}
	if recovered.Text != content.Text {
		t.Errorf("text: expected %q, got %q", content.Text, recovered.Text)
	}
}
