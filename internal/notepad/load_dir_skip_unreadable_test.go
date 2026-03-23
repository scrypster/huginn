package notepad

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// loadDir — skips unreadable files (ReadFile returns error → continue)
// ---------------------------------------------------------------------------

func TestManager_loadDir_SkipsUnreadableFile95(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permission checks")
	}
	dir := t.TempDir()
	// Write two files: one unreadable, one valid.
	unreadable := filepath.Join(dir, "unreadable95.md")
	if err := os.WriteFile(unreadable, []byte("content"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(unreadable, 0o640) //nolint:errcheck

	valid := filepath.Join(dir, "valid95read.md")
	if err := os.WriteFile(valid, []byte("valid content"), 0o640); err != nil {
		t.Fatal(err)
	}

	m := NewManager(dir, "")
	nps, err := m.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Should return only the readable file, skipping the unreadable one.
	if len(nps) != 1 {
		t.Errorf("expected 1 notepad (unreadable skipped), got %d", len(nps))
	}
}

// ---------------------------------------------------------------------------
// loadDir — skips files with invalid/malformed YAML frontmatter
// (ParseNotepad returns error → continue)
// ---------------------------------------------------------------------------

func TestManager_loadDir_SkipsMalformedYAML95(t *testing.T) {
	dir := t.TempDir()
	// File with malformed YAML frontmatter — causes ParseNotepad to error.
	badYAML := filepath.Join(dir, "badyaml95.md")
	// Use a tab character inside YAML block to force a parse error.
	if err := os.WriteFile(badYAML, []byte("---\n\tinvalid: yaml: : :\n---\nbody"), 0o640); err != nil {
		t.Fatal(err)
	}

	valid := filepath.Join(dir, "validyaml95.md")
	if err := os.WriteFile(valid, []byte("valid content"), 0o640); err != nil {
		t.Fatal(err)
	}

	m := NewManager(dir, "")
	nps, err := m.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// The malformed YAML file should be skipped; only valid one returned.
	for _, np := range nps {
		if np.Name == "badyaml95" {
			t.Error("expected malformed YAML file to be skipped")
		}
	}
}

// ---------------------------------------------------------------------------
// Load — projectDir loadDir error (projectDir exists but is not a directory)
// ---------------------------------------------------------------------------

func TestManager_Load_ProjectDirError95(t *testing.T) {
	globalDir := t.TempDir()
	// Set projectDir to a regular file (not a directory) — ReadDir will fail
	// with a non-NotExist error.
	regularFile := filepath.Join(t.TempDir(), "notadir95")
	if err := os.WriteFile(regularFile, []byte("data"), 0o640); err != nil {
		t.Fatal(err)
	}

	m := NewManager(globalDir, regularFile)
	_, err := m.Load()
	if err == nil {
		t.Error("expected error when projectDir is a regular file, got nil")
	}
}

// ---------------------------------------------------------------------------
// Create — WriteFile error (directory is read-only)
// ---------------------------------------------------------------------------

func TestManager_Create_WriteFileError95(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permission checks")
	}
	dir := t.TempDir()
	// Make the dir read-only so WriteFile to the .tmp file fails.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755) //nolint:errcheck

	m := NewManager(dir, "")
	err := m.Create("writeerror95", "some content")
	if err == nil {
		t.Error("expected error when directory is read-only, got nil")
	}
}

// ---------------------------------------------------------------------------
// Get — Load error propagation (globalDir is a regular file)
// ---------------------------------------------------------------------------

func TestManager_Get_LoadError95(t *testing.T) {
	// Set globalDir to a regular file — loadDir will call ReadDir on a file,
	// which returns a non-NotExist error, causing Load to return an error.
	regularFile := filepath.Join(t.TempDir(), "notadir95b")
	if err := os.WriteFile(regularFile, []byte("data"), 0o640); err != nil {
		t.Fatal(err)
	}

	m := NewManager(regularFile, "")
	_, err := m.Get("anything")
	if err == nil {
		t.Error("expected error when globalDir is a regular file, got nil")
	}
}

// ---------------------------------------------------------------------------
// DefaultManager — exercises the home-dir path (globalDir has .huginn/notepads suffix)
// ---------------------------------------------------------------------------

func TestDefaultManager_GlobalDirPath95(t *testing.T) {
	m, err := DefaultManager("")
	if err != nil {
		t.Fatalf("DefaultManager: %v", err)
	}
	if !strings.HasSuffix(m.globalDir, filepath.Join(".huginn", "notepads")) {
		t.Errorf("unexpected globalDir: %q", m.globalDir)
	}
}

// ---------------------------------------------------------------------------
// Manager.Get — found path (case-insensitive lookup returns notepad)
// ---------------------------------------------------------------------------

