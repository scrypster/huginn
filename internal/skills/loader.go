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
	"AGENTS.md",
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

// maxRuleFileWalkDepth bounds how far LoadRuleFiles walks up the directory
// tree looking for the git root, to prevent runaway traversal on systems
// without a git repo in the path.
const maxRuleFileWalkDepth = 32

// maxRuleFileBytes caps how much of a single rule file (CLAUDE.md, AGENTS.md,
// etc.) is loaded. Matches the notepad 32KB precedent (maxNotepadsChars in
// internal/agent/context.go) — an oversized rule file is truncated with a
// visible marker instead of silently blowing the context budget.
const maxRuleFileBytes = 32 * 1024

// maxRuleFilesTotalBytes bounds the concatenated size of ALL rule files
// LoadRuleFiles collects while walking every directory level up to the git
// root (multiple known patterns per level, multiple levels). Without this,
// a deep tree with several rule files per level can concatenate far more
// than any single-file cap would suggest.
const maxRuleFilesTotalBytes = 4 * maxRuleFileBytes

// LoadRuleFiles scans workspaceRoot, and every ancestor directory up to AND
// INCLUDING the first directory containing a .git marker, for known provider
// rule file patterns. This picks up instructions living above the working
// directory — e.g. rules at a repo root when the agent operates from a
// subdirectory of that repo.
//
// The walk STOPS at the first .git it finds, matching
// agent.LoadProjectInstructions. So rules living ABOVE a repo (a
// workspace-level CLAUDE.md in a multi-project checkout, where the
// subdirectory is itself a separate git repo) are deliberately NOT loaded —
// the repo boundary is the scope boundary, and crossing it would pull in
// instructions from unrelated sibling projects. See
// TestLoadRuleFiles_StopsAtGitBoundary.
//
// Returns concatenated content with headers. Returns empty string if
// workspaceRoot is empty or no files found.
func (l *Loader) LoadRuleFiles(workspaceRoot string) string {
	if workspaceRoot == "" {
		return ""
	}
	root := filepath.Clean(workspaceRoot)
	dir := root
	var parts []string
	totalBytes := 0
	budgetExhausted := false
runwalk:
	for i := 0; i < maxRuleFileWalkDepth; i++ {
		for _, pattern := range knownRuleFiles {
			path := filepath.Join(dir, pattern)
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			label := pattern
			if rel, relErr := filepath.Rel(root, dir); relErr == nil && rel != "." {
				label = filepath.Join(rel, pattern)
			}
			content := strings.TrimRight(string(data), "\n")
			content, fileTruncated := truncateRuleFile(content)

			if totalBytes+len(content) > maxRuleFilesTotalBytes {
				budgetExhausted = true
				break runwalk
			}
			totalBytes += len(content)

			header := fmt.Sprintf("// Rules from: %s", label)
			if fileTruncated {
				header += fmt.Sprintf(" (truncated, over %dKB cap)", maxRuleFileBytes/1024)
			}
			parts = append(parts, header+"\n"+content)
		}
		// Stop after processing the directory containing the git root marker.
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root
		}
		dir = parent
	}
	if budgetExhausted {
		parts = append(parts, fmt.Sprintf("// …[remaining rule files skipped, total cap of %dKB reached]", maxRuleFilesTotalBytes/1024))
	}
	return strings.Join(parts, "\n\n")
}

// truncateRuleFile caps content at maxRuleFileBytes, appending a clear
// marker naming how much was cut when truncation occurs. Returns the
// (possibly truncated) content and whether truncation happened.
func truncateRuleFile(content string) (string, bool) {
	if len(content) <= maxRuleFileBytes {
		return content, false
	}
	overBytes := len(content) - maxRuleFileBytes
	overKB := (overBytes + 1023) / 1024
	if overKB < 1 {
		overKB = 1
	}
	// Back the cut off any partial UTF-8 rune so the concatenated rule text
	// never carries an invalid byte sequence (a raw byte slice can split a
	// multi-byte rune).
	cut := runeSafeCut(content, maxRuleFileBytes)
	return content[:cut] + fmt.Sprintf("\n…[truncated, %d KB over cap]", overKB), true
}

// runeSafeCut returns the largest index <= n at which s can be sliced without
// splitting a multi-byte UTF-8 rune, walking back over any trailing UTF-8
// continuation bytes (0b10xxxxxx) that a cut at n would orphan.
func runeSafeCut(s string, n int) int {
	if n >= len(s) {
		return len(s)
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return n
}
