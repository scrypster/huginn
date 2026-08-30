package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/scrypster/huginn/internal/backend"
)

// WriteFileTool writes content to a file, creating it if needed.
type WriteFileTool struct {
	SandboxRoot string
	FileLock    *FileLockManager
}

func (t *WriteFileTool) Name() string { return "write_file" }
func (t *WriteFileTool) Description() string {
	return "Write content to a file, creating it and any parent directories if needed."
}
func (t *WriteFileTool) Permission() PermissionLevel { return PermWrite }

func (t *WriteFileTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "write_file",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"file_path", "content"},
				Properties: map[string]backend.ToolProperty{
					"file_path": {Type: "string", Description: "Path to write (relative to project root)"},
					"content":   {Type: "string", Description: "Content to write to the file"},
				},
			},
		},
	}
}

func (t *WriteFileTool) Execute(_ context.Context, args map[string]any) ToolResult {
	filePath, ok := args["file_path"].(string)
	if !ok || filePath == "" {
		return ToolResult{IsError: true, Error: "write_file: 'file_path' argument required"}
	}
	content, ok := args["content"].(string)
	if !ok {
		return ToolResult{IsError: true, Error: "write_file: 'content' argument required"}
	}

	resolved, err := ResolveSandboxed(t.SandboxRoot, filePath)
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}

	// Acquire per-path lock to prevent concurrent writes to the same file (e.g. parallel swarm agents).
	if t.FileLock != nil {
		t.FileLock.Lock(resolved)
		defer t.FileLock.Unlock(resolved)
	}

	if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("write_file: mkdir: %v", err)}
	}

	existingMode := os.FileMode(0644)
	if info, err := os.Stat(resolved); err == nil {
		existingMode = info.Mode()
	}

	// Capture the pre-write content before overwriting so a before/after diff
	// can be attached to the result (a missing/unreadable file just means the
	// diff is presented as a pure addition — not fatal to the write itself).
	var before string
	if data, err := os.ReadFile(resolved); err == nil {
		before = string(data)
	}

	if err := os.WriteFile(resolved, []byte(content), existingMode); err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("write_file: %v", err)}
	}

	metadata := map[string]any{"bytes_written": len(content)}
	attachDiffMetadata(metadata, filePath, before, content)

	return ToolResult{
		Output:   fmt.Sprintf("wrote %d bytes to %s", len(content), filePath),
		Metadata: metadata,
	}
}
