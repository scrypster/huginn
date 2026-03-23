package relay

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	wsWriteBufSize = 256
	wsPingInterval = 30 * time.Second
	wsPongTimeout  = 10 * time.Second
	wsInitDelay    = time.Second
	wsMaxDelay     = 30 * time.Second
)

// WebSocketConfig holds the connection parameters for WebSocketHub.
type WebSocketConfig struct {
	URL           string
	Token         string                // static token; used when TokenProvider is nil
	TokenProvider func() (string, error) // optional; called on each dial to get a fresh token
	MachineID     string
	Version       string
	Store         *SessionStore // optional session store for satellite_hello
}

// WebSocketHub is a Hub that sends messages to HuginnCloud via WebSocket.
// It reconnects automatically when the connection drops.
//
// Thread-safety: Send is safe for concurrent use. The writeCh channel
// serialises all writes to the active connection through writeLoop.
//
// onMessage is stored as an atomic.Pointer so that readPump (the hot message
// receive path) can load the callback without acquiring any lock. This avoids
// all lock contention between SetOnMessage and the high-frequency readPump
// dispatch loop. The pointer stores *func(context.Context, Message) because
// atomic.Pointer requires a pointer type.
type WebSocketHub struct {
	cfg       WebSocketConfig
	onMessage atomic.Pointer[func(context.Context, Message)]

	writeCh   chan []byte // serialised write requests; shared across reconnections
	mu        sync.RWMutex
	conn      *websocket.Conn
	closeOnce sync.Once
	done      chan struct{}

	backoffMu    sync.Mutex
	backoffDelay time.Duration

	cb CircuitBreaker // guards reconnect loop against connection storms

	outboxMu sync.RWMutex
	outbox   *Outbox // optional overflow queue; set via SetOutbox

	droppedMessages atomic.Int64 // incremented when the write buffer is full and no outbox is wired
}

// NewWebSocketHub creates a WebSocketHub with the provided config.
// Call Connect to establish the connection.
func NewWebSocketHub(cfg ...WebSocketConfig) *WebSocketHub {
	var c WebSocketConfig
	if len(cfg) > 0 {
		c = cfg[0]
	}
	return &WebSocketHub{
		cfg:     c,
		done:    make(chan struct{}),
		writeCh: make(chan []byte, wsWriteBufSize),
	}
}

// SetOnMessage registers a callback that fires for every message received from
// the server. The store is lock-free: the atomic.Pointer write is immediately
// visible to any concurrent readPump goroutine without acquiring any mutex,
// eliminating contention on the hot message-receive path.
//
// Passing nil clears the callback; subsequent messages will be silently
// discarded until a non-nil callback is registered.
func (h *WebSocketHub) SetOnMessage(fn func(context.Context, Message)) {
	if fn == nil {
		h.onMessage.Store(nil)
		return
	}
	h.onMessage.Store(&fn)
}

// ResetBackoff resets the reconnect delay to wsInitDelay and clears the
// circuit breaker. Called by Satellite.Reconnect() after a detected wake event
// (e.g., laptop waking from sleep, network interface coming back up).
func (h *WebSocketHub) ResetBackoff() {
	h.backoffMu.Lock()
	h.backoffDelay = wsInitDelay
	h.backoffMu.Unlock()
	h.cb.Reset()
}

// Connect dials the WebSocket server, sends satellite_hello, and starts the
// background read pump, write loop, and ping loop.
// Returns an error only if the initial dial fails.
func (h *WebSocketHub) Connect(ctx context.Context) error {
	conn, err := h.dial(ctx)
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.conn = conn
	h.mu.Unlock()

	if err := h.sendHello(conn); err != nil {
		conn.Close()
		return err
	}

	go h.writeLoop(conn)
	go h.pingLoop(conn)
	go h.readPump(ctx, conn)
	return nil
}

// SetOutbox wires an Outbox that receives overflow messages when the write
// buffer is full. Safe to call before or after Connect.
func (h *WebSocketHub) SetOutbox(ob *Outbox) {
	h.outboxMu.Lock()
	h.outbox = ob
	h.outboxMu.Unlock()
}

// NOTE(security): Huginn communicates with HuginnCloud exclusively over TLS, which
// provides message confidentiality and integrity. Per-message HMAC signatures are
// therefore not required and would add CPU overhead for no security benefit.
// FUTURE: if this relay is ever operated without TLS (e.g., raw TCP), add
// HMAC-SHA256 per-message signing and a nonce/replay-window mechanism.

