package relay_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/relay"
)

// TestGetMachineID_IsStable verifies that repeated calls return the same value.
func TestGetMachineID_IsStable(t *testing.T) {
	a := relay.GetMachineID()
	b := relay.GetMachineID()
	if a != b {
		t.Errorf("GetMachineID is not stable: %q != %q", a, b)
	}
}

// TestGetMachineID_NonEmpty verifies that the machine ID is non-empty.
func TestGetMachineID_NonEmpty(t *testing.T) {
	id := relay.GetMachineID()
	if id == "" {
		t.Error("GetMachineID returned empty string")
	}
}

// TestGetMachineID_IsHex verifies that the machine ID is a pure 8-char hex string.
func TestGetMachineID_IsHex(t *testing.T) {
	id := relay.GetMachineID()
	if len(id) != 8 {
		t.Errorf("GetMachineID %q: expected 8 hex chars, got len %d", id, len(id))
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("GetMachineID %q: non-hex character %q", id, c)
		}
	}
}

// TestNewSatelliteWithStore_NotRegistered returns InProcessHub when not registered.
func TestNewSatelliteWithStore_NotRegistered(t *testing.T) {
	store := &relay.MemoryTokenStore{} // empty — not registered
	sat := relay.NewSatelliteWithStore("wss://example.com", store)
	hub := sat.Hub(context.Background())
	if hub == nil {
		t.Error("expected non-nil Hub from unregistered satellite")
	}
}

// TestNewSatelliteWithStore_StatusNotRegistered verifies Status.Registered is false.
func TestNewSatelliteWithStore_StatusNotRegistered(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	sat := relay.NewSatelliteWithStore("wss://example.com", store)
	status := sat.Status()
	if status.Registered {
		t.Error("expected Registered=false for unregistered satellite")
	}
	if status.Connected {
		t.Error("expected Connected=false for unregistered satellite")
	}
}

// TestNewSatelliteWithStore_StatusRegistered verifies Status.Registered is true after Save.
func TestNewSatelliteWithStore_StatusRegistered(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	_ = store.Save("some-token")
	sat := relay.NewSatelliteWithStore("wss://example.com", store)
	status := sat.Status()
	if !status.Registered {
		t.Error("expected Registered=true after token saved")
	}
}

// TestNewSatellite_DefaultURL verifies the fallback URL when empty string given.
func TestNewSatellite_DefaultURL(t *testing.T) {
	sat := relay.NewSatelliteWithStore("", &relay.MemoryTokenStore{})
	status := sat.Status()
	if status.CloudURL != "wss://api.huginncloud.com" {
		t.Errorf("expected default CloudURL, got %q", status.CloudURL)
	}
}

// TestSatelliteVersion_FallbackToDev verifies that satelliteVersion returns "dev"
// when the env var is not set (tested indirectly via Hub — no panics).
func TestSatelliteHub_NotRegistered_ReturnsInProcessHub(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	sat := relay.NewSatelliteWithStore("wss://example.com", store)
	// Hub with no token should return an InProcessHub (which is a no-op).
	hub := sat.Hub(context.Background())
	// Send must not return an error on InProcessHub.
	if err := hub.Send("machine-1", relay.Message{Type: relay.MsgToken}); err != nil {
		t.Errorf("expected nil error from InProcessHub.Send, got %v", err)
	}
}

// TestNewRegistrarWithStore_Status verifies Status on unregistered registrar.
func TestNewRegistrarWithStore_Status(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	reg := relay.NewRegistrarWithStore("https://example.com", store)
	registered, machineID := reg.Status()
	if registered {
		t.Error("expected registered=false for new registrar")
	}
	if machineID == "" {
		t.Error("expected non-empty machineID")
	}
}

// TestRegistrar_DeliverCode_NoOp_NoPendingCode verifies DeliverCode is safe before Register.
func TestRegistrar_DeliverCode_NoOp_NoPendingCode(t *testing.T) {
	reg := relay.NewRegistrarWithStore("https://example.com", &relay.MemoryTokenStore{})
	reg.DeliverCode("some-code") // must not panic
}

// TestRegistrar_Unregister_RemovesToken verifies Unregister clears the token.
func TestRegistrar_Unregister_RemovesToken(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	_ = store.Save("tok")
	reg := relay.NewRegistrarWithStore("", store)
	if err := reg.Unregister(); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	registered, _ := reg.Status()
	if registered {
		t.Error("expected registered=false after Unregister")
	}
}

// TestIdentity_Save_WritesFile verifies that Save creates the file in HOME.
func TestIdentity_Save_WritesFile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	id := &relay.Identity{
		AgentID:  "agent-abc",
		Endpoint: "https://relay.example.com",
		APIKey:   "key-xyz",
	}
	if err := id.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	path := filepath.Join(tmpHome, ".huginn", "relay.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected relay.json to exist: %v", err)
	}
}

// TestLoadIdentity_AfterSave_RoundTrip verifies LoadIdentity reads back what Save wrote.
func TestLoadIdentity_AfterSave_RoundTrip(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	original := &relay.Identity{
		AgentID:  "round-trip-agent",
		Endpoint: "https://relay.example.com",
		APIKey:   "secret",
	}
	if err := original.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := relay.LoadIdentity()
	if err != nil {
		t.Fatalf("LoadIdentity: %v", err)
	}
	if loaded.AgentID != original.AgentID {
		t.Errorf("AgentID: got %q, want %q", loaded.AgentID, original.AgentID)
	}
	if loaded.Endpoint != original.Endpoint {
		t.Errorf("Endpoint: got %q, want %q", loaded.Endpoint, original.Endpoint)
	}
}

// TestLoadIdentity_CorruptJSON_ReturnsError verifies that corrupt relay.json returns error.
func TestLoadIdentity_CorruptJSON_ReturnsError(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	huginDir := filepath.Join(tmpHome, ".huginn")
	_ = os.MkdirAll(huginDir, 0o750)
	_ = os.WriteFile(filepath.Join(huginDir, "relay.json"), []byte("{invalid json"), 0o600)
	_, err := relay.LoadIdentity()
	if err == nil {
		t.Error("expected error from LoadIdentity with corrupt JSON, got nil")
	}
}
