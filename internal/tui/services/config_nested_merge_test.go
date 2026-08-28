package services

import (
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/config"
)

// TestDirectConfigService_Save_NestedFieldsMergeIndependently is the
// regression test for the nested-granularity form of the config clobber: a
// merge that stops at TOP-LEVEL fields reproduces the same bug one level
// down. A caller changing only backend.provider would assign its whole stale
// BackendConfig, reverting a backend.api_key another writer saved in the
// meantime — precisely the field main.go's relay UpdateModelConfig closures
// write. The merge must recurse into nested structs.
func TestDirectConfigService_Save_NestedFieldsMergeIndependently(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".huginn", "config.json")

	seed := config.Default()
	seed.Backend.APIKey = "sk-original"
	seed.WebUI.Port = 9100
	if err := seed.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	live, err := config.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewDirectConfigService(live)
	snapshot := svc.Get() // stale snapshot: APIKey=sk-original, Port=9100

	// Another writer rotates the API key and changes the port on disk.
	if err := config.UpdateAt(path, func(c *config.Config) {
		c.Backend.APIKey = "sk-rotated"
		c.WebUI.Port = 9200
	}); err != nil {
		t.Fatal(err)
	}

	// The TUI changes ONE nested field in each struct.
	snapshot.Backend.Provider = "anthropic"
	snapshot.WebUI.AutoOpen = !snapshot.WebUI.AutoOpen
	if err := svc.Save(snapshot); err != nil {
		t.Fatal(err)
	}

	final, err := config.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if final.Backend.APIKey != "sk-rotated" {
		t.Errorf("NESTED CLOBBER: Backend.APIKey reverted to %q", final.Backend.APIKey)
	}
	if final.WebUI.Port != 9200 {
		t.Errorf("NESTED CLOBBER: WebUI.Port reverted to %d", final.WebUI.Port)
	}
	if final.Backend.Provider != "anthropic" {
		t.Errorf("intended change lost: Provider=%q", final.Backend.Provider)
	}
}
