package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/mcp"
)

type MockTransport struct {
	mu       sync.Mutex
	toSend   [][]byte
	received [][]byte
	err      error
}

func (m *MockTransport) Send(_ context.Context, msg []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.received = append(m.received, msg)
	return m.err
}

func (m *MockTransport) Receive(_ context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	if len(m.toSend) == 0 {
		return nil, fmt.Errorf("no more responses")
	}
	msg := m.toSend[0]
	m.toSend = m.toSend[1:]
	return msg, nil
}

func (m *MockTransport) Close() error {
	return nil
}

func buildInitResponse(id int) []byte {
	resp, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  map[string]any{"protocolVersion": "2024-11-05"},
	})
	return resp
}

func buildToolsListResponse(id int, tools []map[string]any) []byte {
	resp, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  map[string]any{"tools": tools},
	})
	return resp
}

func TestMCPClient_Initialize(t *testing.T) {
	tr := &MockTransport{toSend: [][]byte{buildInitResponse(1)}}
	c := mcp.NewMCPClient(tr)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if len(tr.received) != 2 {
		t.Fatalf("expected 2 messages (init + notification), got %d", len(tr.received))
	}
	var req map[string]any
	json.Unmarshal(tr.received[0], &req)
	if req["method"] != "initialize" {
		t.Errorf("expected initialize, got %v", req["method"])
	}
}

func TestMCPClient_ListTools(t *testing.T) {
	toolDef := []map[string]any{
		{
			"name":        "test_tool",
			"description": "A test",
			"inputSchema": map[string]any{"type": "object"},
		},
	}
	tr := &MockTransport{toSend: [][]byte{buildInitResponse(1), buildToolsListResponse(2, toolDef)}}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "test_tool" {
		t.Errorf("unexpected tools: %v", tools)
	}
}

func TestMCPClient_IDIncrement(t *testing.T) {
	tr := &MockTransport{
		toSend: [][]byte{
			buildInitResponse(1),
			buildToolsListResponse(2, []map[string]any{}),
		},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())
	c.ListTools(context.Background())

	tr.mu.Lock()
	defer tr.mu.Unlock()
	if len(tr.received) < 2 {
		t.Fatalf("expected at least 2 sends")
	}

	var req1 map[string]any
	json.Unmarshal(tr.received[0], &req1)
	id1 := req1["id"]

	var req2 map[string]any
	json.Unmarshal(tr.received[2], &req2)
	id2 := req2["id"]

	if id1 == id2 {
		t.Error("expected different IDs for sequential requests")
	}
}

func TestMCPClient_Initialize_SendError(t *testing.T) {
	tr := &MockTransport{err: fmt.Errorf("send failed")}
	c := mcp.NewMCPClient(tr)
	err := c.Initialize(context.Background())
	if err == nil {
		t.Error("expected send error")
	}
	if !stringContains(err.Error(), "send failed") {
		t.Errorf("expected send failed in error, got: %v", err)
	}
}

func TestMCPClient_Initialize_ReceiveError(t *testing.T) {
	tr := &MockTransport{
		toSend: [][]byte{},
		err:    fmt.Errorf("receive failed"),
	}
	c := mcp.NewMCPClient(tr)
	err := c.Initialize(context.Background())
	if err == nil {
		t.Error("expected receive error")
	}
	if !stringContains(err.Error(), "receive failed") {
		t.Errorf("expected receive failed in error, got: %v", err)
	}
}

func TestMCPClient_Initialize_UnmarshalError(t *testing.T) {
	tr := &MockTransport{toSend: [][]byte{[]byte("not valid json")}}
	c := mcp.NewMCPClient(tr)
	err := c.Initialize(context.Background())
	if err == nil {
		t.Error("expected unmarshal error")
	}
}

func TestMCPClient_Initialize_ResponseError(t *testing.T) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"error":   map[string]any{"code": -32000, "message": "server error"},
	}
	respData, _ := json.Marshal(resp)
	tr := &MockTransport{toSend: [][]byte{respData}}
	c := mcp.NewMCPClient(tr)
	err := c.Initialize(context.Background())
	if err == nil {
		t.Error("expected RPC error response")
	}
	if !stringContains(err.Error(), "server error") {
		t.Errorf("expected server error in response, got: %v", err)
	}
}

