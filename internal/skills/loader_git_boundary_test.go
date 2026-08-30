package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// LoadRuleFiles walks UP from the working directory, but the first .git
// marker ends the walk. Two things must both hold, and they pull in opposite
// directions — hence one test covering both:
//
//  1. Rules at a repo root ARE picked up when the agent works in a
//     subdirectory of that repo (the point of walking at all).
//  2. Rules ABOVE the repo are NOT picked up — the repo boundary is the
//     scope boundary. Crossing it would inject a sibling project's
//     instructions into this one.
func TestLoadRuleFiles_StopsAtGitBoundary(t *testing.T) {
	outer := t.TempDir()
	if err := os.WriteFile(filepath.Join(outer, "CLAUDE.md"), []byte("OUTSIDE-THE-REPO"), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join(outer, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte("REPO-ROOT-RULES"), 0o644); err != nil {
		t.Fatal(err)
	}

	sub := filepath.Join(repo, "internal", "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "AGENTS.md"), []byte("SUBDIR-RULES"), 0o644); err != nil {
		t.Fatal(err)
	}

	l := &Loader{}
	out := l.LoadRuleFiles(sub)

	if !strings.Contains(out, "SUBDIR-RULES") {
		t.Errorf("rules in the working directory must be loaded, got:\n%s", out)
	}
	if !strings.Contains(out, "REPO-ROOT-RULES") {
		t.Errorf("rules at the repo root must be loaded when working in a subdirectory, got:\n%s", out)
	}
	if strings.Contains(out, "OUTSIDE-THE-REPO") {
		t.Errorf("the walk must stop at the .git boundary and NOT load rules from above the repo, got:\n%s", out)
	}
}

// AGENTS.md was added to knownRuleFiles in this wave; pin that it is actually
// recognised, not just listed.
func TestLoadRuleFiles_RecognisesAgentsMD(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("AGENTS-CONTENT"), 0o644); err != nil {
		t.Fatal(err)
	}

	l := &Loader{}
	out := l.LoadRuleFiles(repo)
	if !strings.Contains(out, "AGENTS-CONTENT") {
		t.Errorf("AGENTS.md must be loaded as a known rule file, got:\n%s", out)
	}
	if !strings.Contains(out, "AGENTS.md") {
		t.Errorf("output should label the source file, got:\n%s", out)
	}
}

// LoadRuleFiles must cap an oversized single rule file, matching the notepad
// 32KB precedent, and mark the truncation clearly rather than dropping the
// file or silently blowing the context budget.
func TestLoadRuleFiles_OversizedFileTruncatesWithMarker(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	over := 3 * 1024
	content := strings.Repeat("x", maxRuleFileBytes+over)
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	l := &Loader{}
	out := l.LoadRuleFiles(repo)

	if !strings.Contains(out, "truncated") {
		t.Errorf("expected a truncation marker in output, got:\n%s", out[:200])
	}
	if len(out) >= len(content) {
		t.Errorf("expected truncated output shorter than the original file: out=%d content=%d", len(out), len(content))
	}
}

// A small rule file well under the cap must be loaded byte-for-byte, with no
// truncation marker attached.
func TestLoadRuleFiles_SmallFileUntouched(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "small rule content"
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	l := &Loader{}
	out := l.LoadRuleFiles(repo)

	if !strings.Contains(out, content) {
		t.Errorf("expected small file content intact, got:\n%s", out)
	}
	if strings.Contains(out, "truncated") {
		t.Errorf("small file should not carry a truncation marker, got:\n%s", out)
	}
}

// The TOTAL size across every rule file walked (multiple patterns per level,
// multiple levels up to the git root) must stay bounded — a directory tree
// with many rule files must not concatenate unboundedly.
func TestLoadRuleFiles_TotalSizeBounded(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Write every known rule pattern at the repo root, each near the
	// per-file cap, so the total comfortably exceeds maxRuleFilesTotalBytes.
	nearCapContent := strings.Repeat("y", maxRuleFileBytes-100)
	for _, pattern := range knownRuleFiles {
		full := filepath.Join(repo, pattern)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(nearCapContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	l := &Loader{}
	out := l.LoadRuleFiles(repo)

	if len(out) > maxRuleFilesTotalBytes+8*1024 { // small slack for headers/markers
		t.Errorf("expected total output bounded near %d bytes, got %d", maxRuleFilesTotalBytes, len(out))
	}
	if !strings.Contains(out, "reached") {
		t.Errorf("expected a marker noting the total cap was reached, got tail:\n%s", out[len(out)-200:])
	}
}
