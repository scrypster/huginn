package tray

import (
	"testing"
)

// resetGlobals resets the package-level globals to a known state before each test.
// This is necessary because tray.go uses package-level variables.
func resetGlobals() {
	globalCfg = Config{}
	serverAddr = ""
	serverOwned = false
	satelliteOnline = false
	hidingOnly = false
}

// TestOnExit_HidingOnly verifies that when hidingOnly=true, onExit does NOT
// call OnStop or OnSatelliteDisconnect — the server and satellite keep running.
func TestOnExit_HidingOnly(t *testing.T) {
	resetGlobals()

	stopCalled := false
	satDisconnectCalled := false

	globalCfg = Config{
		OnStop: func() {
			stopCalled = true
		},
		OnSatelliteDisconnect: func() {
			satDisconnectCalled = true
		},
	}
	serverOwned = true
	satelliteOnline = true
	hidingOnly = true

	onExit()

	if stopCalled {
		t.Error("onExit: OnStop must NOT be called when hidingOnly=true")
	}
	if satDisconnectCalled {
		t.Error("onExit: OnSatelliteDisconnect must NOT be called when hidingOnly=true")
	}
}

// TestOnExit_StopsServerWhenOwned verifies that onExit calls OnStop when
// serverOwned=true and hidingOnly=false.
func TestOnExit_StopsServerWhenOwned(t *testing.T) {
	resetGlobals()

	stopCalled := false
	globalCfg = Config{
		OnStop: func() {
			stopCalled = true
		},
	}
	serverOwned = true
	hidingOnly = false

	onExit()

	if !stopCalled {
		t.Error("onExit: OnStop must be called when serverOwned=true and hidingOnly=false")
	}
}

// TestOnExit_NoStopWhenNotOwned verifies that onExit does NOT call OnStop
// when serverOwned=false (server was not started by this tray instance).
func TestOnExit_NoStopWhenNotOwned(t *testing.T) {
	resetGlobals()

	stopCalled := false
	globalCfg = Config{
		OnStop: func() {
			stopCalled = true
		},
	}
	serverOwned = false
	hidingOnly = false

	onExit()

	if stopCalled {
		t.Error("onExit: OnStop must NOT be called when serverOwned=false")
	}
}

// TestOnExit_DisconnectsSatelliteWhenOnline verifies that onExit calls
// OnSatelliteDisconnect when satelliteOnline=true.
func TestOnExit_DisconnectsSatelliteWhenOnline(t *testing.T) {
	resetGlobals()

	satDisconnectCalled := false
	globalCfg = Config{
		OnSatelliteDisconnect: func() {
			satDisconnectCalled = true
		},
	}
	satelliteOnline = true
	hidingOnly = false

	onExit()

	if !satDisconnectCalled {
		t.Error("onExit: OnSatelliteDisconnect must be called when satelliteOnline=true")
	}
}

// TestOnExit_NoDisconnectWhenSatelliteOffline verifies that onExit does NOT
// call OnSatelliteDisconnect when satelliteOnline=false.
func TestOnExit_NoDisconnectWhenSatelliteOffline(t *testing.T) {
	resetGlobals()

	satDisconnectCalled := false
	globalCfg = Config{
		OnSatelliteDisconnect: func() {
			satDisconnectCalled = true
		},
	}
	satelliteOnline = false
	hidingOnly = false

	onExit()

	if satDisconnectCalled {
		t.Error("onExit: OnSatelliteDisconnect must NOT be called when satelliteOnline=false")
	}
}

// TestOnExit_BothServerAndSatellite verifies that when both serverOwned and
// satelliteOnline are true, both callbacks are invoked.
func TestOnExit_BothServerAndSatellite(t *testing.T) {
	resetGlobals()

	stopCalled := false
	satDisconnectCalled := false
	globalCfg = Config{
		OnStop: func() {
			stopCalled = true
		},
		OnSatelliteDisconnect: func() {
			satDisconnectCalled = true
		},
	}
	serverOwned = true
	satelliteOnline = true
	hidingOnly = false

	onExit()

	if !stopCalled {
		t.Error("onExit: OnStop must be called")
	}
	if !satDisconnectCalled {
		t.Error("onExit: OnSatelliteDisconnect must be called")
	}
}

// TestOnExit_NilCallbacks verifies that onExit does not panic when the
// callbacks are nil (zero-value Config).
func TestOnExit_NilCallbacks(t *testing.T) {
	resetGlobals()

	globalCfg = Config{} // OnStop and OnSatelliteDisconnect are nil
	serverOwned = true
	satelliteOnline = true
	hidingOnly = false

	// Must not panic.
	onExit()
}

