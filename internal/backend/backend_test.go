package backend

import (
	"encoding/json"
	"testing"
)

func TestMessageRoles(t *testing.T) {
	m := Message{Role: "user", Content: "hello"}
	if m.Role != "user" {
		t.Fatalf("expected user, got %s", m.Role)
	}
}

func TestToolPropertyType(t *testing.T) {
	p := ToolProperty{Type: "string", Description: "a path"}
	if p.Type != "string" {
		t.Fatalf("unexpected type: %s", p.Type)
	}
}

// TestMessage_ToolCallFields verifies that ToolCalls, ToolName, and ToolCallID are settable.
func TestMessage_ToolCallFields(t *testing.T) {
	tc := ToolCall{
		ID: "call_123",
		Function: ToolCallFunction{
			Name:      "read_file",
			Arguments: map[string]any{"file_path": "main.go"},
		},
	}
	m := Message{
		Role:       "assistant",
		ToolCalls:  []ToolCall{tc},
		ToolCallID: "call_123",
		ToolName:   "read_file",
	}
	if len(m.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(m.ToolCalls))
	}
	if m.ToolCalls[0].ID != "call_123" {
		t.Errorf("expected call_123, got %q", m.ToolCalls[0].ID)
	}
}

// TestTool_JSONSerialization verifies that Tool serializes to JSON correctly.
func TestTool_JSONSerialization(t *testing.T) {
	tool := Tool{
		Type: "function",
		Function: ToolFunction{
			Name:        "grep",
			Description: "Search files",
			Parameters: ToolParameters{
				Type:     "object",
				Required: []string{"pattern"},
				Properties: map[string]ToolProperty{
					"pattern": {Type: "string", Description: "Regex"},
				},
			},
		},
	}
	data, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if parsed["type"] != "function" {
		t.Errorf("expected type=function, got %v", parsed["type"])
	}
	fn, ok := parsed["function"].(map[string]any)
	if !ok {
		t.Fatal("expected function object in JSON")
	}
	if fn["name"] != "grep" {
		t.Errorf("expected name=grep, got %v", fn["name"])
	}
}

// TestChatRequest_NilOnToken verifies ChatRequest with nil OnToken is a valid state.
func TestChatRequest_NilOnToken(t *testing.T) {
	req := ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
		OnToken:  nil,
	}
	if req.OnToken != nil {
		t.Error("expected nil OnToken")
	}
}

// TestChatResponse_ZeroValue verifies zero-value ChatResponse is safe to use.
func TestChatResponse_ZeroValue(t *testing.T) {
	var resp ChatResponse
	if resp.Content != "" {
		t.Error("expected empty Content")
	}
	if len(resp.ToolCalls) != 0 {
		t.Error("expected empty ToolCalls")
	}
	if resp.DoneReason != "" {
		t.Error("expected empty DoneReason")
	}
}

// TestToolParameters_EmptyRequired verifies that omitempty works for Required.
func TestToolParameters_EmptyRequired(t *testing.T) {
	params := ToolParameters{Type: "object"}
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	// "required" should be omitted when nil.
	if string(data) != `{"type":"object"}` && !jsonFieldAbsent(data, "required") {
		t.Errorf("expected 'required' to be omitted, got: %s", data)
	}
}

// jsonFieldAbsent returns true if the field name is not present in the JSON.
func jsonFieldAbsent(data []byte, field string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return false
	}
	_, exists := m[field]
	return !exists
}

// TestContentPart_TextType verifies ContentPart struct with type=text.
func TestContentPart_TextType(t *testing.T) {
	p := ContentPart{Type: "text", Text: "hello"}
	if p.Type != "text" {
		t.Errorf("type: %q", p.Type)
	}
	if p.Text != "hello" {
		t.Errorf("text: %q", p.Text)
	}
}

// TestMessage_PartsField verifies that Message can hold multiple ContentParts.
func TestMessage_PartsField(t *testing.T) {
	m := Message{
		Role: "user",
		Parts: []ContentPart{
			{Type: "text", Text: "hello"},
			{Type: "image_url", ImageURL: "data:image/png;base64,abc"},
		},
	}
	if len(m.Parts) != 2 {
		t.Errorf("expected 2 parts, got %d", len(m.Parts))
	}
	if m.Parts[0].Type != "text" {
		t.Errorf("first part type: %q", m.Parts[0].Type)
	}
	if m.Parts[1].Type != "image_url" {
		t.Errorf("second part type: %q", m.Parts[1].Type)
	}
}

// TestMessage_BackwardCompat_NilParts verifies that Parts is nil by default.
func TestMessage_BackwardCompat_NilParts(t *testing.T) {
	m := Message{Role: "user", Content: "hello"}
	if m.Parts != nil {
		t.Error("Parts should be nil by default")
	}
}
