package server

// cloud_connect_test.go — tests for POST /api/v1/cloud/connect
//
// Covered scenarios:
//  1. Token already stored, no satellite → response is {"status":"already_registered"}
//  2. Token already stored, satellite wired → sat.Connect() is called within 500ms
//  3. No token → response is {"status":"registering"}
//  4. Registration in progress → second POST returns {"status":"registering"}

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/relay"
)

func TestHandleCloudConnect_AlreadyRegistered_Status(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	if err := store.Save("existing-jwt-token"); err != nil {
		t.Fatal(err)
	}

	srv, ts := newTestServer(t)
	srv.SetRelayConfig(store, "")

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/cloud/connect", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/v1/cloud/connect: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "already_registered" {
		t.Errorf("status = %v, want already_registered", body["status"])
	}
}

// TestHandleCloudConnect_AlreadyRegistered_SatelliteConnectCalled verifies that
// when a token is already stored, the server fires a background goroutine that
// calls sat.Connect(). We confirm by counting TCP hits on a fake WebSocket server.
func TestHandleCloudConnect_AlreadyRegistered_SatelliteConnectCalled(t *testing.T) {
	// Fake WebSocket server: records every connection attempt. Returns 401 so the
	// WS upgrade fails — we only need to observe that the dial was attempted.
	var attempts atomic.Int32
	fakeCloudd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer fakeCloudd.Close()

	store := &relay.MemoryTokenStore{}
	if err := store.Save("existing-jwt-token"); err != nil {
		t.Fatal(err)
	}

	srv, ts := newTestServer(t)
	srv.SetRelayConfig(store, "")

	// Point the satellite at the fake server (ws:// scheme).
	wsBase := "ws" + fakeCloudd.URL[len("http"):]
	sat := relay.NewSatelliteWithStore(wsBase, store)
	srv.SetSatellite(sat)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/cloud/connect", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/v1/cloud/connect: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "already_registered" {
		t.Fatalf("status = %v, want already_registered", body["status"])
	}

	// Wait up to 500ms for the background goroutine to attempt Connect().
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	for {
		if attempts.Load() > 0 {
			return // success
		}
		select {
		case <-ctx.Done():
			t.Error("satellite.Connect() was not called within 500ms after already_registered response")
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestHandleCloudConnect_NotRegistered_StartsRegistering(t *testing.T) {
	store := &relay.MemoryTokenStore{} // empty — no token

	srv, ts := newTestServer(t)
	srv.SetRelayConfig(store, "")
	// newTestServer already sets openBrowserFn to a no-op.

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/cloud/connect", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/v1/cloud/connect: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "registering" {
		t.Errorf("status = %v, want registering", body["status"])
	}
}

func TestHandleCloudConnect_AlreadyRegistering_ReturnsRegistering(t *testing.T) {
	store := &relay.MemoryTokenStore{} // empty so the first POST starts a flow

	srv, ts := newTestServer(t)
	srv.SetRelayConfig(store, "")

	doConnect := func() map[string]any {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/cloud/connect", nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST /api/v1/cloud/connect: %v", err)
		}
		defer resp.Body.Close()
		var body map[string]any
		json.NewDecoder(resp.Body).Decode(&body) //nolint:errcheck
		return body
	}

	body1 := doConnect()
	if body1["status"] != "registering" {
		t.Errorf("first call: want registering, got %v", body1["status"])
	}

	// Second call while first goroutine is still running.
	body2 := doConnect()
	if body2["status"] != "registering" {
		t.Errorf("second call: want registering, got %v", body2["status"])
	}
}
