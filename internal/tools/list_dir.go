package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/scrypster/huginn/internal/backend"
)

// ListDirTool lists directory contents.
type ListDirTool struct {
	SandboxRoot string
}

func (t *ListDirTool) Name() string                { return "list_dir" }
func (t *ListDirTool) Description() string         { return "List the contents of a directory." }
func (t *ListDirTool) Permission() PermissionLevel { return PermRead }

func (t *ListDirTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "list_dir",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"path"},
				Properties: map[string]backend.ToolProperty{
					"path":      {Type: "string", Description: "Directory path to list (relative to project root, use '.' for root)"},
					"recursive": {Type: "boolean", Description: "List recursively up to 3 levels deep (default false)"},
				},
			},
		},
	}
}

func (t *ListDirTool) Execute(_ context.Context, args map[string]any) ToolResult {
	dirPath, ok := args["path"].(string)
	if !ok || dirPath == "" {
		dirPath = "."
	}
	recursive := false
	if v, ok := args["recursive"].(bool); ok {
		recursive = v
	}

	resolved, err := ResolveSandboxed(t.SandboxRoot, dirPath)
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}

	var sb strings.Builder
	const maxEntries = 500

	if recursive {
		count := 0
		err = filepath.WalkDir(resolved, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			// Skip hidden dirs and common noise
			if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor") {
				return filepath.SkipDir
			}
			rel, _ := filepath.Rel(resolved, path)
			if rel == "." {
				return nil
			}
			// Limit depth to 3
			depth := strings.Count(rel, string(filepath.Separator))
			if d.IsDir() && depth >= 3 {
				return filepath.SkipDir
			}
			prefix := "f"
			if d.IsDir() {
				prefix = "d"
			} else if d.Type()&os.ModeSymlink != 0 {
				prefix = "l"
			}
			fmt.Fprintf(&sb, "%s %s\n", prefix, rel)
			count++
			if count >= maxEntries {
				return fmt.Errorf("max entries reached")
			}
			return nil
		})
	} else {
		entries, err2 := os.ReadDir(resolved)
		if err2 != nil {
			return ToolResult{IsError: true, Error: fmt.Sprintf("list_dir: %v", err2)}
		}
		for _, entry := range entries {
			prefix := "f"
			if entry.IsDir() {
				prefix = "d"
			} else if entry.Type()&os.ModeSymlink != 0 {
				prefix = "l"
			}
			fmt.Fprintf(&sb, "%s %s\n", prefix, entry.Name())
		}
	}
	if err != nil && err.Error() != "max entries reached" {
		// Non-fatal: partial results with a note
		sb.WriteString(fmt.Sprintf("\n[warning: walk stopped: %v]\n", err))
	}

	return ToolResult{Output: sb.String()}
}
