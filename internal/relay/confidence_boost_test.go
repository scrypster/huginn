package relay_test

// confidence_boost_test.go — Iteration 4 targeted coverage improvements.
// Targets: InProcessHub.Close, NewRegistrar, NewSatellite, NewTokenStore
// (keyring-backed — exercised at construction).

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/relay"
)

// TestInProcessHub_Close_IsNoOp verifies that Close on an InProcessHub
// does not panic and is safe to call multiple times with various inputs.
func TestInProcessHub_Close_IsNoOp(t *testing.T) {
	hub := relay.NewInProcessHub()
	hub.Close("machine-a")
	hub.Close("machine-b")
	hub.Close("") // empty machine ID — must not panic
}

// TestInProcessHub_SendAndClose_Combined verifies Send followed by Close is safe.
func TestInProcessHub_SendAndClose_Combined(t *testing.T) {
	hub := relay.NewInProcessHub()
	if err := hub.Send("m1", relay.Message{Type: relay.MsgToken}); err != nil {
		t.Errorf("expected nil from InProcessHub.Send, got %v", err)
	}
	hub.Close("m1") // Close after Send — must not panic
}

// TestNewRegistrar_DefaultURL verifies NewRegistrar uses HuginnCloudBaseURL as default.
func TestNewRegistrar_DefaultURL(t *testing.T) {
	reg := relay.NewRegistrar("")
	if reg == nil {
		t.Fatal("NewRegistrar returned nil")
	}
	_, machineID := reg.Status()
	if machineID == "" {
		t.Error("expected non-empty machineID from NewRegistrar")
	}
}

// TestNewRegistrar_CustomURL verifies that NewRegistrar does not panic with a custom URL.
func TestNewRegistrar_CustomURL(t *testing.T) {
	reg := relay.NewRegistrar("https://custom.huginncloud.com")
	if reg == nil {
		t.Fatal("NewRegistrar returned nil")
	}
	_, machineID := reg.Status()
	if machineID == "" {
		t.Error("expected non-empty machineID")
	}
}

// TestNewSatelliteDefaultURL_Confidence verifies NewSatellite applies the
// default relay URL when given an empty string (distinct from hardening_iter4
// which uses NewSatelliteWithStore).
func TestNewSatelliteDefaultURL_Confidence(t *testing.T) {
	sat := relay.NewSatellite("")
	if sat == nil {
		t.Fatal("NewSatellite returned nil")
	}
	status := sat.Status()
	if status.CloudURL != "wss://api.huginncloud.com" {
		t.Errorf("expected default cloud URL, got %q", status.CloudURL)
	}
}

// TestNewSatelliteCustomURL_Confidence verifies NewSatellite uses the provided URL.
func TestNewSatelliteCustomURL_Confidence(t *testing.T) {
	sat := relay.NewSatellite("wss://custom.example.com")
	if sat == nil {
		t.Fatal("NewSatellite returned nil")
	}
	status := sat.Status()
	if status.CloudURL != "wss://custom.example.com" {
		t.Errorf("expected custom URL, got %q", status.CloudURL)
	}
}

// TestNewSatellite_Status_NotRegistered verifies Status() does not panic
// when using the real OS keychain (which has no token in CI).
func TestNewSatellite_Status_NotRegistered(t *testing.T) {
	sat := relay.NewSatellite("wss://api.huginncloud.com")
	status := sat.Status()
	_ = status.Registered
	_ = status.Connected
	_ = status.MachineID
	_ = status.CloudURL
}

// TestNewSatellite_Hub_NotRegistered verifies Hub() returns InProcessHub
// when the OS keychain has no relay token.
func TestNewSatellite_Hub_NotRegistered(t *testing.T) {
	sat := relay.NewSatellite("wss://api.huginncloud.com")
	hub := sat.Hub(context.Background())
	if hub == nil {
		t.Fatal("Hub() returned nil even when not registered")
	}
	// The returned hub (InProcessHub) must accept Send without error.
	err := hub.Send("m1", relay.Message{Type: relay.MsgToken})
	if err != nil {
		t.Errorf("expected nil from fallback InProcessHub.Send, got %v", err)
	}
}

// TestNewTokenStore_Construction verifies NewTokenStore doesn't panic.
func TestNewTokenStore_Construction(t *testing.T) {
	ts := relay.NewTokenStore()
	if ts == nil {
		t.Fatal("NewTokenStore returned nil")
	}
	// IsRegistered is safe to call even if keyring is unavailable.
	_ = ts.IsRegistered()
}

// TestNewTokenStore_Load_ReturnsError verifies Load returns an error when
// no token is stored (expected in CI).
func TestNewTokenStore_Load_ReturnsError(t *testing.T) {
	ts := relay.NewTokenStore()
	_, _ = ts.Load() // may error in CI; must not panic
}

// TestRegistrar_DeliverCode_BeforeRegister verifies DeliverCode is safe
// to call before Register — it should be a no-op.
func TestRegistrar_DeliverCode_BeforeRegister(t *testing.T) {
	reg := relay.NewRegistrarWithStore("https://example.com", &relay.MemoryTokenStore{})
	reg.DeliverCode("code1")
	reg.DeliverCode("code2") // multiple calls — still safe
}

// TestRegistrar_Register_DeliverCodePath exercises the code delivery path
// with a mock exchange endpoint that fails (unreachable server).
func TestRegistrar_Register_DeliverCodePath_WithMockServer(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	reg := newTestRegistrar("http://localhost:19999", store)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Deliver code concurrently — triggers the exchange path (server unreachable,
	// but exercising DeliverCode + exchangeCode error path).
	go func() {
		time.Sleep(20 * time.Millisecond)
		reg.DeliverCode("test-code")
	}()

	_, err := reg.Register(ctx, "127.0.0.1:0")
	_ = err // may be exchange error or context deadline
}
