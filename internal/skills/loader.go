package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/scrypster/huginn/internal/logger"
)

// maxSkillFileSizeBytes is the maximum allowed size for a skill markdown file
// loaded by LoadAll. Files larger than this are skipped with an error entry.
// 512 KB is generous for any realistic skill definition.
const maxSkillFileSizeBytes = 512 * 1024

var knownRuleFiles = []string{
	".cursorrules",
	".cursor/rules",
	"CLAUDE.md",
	".claude/CLAUDE.md",
	".huginn/rules.md",
	".github/copilot-instructions.md",
}

type Loader struct {
	skillsDir string
}

func NewLoader(skillsDir string) *Loader {
	return &Loader{skillsDir: skillsDir}
}

func DefaultLoader() *Loader {
	home, err := os.UserHomeDir()
	if err != nil {
		return &Loader{skillsDir: filepath.Join(".huginn", "skills")}
	}
	return &Loader{skillsDir: filepath.Join(home, ".huginn", "skills")}
}

// LoadAll scans skillsDir for *.md files and loads each as a MarkdownSkill.
// Invalid skill files are collected into the returned []error slice rather than
// silently discarded. Successfully loaded skills are always returned even when
// some files fail to parse.
// Returns an empty (non-nil) slice if skillsDir does not exist.
func (l *Loader) LoadAll() ([]Skill, []error) {
	result := make([]Skill, 0)
	entries, err := os.ReadDir(l.skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, []error{fmt.Errorf("skills: LoadAll: read dir %q: %w", l.skillsDir, err)}
	}

	// Load manifest to check enabled status
	manifestPath := filepath.Join(l.skillsDir, "installed.json")
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		// Log warning but continue (manifest may not exist yet)
		logger.Warn("skills: LoadAll: loading manifest", "path", manifestPath, "err", err)
		manifest = &Manifest{Entries: []InstalledEntry{}}
	}

	var errs []error
	for _, entry := range entries {
		// Skip directories
		if entry.IsDir() {
			continue
		}
		// Only process .md files
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		skillPath := filepath.Join(l.skillsDir, entry.Name())
		info, statErr := os.Stat(skillPath)
		if statErr != nil {
			errs = append(errs, &SkillLoadError{File: entry.Name(), Err: statErr})
			continue
		}
		if info.Size() > maxSkillFileSizeBytes {
			errs = append(errs, &SkillLoadError{File: entry.Name(), Err: fmt.Errorf("skill file too large: %d bytes (max %d)", info.Size(), maxSkillFileSizeBytes)})
			continue
		}
		s, err := LoadMarkdownSkill(skillPath)
		if err != nil {
			errs = append(errs, &SkillLoadError{File: entry.Name(), Err: err})
			continue
		}

		// Deny-by-default: only load skills explicitly enabled in the manifest.
		// A skill absent from the manifest (new install, corrupt file, manual drop)
		// is NOT loaded. Explicit opt-in is required via the enable API.
		entry := manifest.Get(s.Name())
		if entry == nil || !entry.Enabled {
			continue
		}

		result = append(result, s)
	}
	return result, errs
}

// LoadRuleFiles scans workspaceRoot for known provider rule file patterns.
// Returns concatenated content with headers. Returns empty string if workspaceRoot is empty or no files found.
func (l *Loader) LoadRuleFiles(workspaceRoot string) string {
	if workspaceRoot == "" {
		return ""
	}
	var parts []string
	for _, pattern := range knownRuleFiles {
		path := filepath.Join(workspaceRoot, pattern)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		header := fmt.Sprintf("// Rules from: %s", pattern)
		parts = append(parts, header+"\n"+strings.TrimRight(string(data), "\n"))
	}
	return strings.Join(parts, "\n\n")
}
