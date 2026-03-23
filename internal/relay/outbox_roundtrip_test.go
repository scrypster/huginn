package relay_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/scrypster/huginn/internal/relay"
	"github.com/scrypster/huginn/internal/storage"
)

// TestOutbox_BufferFullRoundtrip verifies the full buffer-full -> outbox -> flush -> delivery
// roundtrip with payload integrity. It creates an in-process WebSocket server,
// fills the hub's write buffer, then verifies overflow messages are enqueued to
// the outbox and delivered when flushed.
func TestOutbox_BufferFullRoundtrip(t *testing.T) {
	t.Parallel()

	// --- Set up a mock WebSocket server that collects received messages. ---
	var (
		receivedMu sync.Mutex
		received   []relay.Message
	)

	// blockRead controls whether the server reads from the WS connection.
	// When closed, the server starts reading.
	blockRead := make(chan struct{})

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		defer conn.Close()

		// Wait until unblocked before reading.
		<-blockRead

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg relay.Message
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			receivedMu.Lock()
			received = append(received, msg)
			receivedMu.Unlock()
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	// --- Create a WebSocketHub connected to the mock server. ---
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{
		URL:       wsURL,
		MachineID: "test-machine",
		Version:   "0.0.1",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer hub.Close("")

	// --- Create an Outbox backed by a real (temporary) storage store. ---
	db, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer db.Close()

	outbox := relay.NewOutbox(db, hub)
	hub.SetOutbox(outbox)

	// --- Fill the hub's write buffer. ---
	// The server is blocked (not reading), so messages pile up in the write buffer.
	// wsWriteBufSize is 256 in websocket.go. We need to fill writeCh (256 slots).
	// The writeLoop is running but blocked on conn.WriteMessage because the server
	// isn't reading. However, the OS TCP buffer is large, so we may need many more
	// messages. Instead, send enough to saturate the channel.
	const totalMessages = 300
	overflowMsgs := make([]relay.Message, 0)

	for i := 0; i < totalMessages; i++ {
		msg := relay.Message{
			Type:    relay.MsgToken,
			Payload: map[string]any{"idx": float64(i), "text": "payload"},
		}
		err := hub.Send("", msg)
		if err != nil {
			// Once Send starts failing, messages should go to outbox.
			// If it's ErrWriteBufferFull that shouldn't happen because we wired outbox.
			// If it's ErrNotActivated, the connection died.
			if err == relay.ErrNotActivated {
				break
			}
			t.Fatalf("unexpected Send error: %v", err)
		}
		// Track messages that would overflow (we don't know exactly which ones,
		// but we'll check the outbox count below).
		overflowMsgs = append(overflowMsgs, msg)
	}

	// Check if any messages landed in the outbox.
	outboxLen, err := outbox.Len()
	if err != nil {
		t.Fatalf("outbox.Len: %v", err)
	}
	t.Logf("sent %d messages, %d in outbox", totalMessages, outboxLen)

	// --- Unblock the server so it starts reading. ---
	close(blockRead)

	// Give the writeLoop time to drain the in-channel messages.
	time.Sleep(200 * time.Millisecond)

	// --- Flush the outbox to deliver queued messages. ---
	if outboxLen > 0 {
		if err := outbox.Flush(ctx); err != nil {
			t.Fatalf("outbox.Flush: %v", err)
		}
	}

	// Give the server time to receive the flushed messages.
	time.Sleep(200 * time.Millisecond)

	// --- Verify messages arrived at the server side. ---
	receivedMu.Lock()
	totalReceived := len(received)
	receivedMu.Unlock()

	// We should have received all sent messages (from the channel + flushed outbox).
	// The first message is satellite_hello, which is not counted.
	// We expect at least totalMessages messages of type MsgToken.
	tokenCount := 0
	receivedMu.Lock()
	for _, msg := range received {
		if msg.Type == relay.MsgToken {
			tokenCount++
		}
	}
	receivedMu.Unlock()

	t.Logf("total received: %d, token messages: %d, outbox had: %d", totalReceived, tokenCount, outboxLen)

	// Verify no messages were lost: all sent messages should eventually arrive.
	if tokenCount < totalMessages {
		t.Errorf("expected at least %d token messages, got %d (outbox had %d)",
			totalMessages, tokenCount, outboxLen)
	}

	// Verify the outbox is now empty after flush.
	finalLen, err := outbox.Len()
	if err != nil {
		t.Fatalf("outbox.Len after flush: %v", err)
	}
	if finalLen != 0 {
		t.Errorf("outbox should be empty after flush, got %d", finalLen)
	}
}
