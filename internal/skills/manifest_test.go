package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultManifestPath(t *testing.T) {
	path := DefaultManifestPath()
	if path == "" {
		t.Fatal("DefaultManifestPath() returned empty string, expected non-empty")
	}
	// Should contain .huginn/skills/installed.json
	if !strings.Contains(path, ".huginn") || !strings.Contains(path, "installed.json") {
		t.Errorf("DefaultManifestPath() = %q, expected to contain .huginn and installed.json", path)
	}
}

func TestManifest_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "installed.json")

	// Create a manifest, upsert 2 entries, save
	m := &Manifest{
		Entries: []InstalledEntry{},
		path:    manifestPath,
	}

	m.Upsert(InstalledEntry{
		Name:    "skill-one",
		Source:  "registry",
		Enabled: true,
	})
	m.Upsert(InstalledEntry{
		Name:    "skill-two",
		Source:  "github:user/repo",
		Enabled: false,
	})

	if len(m.Entries) != 2 {
		t.Errorf("after upsert: len(m.Entries) = %d, want 2", len(m.Entries))
	}

	// Save to disk
	if err := m.Save(); err != nil {
		t.Fatalf("m.Save(): %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest file not created: %v", err)
	}

	// Load it back
	m2, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	// Verify entries
	if len(m2.Entries) != 2 {
		t.Errorf("after reload: len(m2.Entries) = %d, want 2", len(m2.Entries))
	}

	// Check first entry
	if m2.Entries[0].Name != "skill-one" {
		t.Errorf("entry 0: got Name=%q, want skill-one", m2.Entries[0].Name)
	}
	if m2.Entries[0].Source != "registry" || m2.Entries[0].Enabled != true {
		t.Errorf("entry 0: got Source=%s Enabled=%v, want registry true", m2.Entries[0].Source, m2.Entries[0].Enabled)
	}

	// Check second entry
	if m2.Entries[1].Name != "skill-two" {
		t.Errorf("entry 1: got Name=%q, want skill-two", m2.Entries[1].Name)
	}
	if m2.Entries[1].Source != "github:user/repo" || m2.Entries[1].Enabled != false {
		t.Errorf("entry 1: got Source=%s Enabled=%v, want github:user/repo false", m2.Entries[1].Source, m2.Entries[1].Enabled)
	}

	// Verify path is set so Save() works
	if m2.path != manifestPath {
		t.Errorf("m2.path = %q, want %q", m2.path, manifestPath)
	}
}

func TestManifest_Upsert_Update(t *testing.T) {
	m := &Manifest{
		Entries: []InstalledEntry{},
	}

	// Upsert same name twice with different source
	m.Upsert(InstalledEntry{
		Name:    "skill",
		Source:  "registry",
		Enabled: true,
	})

	if len(m.Entries) != 1 {
		t.Errorf("after first upsert: len = %d, want 1", len(m.Entries))
	}

	m.Upsert(InstalledEntry{
		Name:    "skill",
		Source:  "local",
		Enabled: false,
	})

	if len(m.Entries) != 1 {
		t.Errorf("after second upsert: len = %d, want 1 (should update, not add)", len(m.Entries))
	}

	if m.Entries[0].Source != "local" {
		t.Errorf("source after update: got %q, want %q", m.Entries[0].Source, "local")
	}
	if m.Entries[0].Enabled != false {
		t.Errorf("enabled after update: got %v, want false", m.Entries[0].Enabled)
	}
}

func TestManifest_SetEnabled(t *testing.T) {
	m := &Manifest{
		Entries: []InstalledEntry{},
	}

	m.Upsert(InstalledEntry{
		Name:    "skill",
		Source:  "registry",
		Enabled: true,
	})

	// Set to disabled
	ok := m.SetEnabled("skill", false)
	if !ok {
		t.Errorf("SetEnabled(\"skill\", false): returned false, want true")
	}
	if m.Entries[0].Enabled != false {
		t.Errorf("after SetEnabled false: Enabled = %v, want false", m.Entries[0].Enabled)
	}

	// Set to enabled again
	ok = m.SetEnabled("skill", true)
	if !ok {
		t.Errorf("SetEnabled(\"skill\", true): returned false, want true")
	}
	if m.Entries[0].Enabled != true {
		t.Errorf("after SetEnabled true: Enabled = %v, want true", m.Entries[0].Enabled)
	}

	// Try to set enabled on nonexistent skill
	ok = m.SetEnabled("nonexistent", false)
	if ok {
		t.Errorf("SetEnabled(\"nonexistent\", false): returned true, want false")
	}
}

func TestManifest_Remove(t *testing.T) {
	m := &Manifest{
		Entries: []InstalledEntry{},
	}

	m.Upsert(InstalledEntry{
		Name:    "skill-one",
		Source:  "registry",
		Enabled: true,
	})
	m.Upsert(InstalledEntry{
		Name:    "skill-two",
		Source:  "registry",
		Enabled: true,
	})

	if len(m.Entries) != 2 {
		t.Errorf("after upserts: len = %d, want 2", len(m.Entries))
	}

	// Remove existing skill
	ok := m.Remove("skill-one")
	if !ok {
		t.Errorf("Remove(\"skill-one\"): returned false, want true")
	}
	if len(m.Entries) != 1 {
		t.Errorf("after Remove: len = %d, want 1", len(m.Entries))
	}
	if m.Entries[0].Name != "skill-two" {
		t.Errorf("remaining entry: Name = %q, want skill-two", m.Entries[0].Name)
	}

	// Remove the last one
	ok = m.Remove("skill-two")
	if !ok {
		t.Errorf("Remove(\"skill-two\"): returned false, want true")
	}
	if len(m.Entries) != 0 {
		t.Errorf("after removing last: len = %d, want 0", len(m.Entries))
	}

	// Remove nonexistent skill
	ok = m.Remove("nonexistent")
	if ok {
		t.Errorf("Remove(\"nonexistent\"): returned true, want false")
	}
}

func TestManifest_Get(t *testing.T) {
	m := &Manifest{
		Entries: []InstalledEntry{},
	}

	m.Upsert(InstalledEntry{
		Name:    "skill",
		Source:  "registry",
		Enabled: true,
	})

	// Get existing skill
	entry := m.Get("skill")
	if entry == nil {
		t.Fatal("Get(\"skill\"): returned nil, want non-nil")
	}
	if entry.Name != "skill" || entry.Source != "registry" {
		t.Errorf("Get(\"skill\"): got %+v, want Name=skill Source=registry", *entry)
	}

	// Get nonexistent skill
	entry = m.Get("nonexistent")
	if entry != nil {
		t.Errorf("Get(\"nonexistent\"): returned %v, want nil", entry)
	}
}

func TestManifest_NotExist(t *testing.T) {
	// Try to load from nonexistent path
	m, err := LoadManifest("/nonexistent/path/to/installed.json")
	if err != nil {
		t.Fatalf("LoadManifest on nonexistent file: expected nil error, got %v", err)
	}
	if m == nil {
		t.Fatal("LoadManifest on nonexistent file: returned nil manifest, want non-nil")
	}
	if len(m.Entries) != 0 {
		t.Errorf("empty manifest: len(Entries) = %d, want 0", len(m.Entries))
	}
	if m.path != "/nonexistent/path/to/installed.json" {
		t.Errorf("path not set: got %q, want /nonexistent/path/to/installed.json", m.path)
	}
}

