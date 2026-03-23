package headless

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetGitHead_ValidGitRepo(t *testing.T) {
	// The huginn repo itself should have a valid HEAD
	cwd, err := os.Getwd()
	if err != nil {
		t.Skip("cannot get cwd")
	}
	// Walk up to find .git
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("not in a git repo")
		}
		dir = parent
	}

	head := getGitHead(dir)
	if head == "HEAD" {
		t.Skip("git not available or not in a repo with commits")
	}
	if len(head) < 7 {
		t.Errorf("expected SHA-like string, got %q", head)
	}
}

func TestGetGitBranch_ValidGitRepo(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Skip("cannot get cwd")
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("not in a git repo")
		}
		dir = parent
	}

	branch := getGitBranch(dir)
	if branch == "" {
		t.Error("expected non-empty branch")
	}
}

func TestGetChangedFiles_ValidGitRepo(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Skip("cannot get cwd")
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("not in a git repo")
		}
		dir = parent
	}

	files := getChangedFiles(dir)
	// Just verify it doesn't panic and returns a valid slice
	for _, f := range files {
		if f == "" {
			t.Error("getChangedFiles returned empty string entry")
		}
	}
}

func TestHeadlessStoreDir_WithHOME(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	result := headlessStoreDir("/some/workspace")
	if !strings.Contains(result, ".huginn") {
		t.Errorf("expected .huginn in store dir, got %q", result)
	}
	if !strings.Contains(result, "store") {
		t.Errorf("expected 'store' in store dir, got %q", result)
	}
}

func TestHeadlessStoreDir_LongPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	longPath := "/" + strings.Repeat("a", 100)
	result := headlessStoreDir(longPath)
	// The sanitized name should be truncated to 64 chars
	base := filepath.Base(result)
	if len(base) > 64 {
		t.Errorf("expected base name <= 64 chars, got %d: %q", len(base), base)
	}
}

func TestSanitizePath_SpecialChars(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/home/user/project", "_home_user_project"},
		{"C:\\Users\\project", "C__Users_project"},
		{"file:name", "file_name"},
		{"a*b?c", "a_b_c"},
		{"normal", "normal"},
		{"", ""},
	}
	for _, tc := range tests {
		got := sanitizePath(tc.input)
		if got != tc.expected {
			t.Errorf("sanitizePath(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
