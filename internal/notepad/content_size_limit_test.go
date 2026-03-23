package notepad

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeOSDir(path string) error {
	return os.MkdirAll(path, 0o750)
}

func TestManager_Create_ContentTooLarge(t *testing.T) {
	tmp := t.TempDir()
	m := NewManager(filepath.Join(tmp, "global"), "")
	// Create content slightly over the 1MB limit
	bigContent := strings.Repeat("x", maxNotepadContentBytes+1)
	err := m.Create("big-note", bigContent)
	if err == nil {
		t.Error("expected error for content exceeding size limit")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("expected 'too large' in error, got: %v", err)
	}
}

func TestManager_Create_ContentAtLimit(t *testing.T) {
	tmp := t.TempDir()
	m := NewManager(filepath.Join(tmp, "global"), "")
	// Content exactly at the limit should be accepted
	content := strings.Repeat("x", maxNotepadContentBytes)
	err := m.Create("limit-note", content)
	if err != nil {
		t.Fatalf("expected success at exact limit, got: %v", err)
	}
}

func TestManager_List(t *testing.T) {
	tmp := t.TempDir()
	gdir := filepath.Join(tmp, "global")
	writeTestNotepad(t, gdir, "note-a", "AAA")
	writeTestNotepad(t, gdir, "note-b", "BBB")
	m := NewManager(gdir, "")
	nps, err := m.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(nps) != 2 {
		t.Errorf("expected 2 notepads, got %d", len(nps))
	}
}

func TestManager_Create_Duplicate(t *testing.T) {
	tmp := t.TempDir()
	m := NewManager(filepath.Join(tmp, "global"), "")
	if err := m.Create("dup-note", "first"); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	err := m.Create("dup-note", "second")
	if err == nil {
		t.Error("expected error for duplicate notepad name")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestManager_Delete_InvalidName(t *testing.T) {
	tmp := t.TempDir()
	m := NewManager(filepath.Join(tmp, "global"), "")
	err := m.Delete("../evil")
	if err == nil {
		t.Error("expected error for invalid name")
	}
}

func TestManager_Delete_NotFound(t *testing.T) {
	tmp := t.TempDir()
	m := NewManager(filepath.Join(tmp, "global"), "")
	err := m.Delete("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent notepad")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestManager_Get_NotFound(t *testing.T) {
	tmp := t.TempDir()
	m := NewManager(filepath.Join(tmp, "global"), "")
	_, err := m.Get("ghost")
	if err == nil {
		t.Error("expected error for nonexistent notepad")
	}
}

func TestManager_Create_UsesProjectDir(t *testing.T) {
	tmp := t.TempDir()
	gdir := filepath.Join(tmp, "global")
	pdir := filepath.Join(tmp, "project")
	m := NewManager(gdir, pdir)
	if err := m.Create("proj-note", "project content"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Should be in project dir, not global
	np, err := m.Get("proj-note")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if np.Content != "project content" {
		t.Errorf("Content = %q", np.Content)
	}
}

func TestManager_LoadDir_SkipsSubdirs(t *testing.T) {
	tmp := t.TempDir()
	gdir := filepath.Join(tmp, "global")
	writeTestNotepad(t, gdir, "valid", "content")
	// Create a subdirectory that should be ignored
	subdir := filepath.Join(gdir, "subdir.md")
	// Actually create it as a directory
	if err := makeDir(subdir); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	m := NewManager(gdir, "")
	nps, err := m.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(nps) != 1 {
		t.Errorf("expected 1 notepad (skip subdirs), got %d", len(nps))
	}
}

func makeDir(path string) error {
	return makeOSDir(path)
}

func TestManager_LoadDir_SkipsInvalidNames(t *testing.T) {
	tmp := t.TempDir()
	gdir := filepath.Join(tmp, "global")
	writeTestNotepad(t, gdir, "valid-name", "content")
	// Write a file with invalid name characters
	writeTestNotepad(t, gdir, "has spaces", "invalid")
	m := NewManager(gdir, "")
	nps, err := m.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(nps) != 1 {
		t.Errorf("expected 1 notepad (skip invalid names), got %d", len(nps))
	}
}
