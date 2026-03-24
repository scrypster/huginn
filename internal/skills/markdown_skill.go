package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/scrypster/huginn/internal/tools"
	"gopkg.in/yaml.v3"
)

// MarkdownFrontmatter represents the YAML frontmatter of a SKILL.md file.
type MarkdownFrontmatter struct {
	Name        string `yaml:"name"`
	Author      string `yaml:"author,omitempty"`
	Source      string `yaml:"source,omitempty"`
	Description string `yaml:"description,omitempty"`
	ToolsDir    string `yaml:"tools_dir,omitempty"`
	Huginn      struct {
		Priority int `yaml:"priority"`
	} `yaml:"huginn,omitempty"`
}

// MarkdownSkill implements the Skill interface for single-file SKILL.md format.
type MarkdownSkill struct {
	fm     MarkdownFrontmatter
	prompt string
	rules  string
	dir    string // directory containing the SKILL.md file (for tools_dir resolution)
}

// LoadMarkdownSkill reads a SKILL.md file from disk and parses it.
func LoadMarkdownSkill(path string) (*MarkdownSkill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("skills: LoadMarkdownSkill %q: %w", path, err)
	}

	s, err := ParseMarkdownSkillBytes(data)
	if err != nil {
		return nil, err
	}
	s.dir = filepath.Dir(path)
	return s, nil
}

// ParseMarkdownSkillBytes parses SKILL.md content from bytes.
// Returns an error if the content is not valid UTF-8.
func ParseMarkdownSkillBytes(data []byte) (*MarkdownSkill, error) {
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("skills: SKILL.md contains invalid UTF-8 encoding")
	}
	return parseMarkdownSkill(string(data))
}

// parseMarkdownSkill parses raw SKILL.md content and returns a MarkdownSkill.
// Unexported function that does the actual parsing.
func parseMarkdownSkill(content string) (*MarkdownSkill, error) {
	// Content MUST start with ---\n
	if !strings.HasPrefix(content, "---\n") {
		return nil, fmt.Errorf("skills: SKILL.md must start with YAML frontmatter (---\\n)")
	}

	// Find closing \n---\n
	closingDelim := "\n---\n"
	closeIdx := strings.Index(content[4:], closingDelim) // Start search after opening ---\n
	if closeIdx == -1 {
		return nil, fmt.Errorf("skills: SKILL.md frontmatter not closed (missing closing ---)")
	}
	closeIdx += 4 // Adjust for the 4 chars of opening "---\n"

	// Extract YAML block (excluding delimiters)
	yamlBlock := content[4:closeIdx]

	// Parse YAML
	var fm MarkdownFrontmatter
	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return nil, fmt.Errorf("skills: SKILL.md YAML parse error: %w", err)
	}

	// Validate required fields
	if fm.Name == "" {
		return nil, fmt.Errorf("skills: SKILL.md missing required field 'name'")
	}

	// Extract body (everything after closing ---)
	body := content[closeIdx+4:] // Skip the closing \n---\n

	// Split body at ## Rules heading
	prompt, rules := splitAtRules(body)

	return &MarkdownSkill{
		fm:     fm,
		prompt: strings.TrimSpace(prompt),
		rules:  strings.TrimSpace(rules),
	}, nil
}

// splitAtRules splits content at the first line that is exactly "## Rules".
// Returns (prompt, rules) where prompt is everything before ## Rules and rules is everything after.
func splitAtRules(body string) (string, string) {
	lines := strings.Split(body, "\n")

	for i, line := range lines {
		if line == "## Rules" {
			// Split at this line
			prompt := strings.Join(lines[:i], "\n")
			rules := strings.Join(lines[i+1:], "\n")
			return prompt, rules
		}
	}

	// No ## Rules found, return all as prompt
	return body, ""
}

// Name returns the skill name from frontmatter.
func (s *MarkdownSkill) Name() string {
	return s.fm.Name
}

// Author returns the author from frontmatter.
func (s *MarkdownSkill) Author() string {
	return s.fm.Author
}

// Source returns the source from frontmatter.
func (s *MarkdownSkill) Source() string {
	return s.fm.Source
}

// Description returns the short one-line description shown in the / picker.
func (s *MarkdownSkill) Description() string {
	return s.fm.Description
}

// Priority returns the huginn.priority value from frontmatter.
func (s *MarkdownSkill) Priority() int {
	return s.fm.Huginn.Priority
}

// SystemPromptFragment returns the prompt content (body before ## Rules).
func (s *MarkdownSkill) SystemPromptFragment() string {
	return s.prompt
}

// RuleContent returns the rules content (body after ## Rules).
func (s *MarkdownSkill) RuleContent() string {
	return s.rules
}

// Tools returns tools declared via the tools_dir frontmatter field.
// If tools_dir is empty the skill is knowledge-only and nil is returned.
func (s *MarkdownSkill) Tools() []tools.Tool {
	if s.fm.ToolsDir == "" {
		return nil
	}
	toolsDir := s.fm.ToolsDir
	if !filepath.IsAbs(toolsDir) && s.dir != "" {
		toolsDir = filepath.Join(s.dir, toolsDir)
	}
	promptTools, err := LoadToolsFromDir(toolsDir)
	if err != nil || len(promptTools) == 0 {
		return nil
	}
	result := make([]tools.Tool, len(promptTools))
	for i, pt := range promptTools {
		result[i] = pt
	}
	return result
}
