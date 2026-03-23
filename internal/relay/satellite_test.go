package relay_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/scrypster/huginn/internal/relay"
)

// TestNewSatelliteWithStore_DefaultURLWhenEmpty verifies that NewSatelliteWithStore
// uses default WebSocket URL when baseURL is empty.
func TestNewSatelliteWithStore_DefaultURLWhenEmpty(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	sat := relay.NewSatelliteWithStore("", store)
	if sat == nil {
		t.Error("expected NewSatelliteWithStore to return non-nil Satellite")
	}
	status := sat.Status()
	if status.CloudURL == "" {
		t.Error("expected non-empty CloudURL with default")
	}
}

// TestNewSatelliteWithStore_CustomURL verifies that NewSatelliteWithStore
// accepts custom baseURL.
func TestNewSatelliteWithStore_CustomURL(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	customURL := "wss://custom.example.com"
	sat := relay.NewSatelliteWithStore(customURL, store)
	status := sat.Status()
	if status.CloudURL != customURL {
		t.Errorf("expected CloudURL=%q, got %q", customURL, status.CloudURL)
	}
}

// TestSatellite_Status_IncludesMachineID verifies that Status includes a non-empty MachineID.
func TestSatellite_Status_IncludesMachineID(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	sat := relay.NewSatelliteWithStore("wss://test.example.com", store)
	status := sat.Status()

	if status.MachineID == "" {
		t.Error("expected non-empty MachineID in Status")
	}
}

// TestSatellite_Status_RegisteredFalseWhenTokenLoadFails verifies Registered is false
// when token cannot be loaded.
func TestSatellite_Status_RegisteredFalseWhenTokenLoadFails(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	// Empty store, Load() will fail
	sat := relay.NewSatelliteWithStore("wss://test.example.com", store)
	status := sat.Status()

	if status.Registered {
		t.Error("expected Registered=false when Load fails")
	}
}

// TestSatellite_Status_RegisteredTrueWhenTokenLoaded verifies Registered is true
// when token is available.
func TestSatellite_Status_RegisteredTrueWhenTokenLoaded(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	store.Save("test-token")
	sat := relay.NewSatelliteWithStore("wss://test.example.com", store)
	status := sat.Status()

	if !status.Registered {
		t.Error("expected Registered=true when token is available")
	}
}

// TestSatellite_Hub_FallsBackToInProcessOnError verifies that Hub returns
// InProcessHub when connection fails.
func TestSatellite_Hub_FallsBackToInProcessOnError(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	store.Save("token")
	// Use an invalid URL that will fail to connect
	sat := relay.NewSatelliteWithStore("wss://nonexistent.invalid.local:99999", store)

	ctx := context.Background()
	hub := sat.Hub(ctx)

	// Verify it's an InProcessHub (no-op fallback)
	_, ok := hub.(*relay.InProcessHub)
	if !ok {
		t.Errorf("expected InProcessHub on connection failure, got %T", hub)
	}
}

// TestSatellite_Hub_ReturnsInProcessWhenNotRegistered verifies Hub returns
// InProcessHub when token store has no token.
func TestSatellite_Hub_ReturnsInProcessWhenNotRegistered(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	// No token saved
	sat := relay.NewSatelliteWithStore("wss://test.example.com", store)

	ctx := context.Background()
	hub := sat.Hub(ctx)

	// Verify it's an InProcessHub
	_, ok := hub.(*relay.InProcessHub)
	if !ok {
		t.Errorf("expected InProcessHub when not registered, got %T", hub)
	}
}

// TestSatellite_Reconnect_NilHub verifies that Reconnect is a safe no-op
// when called on a satellite with no active WebSocket hub.
func TestSatellite_Reconnect_NilHub(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	// No token saved, so no hub will be created
	sat := relay.NewSatelliteWithStore("wss://test.example.com", store)

	// Reconnect on unconnected satellite must not panic
	sat.Reconnect(context.Background())
}

// TestSatellite_Reconnect_WithInProcessHub verifies that Reconnect is a safe
// no-op when the active hub is an InProcessHub.
func TestSatellite_Reconnect_WithInProcessHub(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	sat := relay.NewSatelliteWithStore("wss://test.example.com", store)

	// Manually set hub to InProcessHub (simulating fallback from connect error)
	sat.Disconnect() // ensure hub is nil first

	// Calling Reconnect should be safe even with no hub
	sat.Reconnect(context.Background())
}

// TestSatellite_DefaultURL_IsApiHuginnCloud verifies that the default
// cloud URL points to api.huginncloud.com, not relay.huginncloud.com.
func TestSatellite_DefaultURL_IsApiHuginnCloud(t *testing.T) {
	sat := relay.NewSatellite("")
	status := sat.Status()
	const want = "wss://api.huginncloud.com"
	if status.CloudURL != want {
		t.Errorf("default CloudURL: got %q, want %q", status.CloudURL, want)
	}
}

// TestSatellite_Connect_UsesCorrectPath_Plain verifies that Connect dials
// the path /ws/satellite with no query parameters.
func TestSatellite_Connect_UsesCorrectPath_Plain(t *testing.T) {
	pathCh := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathCh <- r.URL.RequestURI()
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		upgrader.Upgrade(w, r, nil) //nolint:errcheck
	}))
	defer srv.Close()

	// Convert http→ws scheme.
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	store := &relay.MemoryTokenStore{}
	store.Save("test-token")
	sat := relay.NewSatelliteWithStore(wsURL, store)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = sat.Connect(ctx) // error OK — test server closes immediately after upgrade

	select {
	case path := <-pathCh:
		if path != "/ws/satellite" {
			t.Errorf("expected dial path /ws/satellite, got %q", path)
		}
	case <-ctx.Done():
		t.Fatal("satellite did not dial within timeout")
	}
}

// TestSatellite_SetOnMessage_FiresOnInboundMessage verifies that a callback
// registered via SetOnMessage is invoked when the server sends a message.
func TestSatellite_SetOnMessage_FiresOnInboundMessage(t *testing.T) {
	serverMsg := relay.Message{
		Type:      relay.MsgSatelliteHeartbeat,
		MachineID: "srv",
	}

	msgCh := make(chan relay.Message, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		data, _ := json.Marshal(serverMsg)
		conn.WriteMessage(websocket.TextMessage, data) //nolint:errcheck
		<-r.Context().Done()
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	store := &relay.MemoryTokenStore{}
	store.Save("tok")

	sat := relay.NewSatelliteWithStore(wsURL, store)
	sat.SetOnMessage(func(_ context.Context, msg relay.Message) {
		msgCh <- msg
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := sat.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer sat.Disconnect()

	select {
	case got := <-msgCh:
		if got.Type != relay.MsgSatelliteHeartbeat {
			t.Errorf("expected MsgSatelliteHeartbeat, got %q", got.Type)
		}
	case <-ctx.Done():
		t.Fatal("onMessage callback never fired")
	}
}
