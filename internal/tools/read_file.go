package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/scrypster/huginn/internal/backend"
)

const maxReadBytes = 200 * 1024 // 200KB read cap

// ReadFileTool reads a file and returns its content with line numbers.
type ReadFileTool struct {
	SandboxRoot string
}

func (t *ReadFileTool) Name() string { return "read_file" }
func (t *ReadFileTool) Description() string {
	return "Read the contents of a file."
}
func (t *ReadFileTool) Permission() PermissionLevel { return PermRead }

func (t *ReadFileTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "read_file",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"file_path"},
				Properties: map[string]backend.ToolProperty{
					"file_path": {Type: "string", Description: "Path to the file to read (relative to project root)"},
					"offset":    {Type: "integer", Description: "Line number to start reading from (1-indexed, optional)"},
					"limit":     {Type: "integer", Description: "Number of lines to read (optional)"},
				},
			},
		},
	}
}

func (t *ReadFileTool) Execute(_ context.Context, args map[string]any) ToolResult {
	filePath, ok := args["file_path"].(string)
	if !ok || filePath == "" {
		return ToolResult{IsError: true, Error: "read_file: 'file_path' argument required"}
	}

	resolved, err := ResolveSandboxed(t.SandboxRoot, filePath)
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("read_file: %v", err)}
	}

	// Binary detection (sniff before splitting into lines)
	if isBinaryBytes(data) {
		return ToolResult{IsError: true, Error: fmt.Sprintf("read_file: %q is a binary file", filePath)}
	}

	lines := strings.Split(string(data), "\n")

	// Apply offset/limit FIRST so deep reads in large files work correctly.
	// The byte cap is applied afterwards to the already-sliced result.
	offset := 0
	if v, ok := args["offset"]; ok {
		switch n := v.(type) {
		case float64:
			offset = int(n) - 1 // convert to 0-indexed
		case int:
			offset = n - 1
		}
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(lines) {
		return ToolResult{Output: ""}
	}
	lines = lines[offset:]

	if v, ok := args["limit"]; ok {
		var limit int
		switch n := v.(type) {
		case float64:
			limit = int(n)
		case int:
			limit = n
		}
		if limit > 0 && limit < len(lines) {
			lines = lines[:limit]
		}
	}

	// Format with line numbers (cat -n style), then cap output at maxReadBytes.
	var sb strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&sb, "%6d\t%s\n", offset+i+1, line)
		if sb.Len() > maxReadBytes {
			sb.WriteString("... (output truncated at 200KB)\n")
			break
		}
	}

	return ToolResult{Output: sb.String()}
}

// isBinaryBytes detects binary content by looking for null bytes in the first 512 bytes.
func isBinaryBytes(data []byte) bool {
	sniff := data
	if len(sniff) > 512 {
		sniff = sniff[:512]
	}
	for _, b := range sniff {
		if b == 0 {
			return true
		}
	}
	return false
}