// Send encodes msg as JSON and pushes it to the write channel.
// Returns ErrNotActivated if the hub is not connected or closed.
// If an outbox is wired and the write buffer is full the message is enqueued
// to the outbox instead of returning ErrWriteBufferFull.
// machineID is ignored (kept for Hub interface compatibility).
func (h *WebSocketHub) Send(_ string, msg Message) error {
	h.mu.RLock()
	conn := h.conn
	h.mu.RUnlock()
	if conn == nil {
		return ErrNotActivated
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	select {
	case h.writeCh <- data:
		return nil
	case <-h.done:
		return ErrNotActivated
	default:
		// Write buffer full — try outbox before returning an error.
		h.outboxMu.RLock()
		ob := h.outbox
		h.outboxMu.RUnlock()
		if ob != nil {
			return ob.Enqueue(msg)
		}
		h.droppedMessages.Add(1)
		slog.Warn("relay: write buffer full, message dropped",
			"total_dropped", h.droppedMessages.Load())
		return ErrWriteBufferFull
	}
}

// Close closes the WebSocket connection. Safe to call multiple times.
// machineID is ignored (kept for Hub interface compatibility).
func (h *WebSocketHub) Close(_ string) {
	h.closeOnce.Do(func() {
		close(h.done)
		h.mu.Lock()
		conn := h.conn
		h.mu.Unlock()
		if conn != nil {
			conn.Close()
		}
	})
}

// DroppedMessages returns the total number of messages dropped because the
// write buffer was full and no outbox was wired. Monotonically increasing.
func (h *WebSocketHub) DroppedMessages() int64 {
	return h.droppedMessages.Load()
}

// writeLoop is the sole writer to conn. It drains writeCh and forwards
// each message to conn.WriteMessage. Exits when done is closed or on
// write error (read pump will reconnect and restart a new writeLoop).
func (h *WebSocketHub) writeLoop(conn *websocket.Conn) {
	for {
		select {
		case <-h.done:
			return
		case data := <-h.writeCh:
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				// Connection dead. readPump will trigger reconnect which starts
				// a new writeLoop. Messages in writeCh are preserved.
				return
			}
		}
	}
}

// pingLoop sends a WebSocket ping every wsPingInterval.
// gorilla/websocket allows WriteControl concurrent with WriteMessage, so
// this goroutine is safe alongside writeLoop.
// Exits when done is closed or ping fails (indicating dead connection).
func (h *WebSocketHub) pingLoop(conn *websocket.Conn) {
	ticker := time.NewTicker(wsPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-ticker.C:
			deadline := time.Now().Add(wsPongTimeout)
			if err := conn.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
				slog.Debug("relay: ping failed, triggering reconnect", "err", err)
				conn.Close() // causes readPump to detect error and reconnect
				return
			}
		}
	}
}

// dial opens a new WebSocket connection using the configured URL and token.
// If TokenProvider is set it is called on each dial to obtain a fresh token,
// allowing JWT rotation across reconnects. Falls back to the static Token field.
func (h *WebSocketHub) dial(ctx context.Context) (*websocket.Conn, error) {
	token := h.cfg.Token
	if h.cfg.TokenProvider != nil {
		var err error
		token, err = h.cfg.TokenProvider()
		if err != nil {
			slog.Warn("relay: TokenProvider failed, falling back to static token", "err", err)
			token = h.cfg.Token
		}
	}
	header := make(map[string][]string)
	if token != "" {
		header["Authorization"] = []string{"Bearer " + token}
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, h.cfg.URL, header)
	if err != nil {
		return nil, err
	}
	// Set pong handler: reset read deadline on each pong so the connection
	// stays alive as long as the server is responding to pings.
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(wsPingInterval + wsPongTimeout))
	})
	// Initial read deadline: allow one full ping interval + pong timeout before
	// the first ping is sent.
	conn.SetReadDeadline(time.Now().Add(wsPingInterval + wsPongTimeout)) //nolint:errcheck
	return conn, nil
}

