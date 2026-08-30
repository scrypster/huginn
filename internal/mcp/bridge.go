package mcp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// maxInlineImageBytes bounds how large a decoded MCP image content block
// (e.g. browser_take_screenshot) can be before it's surfaced inline as a
// data URI. Above this the image is never attached to the tool result —
// only noted in the output text — so a large screenshot can't blow up
// message payload size or browser memory. 2MB keeps a typical viewport PNG
// comfortably inline while still bounding worst case.
const maxInlineImageBytes = 2 << 20 // 2MB

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

// pluralS returns "s" for any n != 1, "" for n == 1.
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
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
	var imageDataURI string // first image within the size cap, if any
	imagesDropped := 0 // additional images beyond the first attached one (not size-related)
	for _, c := range result.Content {
		switch c.Type {
		case "text":
			if c.Text != "" {
				parts = append(parts, c.Text)
			}
		case "image":
			// MCP image content blocks (e.g. browser_take_screenshot) carry
			// base64-encoded bytes with no "data:" prefix — see MCPContent.
			// Never silently drop these: always note the image in the
			// text output (what the model sees), and additionally attach
			// it as a data URI in Metadata (what the UI renders) when it's
			// within the size cap. Metadata never reaches the model, so
			// attaching it here doesn't cost prompt tokens.
			if c.Data == "" {
				continue
			}
			mimeType := c.MimeType
			if mimeType == "" {
				mimeType = "image/png"
			}
			// V8: only build/attach a data URI for actual image content —
			// a non-image MIME type (or one MCP servers mislabel) is noted
			// as text, never rendered as an <img>.
			if !strings.HasPrefix(mimeType, "image/") {
				parts = append(parts, fmt.Sprintf("[non-image content: %s, not attached]", mimeType))
				continue
			}
			// V7: cap on ACTUAL decoded bytes, not the base64 length
			// estimate — DecodedLen() rounds up to the padded block size
			// and can overestimate, wrongly rejecting an image that's
			// actually within the cap.
			decoded, decodeErr := base64.StdEncoding.DecodeString(c.Data)
			if decodeErr != nil {
				parts = append(parts, fmt.Sprintf("[image captured: %s — could not be decoded, not attached]", mimeType))
				continue
			}
			decodedLen := len(decoded)
			if decodedLen > maxInlineImageBytes {
				parts = append(parts, fmt.Sprintf("[image captured (%s, %d bytes) — exceeds %d byte inline display cap, not attached]", mimeType, decodedLen, maxInlineImageBytes))
				continue
			}
			parts = append(parts, fmt.Sprintf("[image captured: %s, %d bytes]", mimeType, decodedLen))
			if imageDataURI == "" {
				imageDataURI = "data:" + mimeType + ";base64," + c.Data
			} else {
				// Only the first in-cap image is attached (Image is a
				// single field, not a list, across PersistedToolCall /
				// SpaceMessageToolCall / the UI) — say so honestly instead
				// of silently dropping the rest.
				imagesDropped++
			}
		}
	}
	if imagesDropped > 0 {
		parts = append(parts, fmt.Sprintf("%d more screenshot%s not shown", imagesDropped, pluralS(imagesDropped)))
	}
	combined := strings.Join(parts, "\n")
	if result.IsError {
		return tools.ToolResult{IsError: true, Error: combined, Output: combined}
	}
	toolResult := tools.ToolResult{Output: combined}
	if imageDataURI != "" {
		toolResult.Metadata = map[string]any{"image_data_uri": imageDataURI}
	}
	return toolResult
}

var _ tools.Tool = (*MCPToolAdapter)(nil)
