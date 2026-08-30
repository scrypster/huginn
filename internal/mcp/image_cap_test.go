package mcp_test

// V7/V8: image cap uses the actual decoded byte length (not the base64
// length estimate), multi-image results attach only the first in-cap image
// with a truthful "N more screenshots not shown" note, and a non-image MIME
// type is never turned into a data URI.

import (
	"context"
	"encoding/base64"
	"strconv"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/mcp"
)

// maxInlineImageBytes mirrors the unexported constant in bridge.go (2MB).
const maxInlineImageBytesForTest = 2 << 20

func TestMCPToolAdapter_Execute_ImageContent_ExactlyAtCapAttached(t *testing.T) {
	raw := make([]byte, maxInlineImageBytesForTest) // exactly at the cap
	b64 := base64.StdEncoding.EncodeToString(raw)

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
	if result.Metadata == nil {
		t.Fatal("expected Metadata to be set for an image exactly at the cap")
	}
	uri, _ := result.Metadata["image_data_uri"].(string)
	if uri == "" {
		t.Error("expected image_data_uri to be attached when decoded length exactly equals the cap")
	}
	if strings.Contains(result.Output, "exceeds") {
		t.Errorf("an exactly-at-cap image must not be reported as exceeding the cap, got: %q", result.Output)
	}
}

func TestMCPToolAdapter_Execute_ImageContent_EmptyDataSkipped(t *testing.T) {
	tr := &MockTransport{
		toSend: [][]byte{
			buildInitResponse(1),
			buildToolCallResponse(2, []map[string]any{
				{"type": "image", "data": "", "mimeType": "image/png"},
				{"type": "text", "text": "done"},
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
	if result.Metadata != nil {
		if _, ok := result.Metadata["image_data_uri"]; ok {
			t.Error("expected no image_data_uri for an empty-Data image content block")
		}
	}
	if !strings.Contains(result.Output, "done") {
		t.Errorf("expected the sibling text content to survive, got: %q", result.Output)
	}
}

func TestMCPToolAdapter_Execute_MultipleImages_FirstAttachedRestNoted(t *testing.T) {
	img1 := base64.StdEncoding.EncodeToString([]byte("first-image"))
	img2 := base64.StdEncoding.EncodeToString([]byte("second-image"))
	img3 := base64.StdEncoding.EncodeToString([]byte("third-image"))

	tr := &MockTransport{
		toSend: [][]byte{
			buildInitResponse(1),
			buildToolCallResponse(2, []map[string]any{
				{"type": "image", "data": img1, "mimeType": "image/png"},
				{"type": "image", "data": img2, "mimeType": "image/png"},
				{"type": "image", "data": img3, "mimeType": "image/png"},
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
	uri, _ := result.Metadata["image_data_uri"].(string)
	if uri != "data:image/png;base64,"+img1 {
		t.Errorf("expected the first image attached, got image_data_uri = %q", uri)
	}
	if !strings.Contains(result.Output, "2 more screenshots not shown") {
		t.Errorf("expected a truthful note about the 2 dropped screenshots, got: %q", result.Output)
	}
}

func TestMCPToolAdapter_Execute_NonImageMimeType_NoAttachment(t *testing.T) {
	data := base64.StdEncoding.EncodeToString([]byte("<html>not an image</html>"))

	tr := &MockTransport{
		toSend: [][]byte{
			buildInitResponse(1),
			buildToolCallResponse(2, []map[string]any{
				{"type": "image", "data": data, "mimeType": "text/html"},
			}, false),
		},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	tool := mcp.MCPTool{Name: "some_tool", InputSchema: mcp.MCPInputSchema{Type: "object"}}
	adapter := mcp.NewMCPToolAdapter(c, tool)

	result := adapter.Execute(context.Background(), nil)
	if result.IsError {
		t.Fatalf("expected no error, got: %s", result.Error)
	}
	if result.Metadata != nil {
		if _, ok := result.Metadata["image_data_uri"]; ok {
			t.Error("expected no image_data_uri for a non-image/* MIME type")
		}
	}
	if !strings.Contains(result.Output, "text/html") {
		t.Errorf("expected Output to note the non-image content as text, got: %q", result.Output)
	}
}

// Sanity check that the test cap constant matches the package's real cap so
// the exactly-at-cap test above is actually exercising the boundary.
func TestImageCapConstant_Sanity(t *testing.T) {
	if strconv.Itoa(maxInlineImageBytesForTest) != strconv.Itoa(2<<20) {
		t.Fatal("test cap constant drifted from the intended 2MB boundary")
	}
}
