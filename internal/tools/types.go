package tools

import (
	"context"

	"github.com/scrypster/huginn/internal/backend"
)

// PermissionLevel classifies tool safety.
type PermissionLevel int

const (
	PermRead  PermissionLevel = iota // Safe: always allowed (read_file, list_dir, grep, search_files)
	PermWrite                         // Needs approval: write_file, edit_file
	PermExec                          // Always needs approval: bash
)

// ToolResult is returned by every tool execution.
type ToolResult struct {
	Output   string         // stdout or result text
	Error    string         // stderr or error message
	IsError  bool           // true if the tool errored
	Metadata map[string]any // optional (e.g., bytes_written, exit_code)
}

// Tool is the interface every built-in tool must implement.
type Tool interface {
	// Name returns the tool name as the model will call it (e.g., "bash", "read_file").
	Name() string

	// Description returns a one-line description for the model's tool schema.
	Description() string

	// Permission returns the safety classification.
	Permission() PermissionLevel

	// Schema returns the tool definition sent to the model.
	Schema() backend.Tool

	// Execute runs the tool with the given arguments.
	// ctx is cancellable. args come from ToolCallFunctionArguments.ToMap().
	Execute(ctx context.Context, args map[string]any) ToolResult
}