func TestManager_Get_Found95(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, "")
	content := "# My Note\nSome content here."
	path := filepath.Join(dir, "mynote95.md")
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}

	np, err := m.Get("mynote95")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if np.Name != "mynote95" {
		t.Errorf("expected name 'mynote95', got %q", np.Name)
	}
}

// ---------------------------------------------------------------------------
// Manager.Create — already exists (second call fails)
// ---------------------------------------------------------------------------

func TestManager_Create_AlreadyExists95(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, "")
	if err := m.Create("mynote95", "first"); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	err := m.Create("mynote95", "second")
	if err == nil {
		t.Error("expected error for duplicate notepad, got nil")
	}
}

// ---------------------------------------------------------------------------
// Manager.Create — content exactly at the limit (boundary check)
// ---------------------------------------------------------------------------

func TestManager_Create_ContentExactlyAtLimit95(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, "")
	// Content exactly at maxNotepadContentBytes should succeed.
	exactContent := strings.Repeat("x", maxNotepadContentBytes)
	if err := m.Create("exactlimit95", exactContent); err != nil {
		t.Errorf("expected success at exact limit, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Manager.Delete — found in projectDir (projectDir checked first)
// ---------------------------------------------------------------------------

func TestManager_Delete_FromProjectDir95(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()
	m := NewManager(globalDir, projectDir)
	path := filepath.Join(projectDir, "projnote95.md")
	if err := os.WriteFile(path, []byte("content"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete("projnote95"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected file to be deleted")
	}
}

// ---------------------------------------------------------------------------
// loadDir — skips non-.md files and directories
// ---------------------------------------------------------------------------

func TestManager_loadDir_SkipsNonMDFiles95(t *testing.T) {
	dir := t.TempDir()
	// Non-.md file should be skipped.
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("skip me"), 0o640); err != nil {
		t.Fatal(err)
	}
	// Valid .md file.
	if err := os.WriteFile(filepath.Join(dir, "note95.md"), []byte("keep me"), 0o640); err != nil {
		t.Fatal(err)
	}
	// Subdirectory should be skipped.
	if err := os.Mkdir(filepath.Join(dir, "subdir95"), 0o750); err != nil {
		t.Fatal(err)
	}

	m := NewManager(dir, "")
	nps, err := m.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(nps) != 1 {
		t.Errorf("expected 1 notepad, got %d", len(nps))
	}
	if nps[0].Name != "note95" {
		t.Errorf("expected name 'note95', got %q", nps[0].Name)
	}
}

// ---------------------------------------------------------------------------
// ParseNotepad — frontmatter with scope=project override
// ---------------------------------------------------------------------------

func TestParseNotepad_FrontmatterScopeOverride95(t *testing.T) {
	data := []byte("---\nscope: project\npriority: high\ntags:\n  - go\n---\nBody content here.")
	np, err := ParseNotepad("mynote95", "global", "/tmp/mynote95.md", data)
	if err != nil {
		t.Fatalf("ParseNotepad: %v", err)
	}
	if np.Scope != "project" {
		t.Errorf("expected scope 'project', got %q", np.Scope)
	}
	if np.Priority != 1 {
		t.Errorf("expected priority 1 (high), got %d", np.Priority)
	}
	if len(np.Tags) != 1 || np.Tags[0] != "go" {
		t.Errorf("expected tags [go], got %v", np.Tags)
	}
}

// ---------------------------------------------------------------------------
// Load — sort by priority (high first), then by name (alphabetical)
// ---------------------------------------------------------------------------

func TestManager_Load_SortOrderByPriority95(t *testing.T) {
	dir := t.TempDir()
	aaa := []byte("aaa content")
	bbb := []byte("---\npriority: high\n---\nbbb content")
	if err := os.WriteFile(filepath.Join(dir, "aaa95.md"), aaa, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bbb95.md"), bbb, 0o640); err != nil {
		t.Fatal(err)
	}

	m := NewManager(dir, "")
	nps, err := m.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(nps) != 2 {
		t.Fatalf("expected 2 notepads, got %d", len(nps))
	}
	// bbb95 (high priority) should come first.
	if nps[0].Name != "bbb95" {
		t.Errorf("expected bbb95 first (high priority), got %q", nps[0].Name)
	}
}

// ---------------------------------------------------------------------------
// Load — empty globalDir and non-empty projectDir
// ---------------------------------------------------------------------------

func TestManager_Load_EmptyGlobalDirWithProject95(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(projectDir, "projonly95.md"), []byte("proj content"), 0o640); err != nil {
		t.Fatal(err)
	}

	m := NewManager(globalDir, projectDir)
	nps, err := m.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(nps) != 1 {
		t.Fatalf("expected 1 notepad, got %d", len(nps))
	}
	if nps[0].Name != "projonly95" {
		t.Errorf("expected projonly95, got %q", nps[0].Name)
	}
}
