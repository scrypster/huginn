package notepad

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Notepad represents a persistent note with optional YAML frontmatter.
type Notepad struct {
	Name     string
	Priority int
	Tags     []string
	Scope    string
	Content  string
	Path     string
}

type frontmatter struct {
	Priority string   `yaml:"priority"`
	Tags     []string `yaml:"tags"`
	Scope    string   `yaml:"scope"`
}

// ParseNotepad parses a notepad from raw data, extracting optional YAML frontmatter.
func ParseNotepad(name, defaultScope, path string, data []byte) (*Notepad, error) {
	content := string(data)
	fm := frontmatter{}
	body := content

	// Check for YAML frontmatter
	if strings.HasPrefix(content, "---\n") {
		rest := content[4:]
		end := strings.Index(rest, "\n---")
		if end != -1 {
			yamlPart := rest[:end]
			if err := yaml.Unmarshal([]byte(yamlPart), &fm); err != nil {
				return nil, fmt.Errorf("notepad %q: frontmatter parse: %w", name, err)
			}
			body = strings.TrimSpace(rest[end+4:])
			if strings.HasPrefix(body, "\n") {
				body = body[1:]
			}
		}
	}

	// Determine priority level (0 = normal, 1 = high)
	priority := 0
	if strings.ToLower(fm.Priority) == "high" {
		priority = 1
	}

	// Determine scope (defaults to provided scope, or validate if specified)
	scope := defaultScope
	if fm.Scope == "global" || fm.Scope == "project" {
		scope = fm.Scope
	}

	return &Notepad{
		Name:     name,
		Priority: priority,
		Tags:     fm.Tags,
		Scope:    scope,
		Content:  strings.TrimSpace(body),
		Path:     path,
	}, nil
}