// TestOnExit_SatelliteDisconnectOrder verifies that satellite is disconnected
// before the server is stopped (important for clean teardown ordering).
func TestOnExit_SatelliteDisconnectOrder(t *testing.T) {
	resetGlobals()

	var callOrder []string
	globalCfg = Config{
		OnStop: func() {
			callOrder = append(callOrder, "stop")
		},
		OnSatelliteDisconnect: func() {
			callOrder = append(callOrder, "disconnect")
		},
	}
	serverOwned = true
	satelliteOnline = true
	hidingOnly = false

	onExit()

	if len(callOrder) != 2 {
		t.Fatalf("expected 2 calls, got %d: %v", len(callOrder), callOrder)
	}
	if callOrder[0] != "disconnect" {
		t.Errorf("expected satellite disconnect first, got %q first", callOrder[0])
	}
	if callOrder[1] != "stop" {
		t.Errorf("expected server stop second, got %q second", callOrder[1])
	}
}

// TestConfig_ZeroValue ensures the zero-value Config is safe (no panics on
// field access).
func TestConfig_ZeroValue(t *testing.T) {
	var cfg Config
	if cfg.Port != 0 {
		t.Error("zero-value Config.Port should be 0")
	}
	if cfg.HuginnBinPath != "" {
		t.Error("zero-value Config.HuginnBinPath should be empty")
	}
	if cfg.OnStart != nil {
		t.Error("zero-value Config.OnStart should be nil")
	}
	if cfg.OnStop != nil {
		t.Error("zero-value Config.OnStop should be nil")
	}
	if cfg.OnSatelliteConnect != nil {
		t.Error("zero-value Config.OnSatelliteConnect should be nil")
	}
	if cfg.OnSatelliteDisconnect != nil {
		t.Error("zero-value Config.OnSatelliteDisconnect should be nil")
	}
	if cfg.OnSatelliteStatus != nil {
		t.Error("zero-value Config.OnSatelliteStatus should be nil")
	}
}

// TestConfig_AllCallbacksSet verifies Config properly stores all callbacks.
func TestConfig_AllCallbacksSet(t *testing.T) {
	cfg := Config{
		Port:          8421,
		HuginnBinPath: "/usr/bin/huginn",
		OnStart: func() (string, error) {
			return "127.0.0.1:8421", nil
		},
		OnStop:                func() {},
		OnSatelliteConnect:    func() error { return nil },
		OnSatelliteDisconnect: func() {},
		OnSatelliteStatus:     func() bool { return true },
	}

	if cfg.Port != 8421 {
		t.Errorf("Config.Port: got %d, want 8421", cfg.Port)
	}
	if cfg.HuginnBinPath != "/usr/bin/huginn" {
		t.Errorf("Config.HuginnBinPath: got %q", cfg.HuginnBinPath)
	}
	if cfg.OnStart == nil {
		t.Error("Config.OnStart should not be nil")
	}
	addr, err := cfg.OnStart()
	if err != nil {
		t.Errorf("Config.OnStart: unexpected error: %v", err)
	}
	if addr != "127.0.0.1:8421" {
		t.Errorf("Config.OnStart: got addr %q, want 127.0.0.1:8421", addr)
	}
	if cfg.OnSatelliteStatus == nil || !cfg.OnSatelliteStatus() {
		t.Error("Config.OnSatelliteStatus: expected true")
	}
}

// TestGlobalState_InitialValues confirms that after resetGlobals() all state
// variables are at their correct zero values — this catches any future globals
// that are added without being reset.
func TestGlobalState_InitialValues(t *testing.T) {
	resetGlobals()

	if serverAddr != "" {
		t.Errorf("serverAddr: expected empty, got %q", serverAddr)
	}
	if serverOwned {
		t.Error("serverOwned: expected false initially")
	}
	if satelliteOnline {
		t.Error("satelliteOnline: expected false initially")
	}
	if hidingOnly {
		t.Error("hidingOnly: expected false initially")
	}
}

// TestOnExit_HidingOnly_GlobalReset verifies hidingOnly flag is the sole gate
// for the early return — even with server and satellite active, nothing fires.
func TestOnExit_HidingOnly_GlobalReset(t *testing.T) {
	resetGlobals()

	calls := 0
	globalCfg = Config{
		OnStop:                func() { calls++ },
		OnSatelliteDisconnect: func() { calls++ },
	}
	serverOwned = true
	satelliteOnline = true
	hidingOnly = true

	onExit()

	if calls != 0 {
		t.Errorf("onExit with hidingOnly=true: expected 0 calls, got %d", calls)
	}
}
