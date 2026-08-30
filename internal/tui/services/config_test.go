package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/config"
)

// TestDirectConfigService_Save_SurvivesStaleSnapshot is the regression test
// for the config clobber defect as it manifests through the TUI's
// DirectConfigService: a screen that called Get(), held onto that snapshot
// while the config API saved an unrelated field to disk, then called Save()
// with its own change must not revert the API's write.
func TestDirectConfigService_Save_SurvivesStaleSnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".huginn", "config.json")

	seed := config.Default()
	seed.ToolsEnabled = false
	seed.WebUI.Port = 9100
	if err := seed.SaveTo(path); err != nil {
		t.Fatalf("seed SaveTo: %v", err)
	}

	// The TUI service is constructed with a snapshot loaded at this point —
	// analogous to main.go's process-lifetime cfg pointer.
	live, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	svc := NewDirectConfigService(live)

	// A screen reads a snapshot via Get() before the API save below.
	snapshot := svc.Get()

	// The "API" saves field X (ToolsEnabled) via a read-modify-write,
	// simulating handleUpdateConfig's PUT /api/v1/config landing while the
	// TUI process is still running with its older snapshot.
	if err := config.UpdateAt(path, func(cfg *config.Config) {
		cfg.ToolsEnabled = true
	}); err != nil {
		t.Fatalf("UpdateAt (field X): %v", err)
	}

	// The screen now saves an unrelated change (field Y) starting from its
	// stale snapshot.
	snapshot.Backend.Provider = "anthropic"
	if err := svc.Save(snapshot); err != nil {
		t.Fatalf("svc.Save (field Y): %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var final config.Config
	if err := json.Unmarshal(data, &final); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !final.ToolsEnabled {
		t.Fatalf("field X (ToolsEnabled) did not survive the TUI's stale-snapshot save — got false, want true")
	}
	if final.WebUI.Port != 9100 {
		t.Fatalf("unrelated field WebUI.Port was clobbered: got %d, want 9100", final.WebUI.Port)
	}
	if final.Backend.Provider != "anthropic" {
		t.Fatalf("field Y (Backend.Provider) was not applied — got %q, want %q", final.Backend.Provider, "anthropic")
	}
}
