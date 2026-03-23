package notepad

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestNotepad(t *testing.T, dir, name, content string) {
	t.Helper()
	os.MkdirAll(dir, 0o750)
	os.WriteFile(filepath.Join(dir, name+".md"), []byte(content), 0o640)
}

func TestManager_Load_GlobalOnly(t *testing.T) {
	tmp := t.TempDir()
	gdir := filepath.Join(tmp, "global")
	writeTestNotepad(t, gdir, "rules", "All routes must be versioned.")
	m := NewManager(gdir, "")
	nps, err := m.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(nps) != 1 {
		t.Fatalf("expected 1, got %d", len(nps))
	}
	if nps[0].Name != "rules" {
		t.Errorf("Name = %q", nps[0].Name)
	}
}

func TestManager_Load_ProjectWins(t *testing.T) {
	tmp := t.TempDir()
	gdir := filepath.Join(tmp, "g")
	pdir := filepath.Join(tmp, "p")
	writeTestNotepad(t, gdir, "rules", "Global rules.")
	writeTestNotepad(t, pdir, "rules", "Project rules.")
	m := NewManager(gdir, pdir)
	nps, err := m.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(nps) != 1 {
		t.Fatalf("expected 1, got %d", len(nps))
	}
	if nps[0].Content != "Project rules." {
		t.Errorf("expected project to win")
	}
}

func TestManager_Create_AndGet(t *testing.T) {
	tmp := t.TempDir()
	m := NewManager(filepath.Join(tmp, "global"), "")
	if err := m.Create("my-note", "Some content."); err != nil {
		t.Fatalf("Create: %v", err)
	}
	np, err := m.Get("my-note")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if np.Content != "Some content." {
		t.Errorf("Content = %q", np.Content)
	}
}

func TestManager_Get_CaseInsensitive(t *testing.T) {
	tmp := t.TempDir()
	gdir := filepath.Join(tmp, "global")
	writeTestNotepad(t, gdir, "MyNote", "Content here.")
	m := NewManager(gdir, "")
	// Get with different case should still find it
	np, err := m.Get("mynote")
	if err != nil {
		t.Fatalf("Get case-insensitive: %v", err)
	}
	if np.Content != "Content here." {
		t.Errorf("Content = %q", np.Content)
	}
}

func TestManager_Create_InvalidName(t *testing.T) {
	tmp := t.TempDir()
	m := NewManager(filepath.Join(tmp, "global"), "")
	err := m.Create("../../etc/passwd", "evil")
	if err == nil {
		t.Error("expected error for path-traversal name")
	}
}

func TestManager_Delete_Existing(t *testing.T) {
	tmp := t.TempDir()
	gdir := filepath.Join(tmp, "global")
	writeTestNotepad(t, gdir, "del-me", "Content.")
	m := NewManager(gdir, "")
	if err := m.Delete("del-me"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := m.Get("del-me"); err == nil {
		t.Error("expected error after delete")
	}
}
