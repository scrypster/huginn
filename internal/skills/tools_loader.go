package skills

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type toolFrontmatter struct {
	Tool        string         `yaml:"tool"`
	Description string         `yaml:"description"`
	Schema      map[string]any `yaml:"schema"`

	// Phase 2 mode fields
	Mode        string   `yaml:"mode"`         // "template" | "shell" | "agent"
	Shell       string   `yaml:"shell"`         // binary path/name for shell mode
	Args        []string `yaml:"args"`          // static args passed to binary
	Timeout     int      `yaml:"timeout"`       // shell timeout in seconds (0 = no per-tool limit)
	MaxOutputKB int      `yaml:"max_output_kb"` // cap shell output (0 = default 64 KB)

	// Agent mode fields
	AgentModel   string `yaml:"agent_model"`
	BudgetTokens int    `yaml:"budget_tokens"`
}

// LoadToolsFromDir reads skill_dir/tools/*.md and returns a PromptTool for each
// valid file. Files without valid frontmatter or without a `tool:` key are skipped.
// If the tools/ subdirectory does not exist, an empty slice is returned (not an error).
func LoadToolsFromDir(skillDir string) ([]*PromptTool, error) {
	toolsDir := filepath.Join(skillDir, "tools")
	entries, err := os.ReadDir(toolsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var result []*PromptTool
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		info, statErr := entry.Info()
		if statErr != nil {
			continue
		}
		if info.Size() > maxSkillFileSizeBytes {
			continue // skip oversized tool files silently (they'll fail parse anyway)
		}
		data, err := os.ReadFile(filepath.Join(toolsDir, entry.Name()))
		if err != nil {
			continue
		}
		pt, err := parseToolMD(data)
		if err != nil {
			continue
		}
		result = append(result, pt)
	}
	return result, nil
}

func parseToolMD(data []byte) (*PromptTool, error) {
	const sep = "---"
	if !bytes.HasPrefix(data, []byte(sep+"\n")) {
		return nil, errors.New("no frontmatter")
	}
	rest := data[len(sep)+1:]
	idx := bytes.Index(rest, []byte("\n"+sep))
	if idx < 0 {
		return nil, errors.New("unclosed frontmatter")
	}
	fmBytes := rest[:idx]
	body := strings.TrimSpace(string(rest[idx+len("\n"+sep):]))
	if strings.HasPrefix(body, "\n") {
		body = strings.TrimPrefix(body, "\n")
	}

	var fm toolFrontmatter
	if err := yaml.Unmarshal(fmBytes, &fm); err != nil {
		return nil, err
	}
	if fm.Tool == "" {
		return nil, errors.New("missing 'tool' key")
	}

	schemaJSON := "{}"
	if fm.Schema != nil {
		b, err := json.Marshal(fm.Schema)
		if err == nil {
			schemaJSON = string(b)
		}
	}

	pt := NewPromptTool(fm.Tool, fm.Description, schemaJSON, body)

	// Apply mode overrides.
	if fm.Mode != "" {
		pt.mode = fm.Mode
	}

	// Shell mode fields.
	if fm.Shell != "" {
		pt.shellBin = fm.Shell
	}
	if len(fm.Args) > 0 {
		pt.shellArgs = fm.Args
	}
	pt.shellTimeoutSecs = fm.Timeout
	if fm.MaxOutputKB > 0 {
		pt.maxOutputBytes = fm.MaxOutputKB * 1024
	}

	// Agent mode fields.
	pt.agentModel = fm.AgentModel
	pt.budgetTokens = fm.BudgetTokens

	return pt, nil
}
