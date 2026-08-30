package mcp_test

// Tests that MCPToolAdapter.Execute surfaces MCP image content blocks
// (e.g. browser_take_screenshot) instead of silently dropping them — the
// previous behavior, since MCPContent had no Data/MimeType fields and the
// adapter only ever collected c.Type == "text" parts.

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/mcp"
)

func TestMCPToolAdapter_Execute_ImageContent_AttachedWithinCap(t *testing.T) {
	imgBytes := []byte("fake-png-bytes")
	b64 := base64.StdEncoding.EncodeToString(imgBytes)

	tr := &MockTransport{
		toSend: [][]byte{
			buildInitResponse(1),
			buildToolCallResponse(2, []map[string]any{
				{"type": "image", "data": b64, "mimeType": "image/png"},
			}, false),
		},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	tool := mcp.MCPTool{Name: "browser_take_screenshot", InputSchema: mcp.MCPInputSchema{Type: "object"}}
	adapter := mcp.NewMCPToolAdapter(c, tool)

	result := adapter.Execute(context.Background(), nil)
	if result.IsError {
		t.Fatalf("expected no error, got: %s", result.Error)
	}
	if !strings.Contains(result.Output, "image captured") {
		t.Errorf("expected Output to note the captured image, got: %q", result.Output)
	}
	if strings.Contains(result.Output, b64) {
		t.Error("raw base64 image data must not appear in Output (that's model-facing prompt content)")
	}
	if result.Metadata == nil {
		t.Fatal("expected Metadata to be set")
	}
	uri, _ := result.Metadata["image_data_uri"].(string)
	want := "data:image/png;base64," + b64
	if uri != want {
		t.Errorf("image_data_uri = %q, want %q", uri, want)
	}
}

func TestMCPToolAdapter_Execute_ImageContent_OverCapNotAttached(t *testing.T) {
	// Build a base64 payload whose decoded length exceeds the 2MB cap.
	big := make([]byte, 3<<20) // 3MB raw
	b64 := base64.StdEncoding.EncodeToString(big)

	tr := &MockTransport{
		toSend: [][]byte{
			buildInitResponse(1),
			buildToolCallResponse(2, []map[string]any{
				{"type": "image", "data": b64, "mimeType": "image/png"},
			}, false),
		},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	tool := mcp.MCPTool{Name: "browser_take_screenshot", InputSchema: mcp.MCPInputSchema{Type: "object"}}
	adapter := mcp.NewMCPToolAdapter(c, tool)

	result := adapter.Execute(context.Background(), nil)
	if result.IsError {
		t.Fatalf("expected no error, got: %s", result.Error)
	}
	if !strings.Contains(result.Output, "exceeds") {
		t.Errorf("expected Output to explain why the image was dropped, got: %q", result.Output)
	}
	if result.Metadata != nil {
		if _, ok := result.Metadata["image_data_uri"]; ok {
			t.Error("expected no image_data_uri in Metadata when the image exceeds the size cap")
		}
	}
}

func TestMCPToolAdapter_Execute_MixedTextAndImageContent(t *testing.T) {
	imgBytes := []byte("small-image")
	b64 := base64.StdEncoding.EncodeToString(imgBytes)

	tr := &MockTransport{
		toSend: [][]byte{
			buildInitResponse(1),
			buildToolCallResponse(2, []map[string]any{
				{"type": "text", "text": "Navigated to https://example.com"},
				{"type": "image", "data": b64, "mimeType": "image/jpeg"},
			}, false),
		},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	tool := mcp.MCPTool{Name: "browser_navigate", InputSchema: mcp.MCPInputSchema{Type: "object"}}
	adapter := mcp.NewMCPToolAdapter(c, tool)

	result := adapter.Execute(context.Background(), nil)
	if !strings.Contains(result.Output, "Navigated to https://example.com") {
		t.Errorf("expected text content preserved, got: %q", result.Output)
	}
	if !strings.Contains(result.Output, "image captured") {
		t.Errorf("expected image note in Output, got: %q", result.Output)
	}
	uri, _ := result.Metadata["image_data_uri"].(string)
	if uri != "data:image/jpeg;base64,"+b64 {
		t.Errorf("image_data_uri = %q", uri)
	}
}
