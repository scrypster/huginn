package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
