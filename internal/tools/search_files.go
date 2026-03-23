package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/scrypster/huginn/internal/backend"
)

// SearchFilesTool finds files matching a glob pattern.
type SearchFilesTool struct {
	SandboxRoot string
}

func (t *SearchFilesTool) Name() string        { return "search_files" }
func (t *SearchFilesTool) Description() string { return "Search for files matching a glob pattern." }
func (t *SearchFilesTool) Permission() PermissionLevel { return PermRead }

func (t *SearchFilesTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "search_files",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"pattern"},
				Properties: map[string]backend.ToolProperty{
					"pattern": {Type: "string", Description: "Glob pattern (e.g., '**/*.go', 'src/*.ts')"},
					"path":    {Type: "string", Description: "Directory to search within (default: project root)"},
				},
			},
		},
	}
}

func (t *SearchFilesTool) Execute(_ context.Context, args map[string]any) ToolResult {
	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return ToolResult{IsError: true, Error: "search_files: 'pattern' argument required"}
	}
	basePath := "."
	if v, ok := args["path"].(string); ok && v != "" {
		basePath = v
	}

	resolved, err := ResolveSandboxed(t.SandboxRoot, basePath)
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}

	const maxResults = 200
	var matches []string

	hasDoubleGlob := strings.Contains(pattern, "**")

	err = filepath.WalkDir(resolved, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(t.SandboxRoot, path)
		name := d.Name()

		var matched bool
		if hasDoubleGlob {
			// Try matching the full relative path
			subPattern := strings.ReplaceAll(pattern, "**/", "")
			matchedName, _ := filepath.Match(subPattern, name)
			matchedRel, _ := filepath.Match(subPattern, rel)
			matched = matchedName || matchedRel

			// Also try matching each path segment
			if !matched {
				for _, seg := range strings.Split(rel, string(filepath.Separator)) {
					if m, _ := filepath.Match(subPattern, seg); m {
						matched = true
						break
					}
				}
			}
		} else {
			// Try matching against just the filename
			matched, _ = filepath.Match(pattern, name)
			if !matched {
				// Also try matching against the relative path
				matched, _ = filepath.Match(pattern, rel)
			}
		}

		if matched {
			matches = append(matches, rel)
			if len(matches) >= maxResults {
				return fmt.Errorf("max results reached")
			}
		}
		return nil
	})

	if len(matches) == 0 {
		return ToolResult{Output: fmt.Sprintf("no files matching %q found", pattern)}
	}

	return ToolResult{
		Output: strings.Join(matches, "\n"),
		Metadata: map[string]any{"count": len(matches)},
	}
}
