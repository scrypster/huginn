package relay_test

// satellite_e2e_test.go — end-to-end relay round-trip tests.
//
// These tests verify the full huginn→HuginnCloud message path:
//   1. Satellite dials MockCloudWS (in-process httptest server).
//   2. A relay.Message is sent via satellite.ActiveHub().Send().
//   3. MockCloudWS receives the message and exposes it for assertion.
//
// This catches regressions in:
//   - WebSocket connection setup (Authorization header, /ws/satellite path)
//   - JSON framing (gorilla TextMessage, relay.Message shape)
//   - CircuitBreakerState() reflecting real connection state

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/relay"
	"github.com/scrypster/huginn/internal/relay/testutil"
)

// wsBaseURL converts an http://... URL to ws://... for use as satellite baseURL.
// The satellite appends /ws/satellite automatically.
func wsBaseURL(httpURL string) string {
	return strings.Replace(httpURL, "http://", "ws://", 1)
}

// waitConnected polls satellite.Status().Connected until true or the deadline.
func waitConnected(sat *relay.Satellite, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if sat.Status().Connected {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// TestSatelliteRelayRoundTrip verifies the full send path:
// Satellite connects → sends MsgNotificationSync → MockCloudWS receives it.
func TestSatelliteRelayRoundTrip(t *testing.T) {
	mc := testutil.StartMockCloudWS(t)

	machineID := "test-machine-e2e"
	tokenStore := relay.NewMemoryTokenStore()

	token := testutil.ExchangeToken(t, mc.Server, machineID)
	if err := tokenStore.Save(token); err != nil {
		t.Fatalf("save token: %v", err)
	}

	sat := relay.NewSatelliteWithStore(wsBaseURL(mc.URL()), tokenStore)
	sat.SetMachineID(machineID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := sat.Connect(ctx); err != nil {
		t.Fatalf("satellite connect: %v", err)
	}
	defer sat.Disconnect()

	if !waitConnected(sat, 5*time.Second) {
		t.Fatal("satellite did not reach Connected state within 5s")
	}

	msg := relay.Message{
		Type: relay.MsgNotificationSync,
		Payload: map[string]any{
			"id":            "notif-e2e-001",
			"workflow_id":   "wf-e2e",
			"summary":       "e2e test notification",
			"severity":      "info",
			"status":        "pending",
			"pending_count": 1,
		},
	}
	if err := sat.ActiveHub().Send("", msg); err != nil {
		t.Fatalf("hub.Send: %v", err)
	}

	// Use WaitMessageOfType to skip the satellite_hello handshake message
	// that the satellite sends automatically on connect.
	received, ok := mc.WaitMessageOfType(relay.MsgNotificationSync, 5*time.Second)
	if !ok {
		t.Fatal("mock cloud did not receive a MsgNotificationSync within 5s")
	}

	if id, _ := received.Payload["id"].(string); id != "notif-e2e-001" {
		t.Errorf("received payload id = %q, want %q", id, "notif-e2e-001")
	}
}

// TestSatelliteRelayRoundTrip_RejectsUnregistered verifies that a satellite
// with no stored token fails to connect (returns error, does not panic).
func TestSatelliteRelayRoundTrip_RejectsUnregistered(t *testing.T) {
	mc := testutil.StartMockCloudWS(t)

	tokenStore := relay.NewMemoryTokenStore()
	// No token saved — satellite is not registered.

	sat := relay.NewSatelliteWithStore(wsBaseURL(mc.URL()), tokenStore)
	sat.SetMachineID("unregistered")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := sat.Connect(ctx)
	// Should fail because Load() returns error (no token), causing Connect
	// to return "not registered" before dialing.
	if err == nil {
		t.Error("expected Connect to fail for unregistered satellite; got nil")
	}
}

// TestSatelliteCircuitBreakerState_Connected verifies CircuitBreakerState
// returns "closed" after a successful connection.
func TestSatelliteCircuitBreakerState_Connected(t *testing.T) {
	mc := testutil.StartMockCloudWS(t)

	machineID := "test-machine-cb"
	tokenStore := relay.NewMemoryTokenStore()
	token := testutil.ExchangeToken(t, mc.Server, machineID)
	if err := tokenStore.Save(token); err != nil {
		t.Fatalf("save token: %v", err)
	}

	sat := relay.NewSatelliteWithStore(wsBaseURL(mc.URL()), tokenStore)
	sat.SetMachineID(machineID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := sat.Connect(ctx); err != nil {
		t.Fatalf("satellite connect: %v", err)
	}
	defer sat.Disconnect()

	if !waitConnected(sat, 5*time.Second) {
		t.Fatal("satellite did not connect within 5s")
	}

	if state := sat.CircuitBreakerState(); state != "closed" {
		t.Errorf("CircuitBreakerState() = %q after successful connect, want %q", state, "closed")
	}
}

// TestSatelliteCircuitBreakerState_Disconnected verifies CircuitBreakerState
// returns "closed" (safe default) when the satellite has no active hub.
func TestSatelliteCircuitBreakerState_Disconnected(t *testing.T) {
	sat := relay.NewSatelliteWithStore("ws://localhost:0", relay.NewMemoryTokenStore())
	if state := sat.CircuitBreakerState(); state != "closed" {
		t.Errorf("CircuitBreakerState() = %q for unconnected satellite, want %q", state, "closed")
	}
}

// TestMockCloudWS_WaitMessageTimeout verifies WaitMessage returns false when
// no messages arrive within the timeout.
func TestMockCloudWS_WaitMessageTimeout(t *testing.T) {
	mc := testutil.StartMockCloudWS(t)
	_, ok := mc.WaitMessage(50 * time.Millisecond)
	if ok {
		t.Error("WaitMessage returned true but no message was sent")
	}
}
