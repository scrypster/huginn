package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLoadProjectInstructions_HuginnMd(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "# Project Instructions\nDo the thing."
	if err := os.WriteFile(filepath.Join(dir, ".huginn.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadProjectInstructions(dir)
	if got != content {
		t.Errorf("got %q, want %q", got, content)
	}
}

func TestLoadProjectInstructions_HuginnSubdir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".huginn"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "Instructions from subdir."
	if err := os.WriteFile(filepath.Join(dir, ".huginn", "instructions.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadProjectInstructions(dir)
	if got != content {
		t.Errorf("got %q, want %q", got, content)
	}
}

func TestLoadProjectInstructions_DotHuginnMdWinsOverSubdir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".huginn"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".huginn.md"), []byte("top"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".huginn", "instructions.md"), []byte("subdir"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadProjectInstructions(dir)
	if got != "top" {
		t.Errorf("expected .huginn.md to win, got %q", got)
	}
}

func TestLoadProjectInstructions_WalksUpToGitRoot(t *testing.T) {
	root := t.TempDir()
	subdir := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "Root instructions."
	if err := os.WriteFile(filepath.Join(root, ".huginn.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadProjectInstructions(subdir)
	if got != content {
		t.Errorf("got %q, want %q", got, content)
	}
}

func TestLoadProjectInstructions_StopsAtGitRoot(t *testing.T) {
	outer := t.TempDir()
	inner := filepath.Join(outer, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outer, ".huginn.md"), []byte("outer"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(inner, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := LoadProjectInstructions(inner)
	if got != "" {
		t.Errorf("expected empty (stopped at git root), got %q", got)
	}
}

func TestLoadProjectInstructions_NoFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := LoadProjectInstructions(dir)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestLoadProjectInstructions_WhitespaceStripped(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".huginn.md"), []byte("  content  \n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadProjectInstructions(dir)
	if got != "content" {
		t.Errorf("expected trimmed content, got %q", got)
	}
}

func TestLoadProjectInstructions_OversizedFileTruncatesWithMarker(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 5 KB over the 32KB cap.
	over := 5 * 1024
	content := strings.Repeat("a", maxProjectInstructionsBytes+over)
	if err := os.WriteFile(filepath.Join(dir, ".huginn.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadProjectInstructions(dir)
	if !strings.Contains(got, "…[truncated, 5 KB over cap]") {
		t.Errorf("expected truncation marker, got tail: %q", got[len(got)-80:])
	}
	// The kept content should be exactly the cap, plus the marker.
	if len(got) >= len(content) {
		t.Errorf("expected truncated content to be shorter than original: got %d, original %d", len(got), len(content))
	}
	if !strings.HasPrefix(got, strings.Repeat("a", 100)) {
		t.Error("expected truncated content to retain the file's leading bytes")
	}
}

func TestLoadProjectInstructions_SmallFileUntouched(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "# Small\nJust a normal small instructions file."
	if err := os.WriteFile(filepath.Join(dir, ".huginn.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadProjectInstructions(dir)
	if got != content {
		t.Errorf("small file should be untouched: got %q, want %q", got, content)
	}
	if strings.Contains(got, "truncated") {
		t.Error("small file should not carry a truncation marker")
	}
}

func TestLoadProjectInstructions_ExactCapUntouched(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("b", maxProjectInstructionsBytes)
	if err := os.WriteFile(filepath.Join(dir, ".huginn.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadProjectInstructions(dir)
	if got != content {
		t.Error("file exactly at the cap should not be truncated")
	}
}

func TestLoadGlobalInstructions_Missing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	got := LoadGlobalInstructions()
	if got != "" {
		t.Errorf("expected empty for missing file, got %q", got)
	}
}

func TestLoadGlobalInstructions_Present(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	dir := filepath.Join(tmp, ".config", "huginn")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "Global coding style: prefer small functions."
	if err := os.WriteFile(filepath.Join(dir, "instructions.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadGlobalInstructions()
	if got != content {
		t.Errorf("got %q, want %q", got, content)
	}
}

func TestBuildAgentSystemPrompt_InstructionInjection(t *testing.T) {
	tests := []struct {
		name                string
		globalInstructions  string
		projectInstructions string
		wantContains        []string
		wantOrder           [][2]string
	}{
		{
			name:                "both present",
			globalInstructions:  "GLOBAL",
			projectInstructions: "PROJECT",
			wantContains:        []string{"GLOBAL", "PROJECT", "Huginn"},
			wantOrder:           [][2]string{{"GLOBAL", "PROJECT"}, {"PROJECT", "Huginn"}},
		},
		{
			name:               "only global",
			globalInstructions: "GLOBAL",
			wantContains:       []string{"GLOBAL", "Huginn"},
			wantOrder:          [][2]string{{"GLOBAL", "Huginn"}},
		},
		{
			name:                "only project",
			projectInstructions: "PROJECT",
			wantContains:        []string{"PROJECT", "Huginn"},
			wantOrder:           [][2]string{{"PROJECT", "Huginn"}},
		},
		{
			name:         "neither present",
			wantContains: []string{"Huginn"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildAgentSystemPrompt("", "", nil, tc.globalInstructions, tc.projectInstructions, "", "", "", "", "")
			for _, want := range tc.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("prompt missing %q\nfull prompt:\n%s", want, got)
				}
			}
			for _, pair := range tc.wantOrder {
				idxA := strings.Index(got, pair[0])
				idxB := strings.Index(got, pair[1])
				if idxA >= idxB {
					t.Errorf("%q should appear before %q in prompt", pair[0], pair[1])
				}
			}
		})
	}
}

// TestTruncateInstructions_RuneSafe verifies that when the byte cap lands in
// the middle of a multi-byte UTF-8 rune, truncation backs off to a rune
// boundary so the prompt never carries invalid UTF-8. Regression for the
// raw-byte-slice truncation that could split a rune.
func TestTruncateInstructions_RuneSafe(t *testing.T) {
	// "€" is 3 bytes (E2 82 AC). Pad so the cap (maxProjectInstructionsBytes)
	// falls one byte into a euro sign, guaranteeing a mid-rune boundary.
	pad := strings.Repeat("a", maxProjectInstructionsBytes-1)
	s := pad + "€€€€" // first € straddles the cap
	got := truncateInstructions(s)
	if !utf8.ValidString(got) {
		t.Fatalf("truncated instructions contain invalid UTF-8")
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("expected truncation marker, got tail: %q", got[len(got)-40:])
	}
	// Kept body must be valid and no longer than the cap.
	body := strings.SplitN(got, "\n…[truncated", 2)[0]
	if len(body) > maxProjectInstructionsBytes {
		t.Errorf("body %d exceeds cap %d", len(body), maxProjectInstructionsBytes)
	}
	if !utf8.ValidString(body) {
		t.Error("kept body is not valid UTF-8")
	}
}
