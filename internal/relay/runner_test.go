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

func TestRunner_StartsAndStops(t *testing.T) {
	// Use SkipConnectOnStart to avoid real network dials
	runner := relay.NewRunner(relay.RunnerConfig{
		MachineID:          "test-machine",
		HeartbeatInterval:  10 * time.Millisecond,
		SkipConnectOnStart: true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Good: runner stopped when context was cancelled.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("runner did not stop after context cancellation")
	}
}

func TestRunner_WithStorePath_OpensStoreAndStops(t *testing.T) {
	dir := t.TempDir()

	runner := relay.NewRunner(relay.RunnerConfig{
		MachineID:          "test-machine",
		HeartbeatInterval:  10 * time.Millisecond,
		SkipConnectOnStart: true,
		StorePath:          dir,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Good: runner started with StorePath and stopped cleanly.
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not stop after context cancellation")
	}
}

func TestRunner_DispatcherHandlesSessionListRequest(t *testing.T) {
	replyCh := make(chan []byte, 4)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Read satellite_hello (first message from client).
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var hello map[string]any
		json.Unmarshal(data, &hello)

		// Send session_list_request - use the machine_id from hello.
		machineID := "test-machine"
		if hello != nil {
			if mid, ok := hello["machine_id"].(string); ok {
				machineID = mid
			}
		}

		req, _ := json.Marshal(map[string]any{
			"type":       "session_list_request",
			"machine_id": machineID,
			"payload":    map[string]any{},
		})
		if err := conn.WriteMessage(websocket.TextMessage, req); err != nil {
			return
		}

		// Read the reply (session_list_result).
		_, data, err = conn.ReadMessage()
		if err != nil {
			return
		}
		replyCh <- data

		<-r.Context().Done()
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	store := &relay.MemoryTokenStore{}
	store.Save("tok")

	dir := t.TempDir()
	runner := relay.NewRunner(relay.RunnerConfig{
		MachineID:         "test-machine",
		HeartbeatInterval: 10 * time.Second,
		CloudURL:          wsURL,
		StorePath:         dir,
		TokenStore:        store,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go runner.Run(ctx)

	select {
	case data := <-replyCh:
		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("reply is not valid JSON: %v", err)
		}
		if msg["type"] != "session_list_result" {
			t.Errorf("expected type session_list_result, got %q", msg["type"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not dispatch session_list_request within timeout")
	}
}