func TestMCPClient_ListTools_SendError(t *testing.T) {
	tr := &MockTransport{
		toSend: [][]byte{buildInitResponse(1)},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	tr.err = fmt.Errorf("send failed on list")
	tools, err := c.ListTools(context.Background())
	if err == nil {
		t.Error("expected send error")
	}
	if tools != nil {
		t.Error("expected nil tools on error")
	}
}

func TestMCPClient_ListTools_ReceiveError(t *testing.T) {
	tr := &MockTransport{
		toSend: [][]byte{buildInitResponse(1)},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	tr.err = fmt.Errorf("receive failed")
	tools, err := c.ListTools(context.Background())
	if err == nil {
		t.Error("expected receive error")
	}
	if tools != nil {
		t.Error("expected nil tools on error")
	}
}

func TestMCPClient_ListTools_UnmarshalResponseError(t *testing.T) {
	tr := &MockTransport{
		toSend: [][]byte{buildInitResponse(1), []byte("not valid json")},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	tools, err := c.ListTools(context.Background())
	if err == nil {
		t.Error("expected unmarshal error")
	}
	if tools != nil {
		t.Error("expected nil tools on error")
	}
}

func TestMCPClient_ListTools_ResponseError(t *testing.T) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"error":   map[string]any{"code": -32000, "message": "list tools failed"},
	}
	respData, _ := json.Marshal(resp)
	tr := &MockTransport{
		toSend: [][]byte{buildInitResponse(1), respData},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	tools, err := c.ListTools(context.Background())
	if err == nil {
		t.Error("expected RPC error")
	}
	if tools != nil {
		t.Error("expected nil tools on error")
	}
}

func TestMCPClient_ListTools_UnmarshalResultError(t *testing.T) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"result":  "invalid result format",
	}
	respData, _ := json.Marshal(resp)
	tr := &MockTransport{
		toSend: [][]byte{buildInitResponse(1), respData},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	tools, err := c.ListTools(context.Background())
	if err == nil {
		t.Error("expected unmarshal result error")
	}
	if tools != nil {
		t.Error("expected nil tools on error")
	}
}

func TestMCPClient_CallTool_Send_Error(t *testing.T) {
	tr := &MockTransport{
		toSend: [][]byte{buildInitResponse(1)},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	tr.err = fmt.Errorf("send failed")
	result, err := c.CallTool(context.Background(), "test", map[string]any{})
	if err == nil {
		t.Error("expected send error")
	}
	if result != nil {
		t.Error("expected nil result on error")
	}
}

func TestMCPClient_CallTool_ReceiveError(t *testing.T) {
	tr := &MockTransport{
		toSend: [][]byte{buildInitResponse(1)},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	tr.err = fmt.Errorf("receive failed")
	result, err := c.CallTool(context.Background(), "test", map[string]any{})
	if err == nil {
		t.Error("expected receive error")
	}
	if result != nil {
		t.Error("expected nil result on error")
	}
}

func TestMCPClient_CallTool_UnmarshalResponseError(t *testing.T) {
	tr := &MockTransport{
		toSend: [][]byte{buildInitResponse(1), []byte("not valid json")},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	result, err := c.CallTool(context.Background(), "test", map[string]any{})
	if err == nil {
		t.Error("expected unmarshal error")
	}
	if result != nil {
		t.Error("expected nil result on error")
	}
}

func TestMCPClient_CallTool_ResponseError(t *testing.T) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"error":   map[string]any{"code": -32000, "message": "call failed"},
	}
	respData, _ := json.Marshal(resp)
	tr := &MockTransport{
		toSend: [][]byte{buildInitResponse(1), respData},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	result, err := c.CallTool(context.Background(), "test", map[string]any{})
	if err == nil {
		t.Error("expected RPC error")
	}
	if result != nil {
		t.Error("expected nil result on error")
	}
}

func TestMCPClient_CallTool_UnmarshalResultError(t *testing.T) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"result":  "invalid result format",
	}
	respData, _ := json.Marshal(resp)
	tr := &MockTransport{
		toSend: [][]byte{buildInitResponse(1), respData},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	result, err := c.CallTool(context.Background(), "test", map[string]any{})
	if err == nil {
		t.Error("expected unmarshal result error")
	}
	if result != nil {
		t.Error("expected nil result on error")
	}
}

func TestMCPClient_CallTool_Success(t *testing.T) {
	toolResult := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"result": map[string]any{
			"content": []map[string]any{
				{
					"type": "text",
					"text": "success",
				},
			},
			"isError": false,
		},
	}
	toolResultData, _ := json.Marshal(toolResult)
	tr := &MockTransport{
		toSend: [][]byte{buildInitResponse(1), toolResultData},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	result, err := c.CallTool(context.Background(), "test", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
	if len(result.Content) != 1 || result.Content[0].Text != "success" {
		t.Errorf("unexpected result content: %v", result)
	}
}

// Tests for EOF error wrapping via ListTools and CallTool (wrapReceiveErr is tested indirectly)
func TestMCPClient_ListTools_EOFError(t *testing.T) {
	tr := &MockTransport{
		toSend: [][]byte{buildInitResponse(1)},
		err:    io.EOF,
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	tools, err := c.ListTools(context.Background())
	if err == nil {
		t.Error("expected error for EOF")
	}
	errStr := err.Error()
	if !stringContains(errStr, "server disconnected") && !stringContains(errStr, "EOF") {
		t.Errorf("expected server disconnected or EOF message, got: %v", err)
	}
	if tools != nil {
		t.Error("expected nil tools on error")
	}
}

func TestMCPClient_CallTool_UnexpectedEOFError(t *testing.T) {
	tr := &MockTransport{
		toSend: [][]byte{buildInitResponse(1)},
		err:    io.ErrUnexpectedEOF,
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	result, err := c.CallTool(context.Background(), "test", map[string]any{})
	if err == nil {
		t.Error("expected error for ErrUnexpectedEOF")
	}
	errStr := err.Error()
	if !stringContains(errStr, "server disconnected") && !stringContains(errStr, "unexpected EOF") {
		t.Errorf("expected server disconnected or unexpected EOF message, got: %v", err)
	}
	if result != nil {
		t.Error("expected nil result on error")
	}
}

func TestMCPClient_Close(t *testing.T) {
	tr := &MockTransport{}
	c := mcp.NewMCPClient(tr)
	err := c.Close()
	if err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestMCPClient_RPCErrorType verifies RPCError implementation
func TestMCPClient_RPCErrorType(t *testing.T) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"error":   map[string]any{"code": -32000, "message": "test error"},
	}
	respData, _ := json.Marshal(resp)
	tr := &MockTransport{toSend: [][]byte{respData}}
	c := mcp.NewMCPClient(tr)
	err := c.Initialize(context.Background())

	if err == nil {
		t.Error("expected error")
	}

	errStr := err.Error()
	if !stringContains(errStr, "JSON-RPC error") && !stringContains(errStr, "test error") {
		t.Errorf("expected JSON-RPC error or test error in message, got: %v", errStr)
	}
}

func stringContains(s, substr string) bool {
	if len(s) == 0 || len(substr) == 0 {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