// sendHello transmits the satellite_hello message on conn.
// It includes active_sessions from the SessionStore if available.
func (h *WebSocketHub) sendHello(conn *websocket.Conn) error {
	payload := map[string]any{
		"version": h.cfg.Version,
	}

	// Include active sessions if the store is available.
	if h.cfg.Store != nil {
		sessions, err := h.cfg.Store.List()
		if err != nil {
			slog.Warn("relay: could not list sessions for satellite_hello", "err", err)
			// Continue without sessions rather than failing the entire hello.
		} else if len(sessions) > 0 {
			payload["active_sessions"] = sessions
		}
	}

	hello := Message{
		Type:      MsgSatelliteHello,
		MachineID: h.cfg.MachineID,
		Payload:   payload,
	}
	data, err := json.Marshal(hello)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

// readPump reads messages from conn and dispatches them to onMessage.
// When the connection drops it calls reconnect until done is closed.
func (h *WebSocketHub) readPump(ctx context.Context, conn *websocket.Conn) {
	defer conn.Close()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			select {
			case <-h.done:
				return
			default:
			}

			newConn := h.reconnect(ctx)
			if newConn == nil {
				return // done channel closed during reconnect
			}
			conn = newConn
			continue
		}

		// Load the callback atomically — no lock required. The atomic.Pointer
		// guarantees that we see either the old or the new callback, never a
		// torn write. This is the hot path: one atomic load per received message.
		if cbp := h.onMessage.Load(); cbp != nil {
			var msg Message
			if json.Unmarshal(data, &msg) == nil {
				func() {
					defer func() {
						if r := recover(); r != nil {
							slog.Error("relay: onMessage callback panicked", "recover", r)
						}
					}()
					(*cbp)(ctx, msg)
				}()
			}
		}
	}
}

// reconnect loops with exponential backoff until a new connection is
// established or done is closed. Returns nil if done is closed.
// Backoff: 1s → 2s → 4s → 8s → 16s → 30s (cap) with 10% jitter.
// A CircuitBreaker prevents hammering the server after sustained outages:
// after cbThreshold consecutive failures the circuit opens for cbOpenDuration.
func (h *WebSocketHub) reconnect(ctx context.Context) *websocket.Conn {
	// Initialize backoff delay.
	h.backoffMu.Lock()
	if h.backoffDelay == 0 {
		h.backoffDelay = wsInitDelay
	}
	h.backoffMu.Unlock()

	for {
		// Circuit breaker: while Open, wait until the window expires rather than
		// spinning on dial attempts. Allow() transitions Open → HalfOpen when
		// the window lapses so one probe attempt can proceed.
		if !h.cb.Allow() {
			timer := time.NewTimer(wsInitDelay)
			select {
			case <-h.done:
				timer.Stop()
				return nil
			case <-timer.C:
			}
			continue
		}

		h.backoffMu.Lock()
		delay := h.backoffDelay
		h.backoffMu.Unlock()

		timer := time.NewTimer(delay)
		select {
		case <-h.done:
			timer.Stop()
			return nil
		case <-timer.C:
		}

		conn, err := h.dial(ctx)
		if err != nil {
			slog.Debug("relay: reconnect dial failed", "err", err, "next_delay", delay)
			h.cb.RecordFailure()
			h.backoffMu.Lock()
			h.backoffDelay = nextBackoff(delay, wsMaxDelay)
			h.backoffMu.Unlock()
			continue
		}
		if err := h.sendHello(conn); err != nil {
			conn.Close()
			h.cb.RecordFailure()
			h.backoffMu.Lock()
			h.backoffDelay = nextBackoff(delay, wsMaxDelay)
			h.backoffMu.Unlock()
			continue
		}

		h.cb.RecordSuccess()
		h.backoffMu.Lock()
		h.backoffDelay = wsInitDelay // reset backoff on success
		h.backoffMu.Unlock()

		h.mu.Lock()
		h.conn = conn
		h.mu.Unlock()

		// Restart per-connection goroutines.
		go h.writeLoop(conn)
		go h.pingLoop(conn)

		slog.Info("relay: reconnected to HuginnCloud")
		return conn
	}
}

// nextBackoff doubles delay up to max, then adds up to 10% positive jitter.
func nextBackoff(delay, max time.Duration) time.Duration {
	delay *= 2
	if delay > max {
		delay = max
	}
	// Add up to 10% positive jitter to prevent thundering herd.
	jitter := time.Duration(rand.Int63n(int64(delay / 10)))
	return delay + jitter
}

// ErrNotActivated is returned when WebSocketHub is used before activation
// or the connection is closed.
var ErrNotActivated = errNotActivated{}

type errNotActivated struct{}

func (errNotActivated) Error() string {
	return "relay: WebSocket hub not activated — run `huginn relay start`"
}

// ErrWriteBufferFull is returned when Send() cannot enqueue because the
// write buffer (size wsWriteBufSize) is full. This indicates the satellite
// is under heavy backpressure — messages will be preserved in the Pebble
// outbox (Task 6).
var ErrWriteBufferFull = errWriteBufferFull{}

type errWriteBufferFull struct{}

func (errWriteBufferFull) Error() string {
	return "relay: write buffer full"
}
