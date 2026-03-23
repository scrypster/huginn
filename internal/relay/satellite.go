package relay

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/gorilla/websocket"
)

// Satellite manages the connection between the local huginn instance and
// HuginnCloud. It is a no-op when the machine is not registered.
type Satellite struct {
	tokens       TokenStorer
	machineID    string
	mu           sync.Mutex
	hub          Hub   // nil when disconnected; *WebSocketHub when connected
	baseURL      string
	onMessage    func(context.Context, Message)
	hubOnce      sync.Once // guards singleton Hub() initialization
	lazyHub      Hub       // cached hub from lazy initialization
}

// NewSatellite creates a Satellite using the OS-keyring token store.
// baseURL defaults to "wss://api.huginncloud.com" when empty.
func NewSatellite(baseURL string) *Satellite {
	if baseURL == "" {
		baseURL = "wss://api.huginncloud.com"
	}
	machineID := GetMachineID()
	return &Satellite{
		tokens:    NewTokenStore(),
		machineID: machineID,
		baseURL:   baseURL,
	}
}

// NewSatelliteWithStore creates a Satellite with a custom TokenStorer (for testing).
func NewSatelliteWithStore(baseURL string, store TokenStorer) *Satellite {
	if baseURL == "" {
		baseURL = "wss://api.huginncloud.com"
	}
	machineID := GetMachineID()
	return &Satellite{
		tokens:    store,
		machineID: machineID,
		baseURL:   baseURL,
	}
}

// SetMachineID sets the machine ID for this satellite.
// This overrides the detected machine ID and should be called before Connect().
func (s *Satellite) SetMachineID(machineID string) {
	s.mu.Lock()
	s.machineID = machineID
	s.mu.Unlock()
}

// SetOnMessage registers a callback that fires for every message received from
// HuginnCloud. Must be called before Connect() so the callback is wired before
// the read pump starts.
func (s *Satellite) SetOnMessage(fn func(context.Context, Message)) {
	s.mu.Lock()
	s.onMessage = fn
	s.mu.Unlock()
}

// wsConfig returns the WebSocket connection configuration for this satellite.
// TokenProvider is used so that every reconnect attempt loads a fresh token
// from the keystore rather than reusing a token captured at startup.
func (s *Satellite) wsConfig() WebSocketConfig {
	return WebSocketConfig{
		URL:           s.baseURL + "/ws/satellite",
		TokenProvider: s.tokens.Load,
		MachineID:     s.machineID,
		Version:       satelliteVersion(),
	}
}

// Hub returns the active relay Hub.
// If the machine is not registered, returns InProcessHub (no-op, current behaviour).
// If registered but the WebSocket dial fails, logs a warning and falls back to InProcessHub.
// The hub is lazily initialized once and cached using sync.Once to prevent races.
func (s *Satellite) Hub(ctx context.Context) Hub {
	s.hubOnce.Do(func() {
		if _, err := s.tokens.Load(); err != nil {
			// Not registered — use no-op hub (existing behaviour).
			s.lazyHub = &InProcessHub{}
			return
		}

		s.mu.Lock()
		machineID := s.machineID
		s.mu.Unlock()

		wsHub := NewWebSocketHub(s.wsConfig())

		s.mu.Lock()
		cb := s.onMessage
		s.mu.Unlock()
		if cb != nil {
			wsHub.SetOnMessage(cb)
		}

		if err := wsHub.Connect(ctx); err != nil {
			// A bad handshake means the server rejected the token (401). Clear it
			// so that the next status poll reports registered=false.
			if errors.Is(err, websocket.ErrBadHandshake) {
				slog.Warn("relay: cleared stale token (auth rejected by HuginnCloud)")
				_ = s.tokens.Clear()
			}
			slog.Warn("relay: could not connect to HuginnCloud", "err", err)
			s.lazyHub = &InProcessHub{}
			return
		}

		slog.Info("relay: connected to HuginnCloud", "machine_id", machineID)
		s.mu.Lock()
		s.hub = wsHub
		s.mu.Unlock()
		s.lazyHub = wsHub
	})

	return s.lazyHub
}

// NewHubForConnect returns a new WebSocketHub configured for this satellite
// but not yet connected. Returns nil if the satellite is not registered.
//
// The caller should:
//  1. Build the dispatcher using the returned hub as cfg.Hub.
//  2. Call hub.SetOnMessage(dispatcher) to wire the callback.
//  3. Call ConnectHub(ctx, hub) to dial and start the read pump.
//
// This ordering ensures the dispatcher callback is registered before
// readPump starts, preventing inbound messages from being dropped in the
// window between sendHello and SetOnMessage.
func (s *Satellite) NewHubForConnect() *WebSocketHub {
	if _, err := s.tokens.Load(); err != nil {
		return nil // not registered
	}
	return NewWebSocketHub(s.wsConfig())
}

