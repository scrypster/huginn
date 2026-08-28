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

func (t *GrepTool) Name() string                { return "grep" }
func (t *GrepTool) Description() string         { return "Search file contents for a regex pattern." }
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
	const maxLines = 500      // cap on total detail lines rendered (including context lines)
	const maxDetailLines = 50 // cap on detail lines shown after the summary — a verbose
	// dump of every match measured worse than no tool at all;
	// the file:count summary carries the "how much" signal,
	// the capped detail carries "what it looks like".
	var detail strings.Builder
	var fileOrder []string
	matchCount := map[string]int{} // per-file count of MATCHING lines (excludes context)
	fileCount := 0
	lineCount := 0    // total detail lines rendered (matches + context)
	totalMatches := 0 // total matching lines found, across all files (uncapped)

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
		if fileCount >= maxFiles {
			break
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil || isBinaryBytes(data) {
			continue
		}
		lines := strings.Split(string(data), "\n")
		rel, _ := filepath.Rel(t.SandboxRoot, path)

		fileMatched := false
		for i, line := range lines {
			if !re.MatchString(line) {
				continue
			}
			fileMatched = true
			matchCount[rel]++
			totalMatches++

			if lineCount >= maxDetailLines {
				continue // keep counting for the summary, stop rendering detail
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
			for j := start; j < end && lineCount < maxLines; j++ {
				sep := ":"
				if j != i {
					sep = "-"
				}
				fmt.Fprintf(&detail, "%s:%d%s%s\n", rel, j+1, sep, lines[j])
				lineCount++
			}
			if contextLines > 0 {
				detail.WriteString("--\n")
			}
		}
		if fileMatched {
			fileOrder = append(fileOrder, rel)
			fileCount++
		}
	}

	if fileCount == 0 {
		return ToolResult{Output: fmt.Sprintf("no matches for %q", pattern)}
	}

	// Summary first: file:count so the model knows the shape of the result
	// before reading any lines, then the (possibly truncated) matching lines.
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d files matched, %d total matching lines\n", fileCount, totalMatches)
	for _, f := range fileOrder {
		fmt.Fprintf(&sb, "%s: %d\n", f, matchCount[f])
	}
	sb.WriteString("\n")
	sb.WriteString(detail.String())
	if totalMatches > lineCount {
		fmt.Fprintf(&sb, "... [%d more matching lines not shown]\n", totalMatches-lineCount)
	}

	return ToolResult{
		Output:   sb.String(),
		Metadata: map[string]any{"files_matched": fileCount, "lines": totalMatches},
	}
}
