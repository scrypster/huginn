package relay_test

// coverage_boost_test.go — tests to push relay package to 80%+.
// Targets: identity.go, registration.go, satellite.go, websocket.go, token.go

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/relay"
)

// ─── loadOrCreateMachineSuffix — loads existing valid suffix ─────────────────

func TestLoadOrCreateMachineSuffix_LoadsExisting(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Write a valid 8-char hex suffix to the machine_id file
	huginnDir := filepath.Join(tmpHome, ".huginn")
	if err := os.MkdirAll(huginnDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	expected := "deadbeef"
	if err := os.WriteFile(filepath.Join(huginnDir, "machine_id"), []byte(expected), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// GetMachineID now returns the suffix directly (no hostname prefix)
	id := relay.GetMachineID()
	if id != expected {
		t.Errorf("expected machine ID %q, got %q", expected, id)
	}
}

// ─── loadOrCreateMachineSuffix — creates new when file missing ───────────────

func TestLoadOrCreateMachineSuffix_CreatesNew(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// No machine_id file yet — should create one
	id := relay.GetMachineID()
	if id == "" {
		t.Error("expected non-empty machine ID")
	}

	// File should now exist
	path := filepath.Join(tmpHome, ".huginn", "machine_id")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected machine_id file to be created: %v", err)
	}
	suffix := string(data)
	if len(suffix) != 8 {
		t.Errorf("expected 8-char suffix, got %q (len=%d)", suffix, len(suffix))
	}
}

// ─── loadOrCreateMachineSuffix — invalid existing suffix → regenerate ────────

func TestLoadOrCreateMachineSuffix_InvalidSuffixRegenerate(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	huginnDir := filepath.Join(tmpHome, ".huginn")
	if err := os.MkdirAll(huginnDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Write an invalid suffix (wrong length and non-hex)
	if err := os.WriteFile(filepath.Join(huginnDir, "machine_id"), []byte("not-hex!"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	id := relay.GetMachineID()
	if id == "" {
		t.Error("expected non-empty machine ID even with invalid suffix file")
	}
}

// ─── isHex — non-hex characters ──────────────────────────────────────────────
// (tested indirectly via loadOrCreateMachineSuffix — the invalid case above)

// ─── sanitizeHostname — empty hostname ────────────────────────────────────────

func TestGetMachineID_EmptyHostname(t *testing.T) {
	// Override HOME to avoid using real machine_id
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// We can't easily override the hostname, but we can verify format is valid
	id := relay.GetMachineID()
	if id == "" {
		t.Error("expected non-empty machine ID")
	}
	if len(id) < 8 {
		t.Errorf("machine ID too short: %q", id)
	}
}

// ─── machineSuffixPath — UserHomeDir fails ────────────────────────────────────

func TestMachineSuffixPath_HomeNotSet(t *testing.T) {
	// $HOME unset causes os.UserHomeDir to fail on some platforms.
	// GetMachineID should still return a non-empty string via fallback.
	origHome := os.Getenv("HOME")
	os.Unsetenv("HOME")
	defer os.Setenv("HOME", origHome)

	id := relay.GetMachineID()
	if id == "" {
		t.Error("expected non-empty machine ID even with HOME unset")
	}
}

// ─── identity.Save and LoadIdentity ──────────────────────────────────────────

func TestIdentity_Save_CreatesDirectory(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	id := &relay.Identity{
		AgentID:  "test-agent",
		Endpoint: "https://example.com",
	}
	if err := id.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Directory should have been created
	dir := filepath.Join(tmpHome, ".huginn")
	if fi, err := os.Stat(dir); err != nil {
		t.Fatalf("expected .huginn dir to exist: %v", err)
	} else if !fi.IsDir() {
		t.Error("expected .huginn to be a directory")
	}
}

func TestLoadIdentity_InvalidJSON(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	dir := filepath.Join(tmpHome, ".huginn")
	_ = os.MkdirAll(dir, 0o750)
	_ = os.WriteFile(filepath.Join(dir, "relay.json"), []byte("{invalid"), 0o600)

	_, err := relay.LoadIdentity()
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ─── Registration — context cancelled ────────────────────────────────────────

func TestRegistrar_Register_ContextCancelled(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	reg := newTestRegistrar("http://localhost:9999", store)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, regErr := reg.Register(ctx, "127.0.0.1:0")
		errCh <- regErr
	}()

	// Cancel before any code is delivered
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("expected context cancellation error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Register did not respect context cancellation")
	}
}

// ─── Registration — timeout path (short timeout) ─────────────────────────────

// Note: We cannot practically test the full 5-minute timeout, so we exercise
// the context deadline path instead (which covers the same select branch).

func TestRegistrar_Register_DeadlineExceeded(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	reg := newTestRegistrar("http://localhost:9999", store)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := reg.Register(ctx, "127.0.0.1:0")
	if err == nil {
		t.Error("expected error from expired context")
	}
}

// ─── satelliteVersion ─────────────────────────────────────────────────────────

func TestSatelliteVersion_ViaEnvVar(t *testing.T) {
	// Set HUGINN_VERSION env var and call Hub — exercises the non-"dev" path indirectly
	t.Setenv("HUGINN_VERSION", "1.2.3")
	store := &relay.MemoryTokenStore{}
	_ = store.Save("some-token")
	sat := relay.NewSatelliteWithStore("wss://127.0.0.1:1", store) // unreachable
	// Hub should fail to connect and fall back to InProcessHub
	hub := sat.Hub(context.Background())
	if hub == nil {
		t.Error("expected non-nil hub")
	}
	// Send should succeed (InProcessHub is a no-op)
	if err := hub.Send("machine-1", relay.Message{Type: relay.MsgToken}); err != nil {
		t.Errorf("expected nil from fallback hub, got %v", err)
	}
}

func TestSatelliteVersion_Dev(t *testing.T) {
	// Ensure HUGINN_VERSION is unset — exercises "dev" fallback
	os.Unsetenv("HUGINN_VERSION")
	store := &relay.MemoryTokenStore{}
	_ = store.Save("some-token")
	sat := relay.NewSatelliteWithStore("wss://127.0.0.1:1", store)
	hub := sat.Hub(context.Background())
	if hub == nil {
		t.Error("expected non-nil hub")
	}
}

// ─── InProcessHub.Close ───────────────────────────────────────────────────────

func TestInProcessHub_Close_NoPanic(t *testing.T) {
	h := relay.NewInProcessHub()
	h.Close("machine-1") // should not panic
	h.Close("machine-1") // safe to call twice
}

// ─── Satellite.Status — with hub connected ────────────────────────────────────

func TestSatelliteStatus_WithHub(t *testing.T) {
	// Hub field is non-nil only after a successful Connect
	// We test via NewSatelliteWithStore where the token is saved and
	// we verify Connected=false (hub is nil until connect)
	store := &relay.MemoryTokenStore{}
	_ = store.Save("some-token")
	sat := relay.NewSatelliteWithStore("wss://127.0.0.1:1", store)
	status := sat.Status()
	if !status.Registered {
		t.Error("expected Registered=true when token is saved")
	}
	if status.Connected {
		t.Error("expected Connected=false before Hub() is called")
	}
}

// ─── WebSocketHub — Send with nil connection ─────────────────────────────────

func TestWebSocketHub_Send_NilConn(t *testing.T) {
	hub := relay.NewWebSocketHub()
	err := hub.Send("m-1", relay.Message{Type: relay.MsgToken})
	if err != relay.ErrNotActivated {
		t.Errorf("expected ErrNotActivated, got %v", err)
	}
}

// ─── MemoryTokenStore — full lifecycle ────────────────────────────────────────

func TestMemoryTokenStore_FullLifecycle(t *testing.T) {
	store := &relay.MemoryTokenStore{}

	if store.IsRegistered() {
		t.Error("expected not registered on empty store")
	}

	if err := store.Save("my-token"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if !store.IsRegistered() {
		t.Error("expected registered after Save")
	}

	tok, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if tok != "my-token" {
		t.Errorf("Load: got %q, want %q", tok, "my-token")
	}

	if err := store.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	if store.IsRegistered() {
		t.Error("expected not registered after Clear")
	}

	_, err = store.Load()
	if err == nil {
		t.Error("expected error from Load after Clear")
	}
}

// ─── ErrNotActivated.Error — 0% — exercise the error string ──────────────────

func TestErrNotActivated_ErrorString(t *testing.T) {
	hub := relay.NewWebSocketHub()
	err := hub.Send("m1", relay.Message{Type: relay.MsgToken})
	if err == nil {
		t.Fatal("expected ErrNotActivated")
	}
	// Calling Error() on the returned error covers errNotActivated.Error
	msg := err.Error()
	if !strings.Contains(msg, "relay:") {
		t.Errorf("unexpected error message: %q", msg)
	}
	// Verify it is the sentinel
	if !errors.Is(err, relay.ErrNotActivated) {
		t.Errorf("expected errors.Is(err, relay.ErrNotActivated), got %T", err)
	}
}

// ─── GetMachineID — ID is exactly 8 hex chars, no hostname prefix ────────────

func TestGetMachineID_LongHostname(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Machine ID should now be a pure 8-char hex string, hostname-independent.
	id := relay.GetMachineID()
	if id == "" {
		t.Error("expected non-empty machine ID")
	}
	if len(id) != 8 {
		t.Errorf("expected 8-char hex machine ID, got %q (len=%d)", id, len(id))
	}
}

// ─── LoadIdentity — file exists but not JSON (read succeeds, unmarshal fails) ─

func TestLoadIdentity_FileExistsButBadJSON(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	dir := filepath.Join(tmpHome, ".huginn")
	_ = os.MkdirAll(dir, 0o750)
	// Write a file that exists but is not valid JSON
	_ = os.WriteFile(filepath.Join(dir, "relay.json"), []byte("not-json-at-all"), 0o600)

	_, err := relay.LoadIdentity()
	if err == nil {
		t.Error("expected error for malformed relay.json")
	}
}

// ─── LoadIdentity — file does not exist → ErrNotRegistered ───────────────────

func TestLoadIdentity_NotFound(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	// No relay.json written — should get ErrNotRegistered
	_, err := relay.LoadIdentity()
	if !errors.Is(err, relay.ErrNotRegistered) {
		t.Errorf("expected ErrNotRegistered, got %v", err)
	}
}

// ─── Identity.Save — round-trip: save then load ───────────────────────────────

func TestIdentity_SaveLoad_RoundTrip(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	orig := &relay.Identity{
		AgentID:  "agent-xyz",
		Endpoint: "https://relay.example.com",
		APIKey:   "secret-key",
	}
	if err := orig.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := relay.LoadIdentity()
	if err != nil {
		t.Fatalf("LoadIdentity: %v", err)
	}
	if loaded.AgentID != orig.AgentID {
		t.Errorf("AgentID: got %q, want %q", loaded.AgentID, orig.AgentID)
	}
	if loaded.Endpoint != orig.Endpoint {
		t.Errorf("Endpoint: got %q, want %q", loaded.Endpoint, orig.Endpoint)
	}
}

// ─── WebSocketHub — Connect then Send and Close ───────────────────────────────

func TestWebSocketHub_ConnectAndSend(t *testing.T) {
	// Hub with bad URL — Connect should fail; Send should return ErrNotActivated
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{
		URL:       "wss://127.0.0.1:1/satellite",
		Token:     "test-token",
		MachineID: "test-machine",
		Version:   "test",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Connect must fail (unreachable server)
	err := hub.Connect(ctx)
	if err == nil {
		t.Error("expected Connect to fail on unreachable server")
		hub.Close("test-machine")
		return
	}

	// Send must return ErrNotActivated since conn is nil
	sendErr := hub.Send("test-machine", relay.Message{Type: relay.MsgToken})
	if sendErr != relay.ErrNotActivated {
		t.Errorf("expected ErrNotActivated after failed Connect, got %v", sendErr)
	}
}

// ─── exchangeCode — store save error ─────────────────────────────────────────

// failingTokenStore always returns an error on Save.
type failingTokenStore struct{}

func (f *failingTokenStore) Save(_ string) error  { return errors.New("keyring: access denied") }
func (f *failingTokenStore) Load() (string, error) { return "", errors.New("no token") }
func (f *failingTokenStore) Clear() error          { return nil }
func (f *failingTokenStore) IsRegistered() bool    { return false }

func TestRegistrar_StoreSaveError_BrowserFlow(t *testing.T) {
	reg := relay.NewRegistrarWithStore("http://localhost:0", &failingTokenStore{})
	reg.OpenBrowserFn = func(rawURL string) error {
		u, _ := url.Parse(rawURL)
		cbURL := u.Query().Get("cb")
		go func() {
			time.Sleep(50 * time.Millisecond)
			resp, _ := http.Get(cbURL + "?api_key=k&machine_id=m")
			if resp != nil {
				resp.Body.Close()
			}
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := reg.Register(ctx, "test")
	if err == nil {
		t.Error("expected save error from Register, got nil")
	}
}

// ─── Registrar.Register — open browser fn called ─────────────────────────────

func TestRegistrar_Register_BrowserFnCalled(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	reg := newTestRegistrar("http://localhost:9999", store)

	var browserURL string
	reg.OpenBrowserFn = func(rawURL string) error {
		browserURL = rawURL
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Register will open the browser (our fn) and then block until timeout
	_, _ = reg.Register(ctx, "127.0.0.1:0")

	if browserURL == "" {
		t.Error("expected OpenBrowserFn to be called with a URL")
	}
	if !strings.Contains(browserURL, "machine_id") {
		t.Errorf("browser URL missing machine_id: %q", browserURL)
	}
}

// ─── Satellite.Hub — not registered falls back to InProcessHub ───────────────

func TestSatellite_Hub_NotRegistered(t *testing.T) {
	store := &relay.MemoryTokenStore{} // empty — no token
	sat := relay.NewSatelliteWithStore("wss://127.0.0.1:1", store)

	hub := sat.Hub(context.Background())
	if hub == nil {
		t.Error("expected non-nil hub (InProcessHub fallback)")
	}
	// InProcessHub.Send is a no-op — should return nil
	err := hub.Send("m1", relay.Message{Type: relay.MsgToken})
	if err != nil {
		t.Errorf("expected nil from InProcessHub.Send, got %v", err)
	}
}

// ─── Satellite.Hub — registered, connect succeeds → hub stored ───────────────

func TestSatellite_Hub_ConnectSucceeds(t *testing.T) {
	// Stand up a real WebSocket server so Hub.Connect succeeds.
	// We reuse the `upgrader` var and `wsURL` helper defined in ws_test.go
	// (same package relay_test).
	srv := newBoostWSSrv(t)
	defer srv.Close()

	wsBaseURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	store := &relay.MemoryTokenStore{}
	_ = store.Save("test-token")
	sat := relay.NewSatelliteWithStore(wsBaseURL, store)

	hub := sat.Hub(context.Background())
	if hub == nil {
		t.Fatal("expected non-nil hub")
	}
	// After Hub() we expect Connected=true
	status := sat.Status()
	if !status.Connected {
		t.Error("expected Connected=true after successful Hub()")
	}
}

// newBoostWSSrv starts a minimal WebSocket server that accepts connections
// and drains messages until disconnected.
func newBoostWSSrv(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// reuse upgrader from ws_test.go (same relay_test package)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
}
