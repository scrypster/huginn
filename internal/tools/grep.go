package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/scrypster/huginn/internal/backend"
)

// GrepTool searches file contents for a regex pattern.
type GrepTool struct {
	SandboxRoot string
}

func (t *GrepTool) Name() string        { return "grep" }
func (t *GrepTool) Description() string { return "Search file contents for a regex pattern." }
func (t *GrepTool) Permission() PermissionLevel { return PermRead }

func (t *GrepTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "grep",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"pattern"},
				Properties: map[string]backend.ToolProperty{
					"pattern":       {Type: "string", Description: "Regular expression to search for"},
					"path":          {Type: "string", Description: "Directory or file to search (default: project root)"},
					"include":       {Type: "string", Description: "Glob pattern to filter files (e.g., '*.go')"},
					"context_lines": {Type: "integer", Description: "Lines of context before/after each match (default 0)"},
				},
			},
		},
	}
}

func (t *GrepTool) Execute(_ context.Context, args map[string]any) ToolResult {
	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return ToolResult{IsError: true, Error: "grep: 'pattern' argument required"}
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("grep: invalid pattern %q: %v", pattern, err)}
	}

	basePath := "."
	if v, ok := args["path"].(string); ok && v != "" {
		basePath = v
	}
	include := ""
	if v, ok := args["include"].(string); ok {
		include = v
	}
	contextLines := 0
	if v, ok := args["context_lines"]; ok {
		switch n := v.(type) {
		case float64:
			contextLines = int(n)
		case int:
			contextLines = n
		}
	}

	resolved, err2 := ResolveSandboxed(t.SandboxRoot, basePath)
	if err2 != nil {
		return ToolResult{IsError: true, Error: err2.Error()}
	}

	const maxFiles = 100
	const maxLines = 500
	var sb strings.Builder
	fileCount := 0
	lineCount := 0

	info, statErr := os.Stat(resolved)
	var paths []string
	if statErr == nil && !info.IsDir() {
		paths = []string{resolved}
	} else {
		filepath.WalkDir(resolved, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				if d != nil && d.IsDir() {
					n := d.Name()
					if n == ".git" || n == "node_modules" || n == "vendor" {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if include != "" {
				if matched, _ := filepath.Match(include, d.Name()); !matched {
					return nil
				}
			}
			paths = append(paths, path)
			return nil
		})
	}

	for _, path := range paths {
		if fileCount >= maxFiles || lineCount >= maxLines {
			break
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil || isBinaryBytes(data) {
			continue
		}
		lines := strings.Split(string(data), "\n")
		rel, _ := filepath.Rel(t.SandboxRoot, path)

		prevLen := sb.Len()
		for i, line := range lines {
			if !re.MatchString(line) {
				continue
			}
			// Context before
			start := i - contextLines
			if start < 0 {
				start = 0
			}
			end := i + contextLines + 1
			if end > len(lines) {
				end = len(lines)
			}
			for j := start; j < end; j++ {
				sep := ":"
				if j != i {
					sep = "-"
				}
				fmt.Fprintf(&sb, "%s:%d%s%s\n", rel, j+1, sep, lines[j])
				lineCount++
				if lineCount >= maxLines {
					break
				}
			}
			if contextLines > 0 {
				sb.WriteString("--\n")
			}
		}
		if sb.Len() > prevLen {
			fileCount++
		}
	}

	if sb.Len() == 0 {
		return ToolResult{Output: fmt.Sprintf("no matches for %q", pattern)}
	}
	return ToolResult{
		Output: sb.String(),
		Metadata: map[string]any{"files_matched": fileCount, "lines": lineCount},
	}
}
