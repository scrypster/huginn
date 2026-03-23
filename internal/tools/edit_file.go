package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/scrypster/huginn/internal/backend"
)

// EditFileTool performs string-replacement edits on a file.
type EditFileTool struct {
	SandboxRoot string
	FileLock    *FileLockManager
}

func (t *EditFileTool) Name() string { return "edit_file" }
func (t *EditFileTool) Description() string {
	return "Edit a file by replacing an exact string. old_string must match exactly (including whitespace and indentation). If old_string appears more than once and replace_all is false, the operation fails."
}
func (t *EditFileTool) Permission() PermissionLevel { return PermWrite }

func (t *EditFileTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "edit_file",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"file_path", "old_string", "new_string"},
				Properties: map[string]backend.ToolProperty{
					"file_path":   {Type: "string", Description: "Path to the file to edit (relative to project root)"},
					"old_string":  {Type: "string", Description: "Exact string to find and replace (must match exactly)"},
					"new_string":  {Type: "string", Description: "String to replace it with"},
					"replace_all": {Type: "boolean", Description: "Replace all occurrences (default false)"},
				},
			},
		},
	}
}

func (t *EditFileTool) Execute(_ context.Context, args map[string]any) ToolResult {
	filePath, ok := args["file_path"].(string)
	if !ok || filePath == "" {
		return ToolResult{IsError: true, Error: "edit_file: 'file_path' argument required"}
	}
	oldStr, ok := args["old_string"].(string)
	if !ok || oldStr == "" {
		return ToolResult{IsError: true, Error: "edit_file: 'old_string' argument required and must not be empty"}
	}
	newStr, ok := args["new_string"].(string)
	if !ok {
		return ToolResult{IsError: true, Error: "edit_file: 'new_string' argument required"}
	}
	replaceAll := false
	if v, ok := args["replace_all"].(bool); ok {
		replaceAll = v
	}

	resolved, err := ResolveSandboxed(t.SandboxRoot, filePath)
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}

	// Acquire per-path lock to prevent concurrent read-modify-write races (e.g. parallel swarm agents).
	if t.FileLock != nil {
		t.FileLock.Lock(resolved)
		defer t.FileLock.Unlock(resolved)
	}

	info, err := os.Stat(resolved)
	existingMode := os.FileMode(0644)
	if err == nil {
		existingMode = info.Mode()
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("edit_file: read %s: %v", filePath, err)}
	}

	content := string(data)
	count := strings.Count(content, oldStr)
	if count == 0 {
		return ToolResult{IsError: true, Error: fmt.Sprintf("edit_file: old_string not found in %s", filePath)}
	}
	if count > 1 && !replaceAll {
		return ToolResult{IsError: true, Error: fmt.Sprintf("edit_file: old_string appears %d times in %s — use replace_all=true or provide more unique context", count, filePath)}
	}

	var result string
	if replaceAll {
		result = strings.ReplaceAll(content, oldStr, newStr)
	} else {
		result = strings.Replace(content, oldStr, newStr, 1)
	}

	if err := os.WriteFile(resolved, []byte(result), existingMode); err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("edit_file: write %s: %v", filePath, err)}
	}

	replacements := 1
	if replaceAll {
		replacements = count
	}
	return ToolResult{
		Output:   fmt.Sprintf("edited %s: replaced %d occurrence(s)", filePath, replacements),
		Metadata: map[string]any{"replacements": replacements},
	}
}