// ConnectHub dials hub and registers it as the active satellite hub.
// hub must have been obtained from NewHubForConnect; passing nil is a no-op
// error. Replaces any existing active hub.
func (s *Satellite) ConnectHub(ctx context.Context, hub *WebSocketHub) error {
	if hub == nil {
		return fmt.Errorf("relay: ConnectHub: hub is nil (satellite not registered?)")
	}
	if err := hub.Connect(ctx); err != nil {
		if errors.Is(err, websocket.ErrBadHandshake) {
			slog.Warn("relay: cleared stale token (auth rejected by HuginnCloud)")
			_ = s.tokens.Clear()
		}
		return fmt.Errorf("relay: dial HuginnCloud: %w", err)
	}
	s.mu.Lock()
	old := s.hub
	s.hub = hub
	machineID := s.machineID
	s.mu.Unlock()
	if old != nil {
		old.Close("")
	}
	slog.Info("relay: connected to HuginnCloud", "machine_id", machineID)
	return nil
}

// Connect dials HuginnCloud and stores the active hub.
// Returns an error if the machine is not registered or the dial fails.
// If already connected, the existing connection is closed first.
func (s *Satellite) Connect(ctx context.Context) error {
	if _, err := s.tokens.Load(); err != nil {
		return fmt.Errorf("relay: not registered with HuginnCloud")
	}

	s.mu.Lock()
	machineID := s.machineID
	s.mu.Unlock()

	wsHub := NewWebSocketHub(s.wsConfig())

	s.mu.Lock()
	cb := s.onMessage
	s.mu.Unlock()
	if cb != nil {
		wsHub.SetOnMessage(cb)
	}

	if err := wsHub.Connect(ctx); err != nil {
		// A bad handshake is caused by the server rejecting the connection
		// at the HTTP upgrade level — the most common cause is a 401 (stale
		// or revoked token). Clear the token so the next status poll reports
		// registered=false and the user can re-register cleanly.
		if errors.Is(err, websocket.ErrBadHandshake) {
			slog.Warn("relay: cleared stale token (auth rejected by HuginnCloud)")
			_ = s.tokens.Clear()
		}
		return fmt.Errorf("relay: dial HuginnCloud: %w", err)
	}

	s.mu.Lock()
	old := s.hub
	s.hub = wsHub
	s.mu.Unlock()

	// Close the previous hub after releasing the lock.
	if old != nil {
		old.Close("")
	}

	slog.Info("relay: connected to HuginnCloud", "machine_id", machineID)
	return nil
}

// Disconnect closes the active hub connection and clears the stored hub.
// Safe to call when not connected.
func (s *Satellite) Disconnect() {
	s.mu.Lock()
	hub := s.hub
	s.hub = nil
	s.mu.Unlock()

	if hub != nil {
		hub.Close("")
		slog.Info("relay: disconnected from HuginnCloud")
	}
}

// ActiveHub returns the currently active Hub (may be a no-op InProcessHub).
func (s *Satellite) ActiveHub() Hub {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hub == nil {
		return &InProcessHub{}
	}
	return s.hub
}

// Reconnect closes the existing connection and immediately re-dials.
// Used by the wake notifier to bypass reconnect backoff after OS sleep.
func (s *Satellite) Reconnect(ctx context.Context) {
	s.mu.Lock()
	hub := s.hub
	s.mu.Unlock()
	wsHub, ok := hub.(*WebSocketHub)
	if !ok || wsHub == nil {
		return
	}
	wsHub.ResetBackoff()
	wsHub.mu.Lock()
	conn := wsHub.conn
	wsHub.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
}

// CloudStatus contains the current registration and connection state.
type CloudStatus struct {
	Registered bool   `json:"registered"`
	MachineID  string `json:"machine_id"`
	Connected  bool   `json:"connected"`
	CloudURL   string `json:"cloud_url"`
}

// Status returns the current CloudStatus for this satellite.
func (s *Satellite) Status() CloudStatus {
	_, err := s.tokens.Load()
	s.mu.Lock()
	hub := s.hub
	machineID := s.machineID
	s.mu.Unlock()
	_, isWS := hub.(*WebSocketHub)
	return CloudStatus{
		Registered: err == nil,
		MachineID:  machineID,
		Connected:  isWS && err == nil,
		CloudURL:   s.baseURL,
	}
}

// CircuitBreakerState returns the active hub's circuit breaker state as a
// human-readable string ("closed", "open", "half_open"). Returns "closed" when
// the hub is nil or is not a WebSocketHub.
func (s *Satellite) CircuitBreakerState() string {
	s.mu.Lock()
	hub := s.hub
	s.mu.Unlock()
	if ws, ok := hub.(*WebSocketHub); ok && ws != nil {
		return ws.cb.State()
	}
	return "closed"
}

// satelliteVersion returns the huginn version string for use in satellite_hello.
// It reads HUGINN_VERSION from the environment; falls back to "dev".
func satelliteVersion() string {
	if v := os.Getenv("HUGINN_VERSION"); v != "" {
		return v
	}
	return "dev"
}
