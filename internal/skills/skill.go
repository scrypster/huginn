package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/scrypster/huginn/internal/tools"
)

// Skill is a packaged bundle of prompt fragments, rule content, and (Phase 2) tools.
// Phase 1: Tools() always returns nil. SystemPromptFragment and RuleContent carry value.
type Skill interface {
	Name() string
	// Description returns the short one-line description shown in the / picker.
	Description() string
	SystemPromptFragment() string
	RuleContent() string
	Tools() []tools.Tool // nil for Phase 1, ready for Phase 2
}

// VersionedSkill is an optional extension of Skill for skills that carry a
// version identifier. The SkillRegistry uses this to detect when two skills
// with the same name but different versions are registered — typically
// indicating an installation conflict that the user should resolve.
// Implementing this interface is optional; non-versioned skills continue to
// work as before (last-registered wins with no conflict warning).
type VersionedSkill interface {
	Skill
	// Version returns a semver or arbitrary version string for the skill.
	Version() string
}

// SkillDef is the on-disk skill.json format.
type SkillDef struct {
	Name           string   `json:"name"`
	ProviderCompat []string `json:"provider_compat,omitempty"`
	PromptFile     string   `json:"prompt_file,omitempty"`
	RulesFile      string   `json:"rules_file,omitempty"`
}

// FilesystemSkill implements Skill from a directory on disk.
type FilesystemSkill struct {
	dir            string
	def            SkillDef
	promptFragment string
	ruleContent    string
}

// LoadFromDir reads a skill directory and returns a FilesystemSkill.
// skill.json is required. prompt.md and rules.md are optional (empty string if absent).
func LoadFromDir(dir string) (*FilesystemSkill, error) {
	defPath := filepath.Join(dir, "skill.json")
	defData, err := os.ReadFile(defPath)
	if err != nil {
		return nil, fmt.Errorf("skills: LoadFromDir %q: skill.json: %w", dir, err)
	}
	var def SkillDef
	if err := json.Unmarshal(defData, &def); err != nil {
		return nil, fmt.Errorf("skills: LoadFromDir %q: skill.json parse: %w", dir, err)
	}

	promptFile := def.PromptFile
	if promptFile == "" {
		promptFile = "prompt.md"
	}
	promptContent, err := os.ReadFile(filepath.Join(dir, promptFile))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("skills: LoadFromDir %q: %s: %w", dir, promptFile, err)
	}

	rulesFile := def.RulesFile
	if rulesFile == "" {
		rulesFile = "rules.md"
	}
	rulesContent, err := os.ReadFile(filepath.Join(dir, rulesFile))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("skills: LoadFromDir %q: %s: %w", dir, rulesFile, err)
	}

	return &FilesystemSkill{
		dir:            dir,
		def:            def,
		promptFragment: string(promptContent),
		ruleContent:    string(rulesContent),
	}, nil
}

func (s *FilesystemSkill) Name() string                 { return s.def.Name }
func (s *FilesystemSkill) Description() string          { return "" }
func (s *FilesystemSkill) SystemPromptFragment() string { return s.promptFragment }
func (s *FilesystemSkill) RuleContent() string          { return s.ruleContent }

func (s *FilesystemSkill) Tools() []tools.Tool {
	promptTools, err := LoadToolsFromDir(s.dir)
	if err != nil || len(promptTools) == 0 {
		return nil
	}
	result := make([]tools.Tool, len(promptTools))
	for i, pt := range promptTools {
		result[i] = pt
	}
	return result
}
