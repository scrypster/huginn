package server

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/logger"
	"github.com/scrypster/huginn/internal/memory"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/threadmgr"
	"github.com/scrypster/huginn/internal/workforce"
)

// agentFromDefWithVault wraps agents.FromDef and forces the resulting Agent
// to have its effective VaultName resolved exactly the way
// agents.BuildRegistryWithUsername does. Without this, agents whose
// VaultName field is empty in agents.json would be returned with VaultName=""
// — and connectAgentVault skips connecting in that case, so the orchestrator
// loses MuninnDB access for any chat handled via resolveAgentForMessage.
// This was the source of the "no muninn_* tool calls" regression: the
// registry built at startup had VaultName populated, but ws.go was building
// fresh Agents from defs and dropping it.
func agentFromDefWithVault(def agents.AgentDef) *agents.Agent {
	a := agents.FromDef(def)
	if a.VaultName == "" {
		username := memory.ResolveUsername("")
		a.VaultName = def.ResolvedVaultName(username)
	}
	return a
}

// serverEpoch is a random non-zero value generated at process startup. It is stamped
// on every session-scoped WebSocket message so that clients can detect server
// restarts and reset their sequence-number state.
// The value is constrained to 53 bits so JavaScript clients can compare it
// exactly (Number safe integer range).
var serverEpoch uint64

func init() {
	const maxSafeJSEpoch = uint64(1<<53) - 1
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		serverEpoch = binary.LittleEndian.Uint64(b[:]) & maxSafeJSEpoch
	}
	if serverEpoch == 0 {
		serverEpoch = 1
	}
}

const (
	// wsRateLimitMsgs is the maximum number of inbound WS messages allowed per window.
	wsRateLimitMsgs = 30
	// wsRateLimitWindow is the sliding window duration for WS rate limiting.
	wsRateLimitWindow = 10 * time.Second
	// wsSendBufSize is the capacity of each client's outbound send channel.
	// Sized to absorb short bursts without dropping messages for well-behaved clients.
	wsSendBufSize = 256

	// Protocol-level keepalive: the server sends RFC 6455 Ping control frames every
	// wsPingInterval and expects a Pong within wsPongWait. If the Pong does not
	// arrive the read deadline fires, ReadMessage returns an error, and the client
	// is cleanly unregistered. This detects silent TCP half-open connections that
	// would otherwise linger indefinitely.
	wsPingInterval = 30 * time.Second
	wsPongWait     = 10 * time.Second
	// wsWriteWait is the deadline for every individual write to the connection.
	// Prevents a slow network path from stalling the write goroutine indefinitely.
	wsWriteWait = 10 * time.Second
)

// wsMaxDrops is the consecutive-drop threshold before a slow client is evicted.
// Each drop means the 256-message send buffer was full when a broadcast arrived.
// On the wsMaxDrops-th consecutive drop the connection is closed with code 4002
// ("slow_client_eviction") and unregistered. Drops reset to zero on any success.
const wsMaxDrops int32 = 5

// wsReplayBufferSize is the number of recently broadcast session-scoped
// messages retained per session so that clients reconnecting after a drop can
// recover missed events via the "resume" message instead of losing them.
const wsReplayBufferSize = 512

// WSMessage is a message sent over the WebSocket connection.
type WSMessage struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload,omitempty"`
	// Common fields flattened for convenience:
	SessionID string `json:"session_id,omitempty"`
	Content   string `json:"content,omitempty"`
	// Epoch and Seq are stamped on session-scoped messages only (broadcastToSession).
	// Clients can use Epoch to detect server restarts and Seq for ordering/dedup.
	// Global broadcasts (broadcast) leave these zero (omitempty).
	Epoch uint64 `json:"epoch,omitempty"`
	Seq   uint64 `json:"seq,omitempty"`
	// RunID ties a streaming event to the specific agent run that produced it,
	// allowing the frontend to discard stale events from previous runs.
	RunID string `json:"run_id,omitempty"`
	Agent string `json:"agent,omitempty"`
}

type wsClient struct {
	conn      *websocket.Conn
	send      chan WSMessage
	sessionID string // empty = receives all broadcasts (wildcard)
	ctx       context.Context
	cancel    context.CancelFunc

	// Per-connection inbound rate limiting (30 msgs / 10 s).
	// msgCount is accessed atomically; msgMu guards msgWindowStart.
	msgCount       int64
	msgWindowStart time.Time
	msgMu          sync.Mutex

	// consecutiveDrops tracks how many consecutive broadcast messages were
	// dropped because the send channel was full. Accessed atomically.
	// Reset to 0 on every successful enqueue; client evicted at wsMaxDrops.
	consecutiveDrops int32
}

// safeSend enqueues msg on the client's send channel without panicking if
// the channel is closed or the context has been cancelled (client disconnected).
// Returns true if the message was delivered, false if the client is gone.
func (c *wsClient) safeSend(msg WSMessage) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	select {
	case c.send <- msg:
		return true
	case <-c.ctx.Done():
		return false
	}
}

// wsRateAllow returns true if the inbound message is within the rate limit.
// It implements a fixed-window counter per wsRateLimitWindow.
func (c *wsClient) wsRateAllow() bool {
	c.msgMu.Lock()
	defer c.msgMu.Unlock()
	now := time.Now()
	if now.Sub(c.msgWindowStart) >= wsRateLimitWindow {
		// Start a new window.
		c.msgWindowStart = now
		atomic.StoreInt64(&c.msgCount, 1)
		return true
	}
	n := atomic.AddInt64(&c.msgCount, 1)
	return n <= wsRateLimitMsgs
}

// replayRing is a fixed-capacity ring buffer of recently broadcast
// session-scoped WSMessages. Messages are appended in sequence order, so
// iteration from the oldest slot yields ascending Seq values.
// All access must be guarded by WSHub.seqMu.
type replayRing struct {
	buf  []WSMessage
	next int  // index of the next write
	full bool // true once the ring has wrapped at least once
}

func newReplayRing(capacity int) *replayRing {
	return &replayRing{buf: make([]WSMessage, capacity)}
}

func (r *replayRing) add(msg WSMessage) {
	r.buf[r.next] = msg
	r.next = (r.next + 1) % len(r.buf)
	if r.next == 0 {
		r.full = true
	}
}

// since returns all buffered messages with Seq > seq, oldest first.
func (r *replayRing) since(seq uint64) []WSMessage {
	start, n := 0, r.next
	if r.full {
		start, n = r.next, len(r.buf)
	}
	var out []WSMessage
	for i := 0; i < n; i++ {
		m := r.buf[(start+i)%len(r.buf)]
		if m.Seq > seq {
			out = append(out, m)
		}
	}
	return out
}

// oldestSeq returns the sequence number of the oldest buffered message,
// or 0 when the ring is empty.
func (r *replayRing) oldestSeq() uint64 {
	if r.full {
		return r.buf[r.next].Seq
	}
	if r.next == 0 {
		return 0
	}
	return r.buf[0].Seq
}

// WSHub manages all active WebSocket client connections.
type WSHub struct {
	clients    map[*wsClient]struct{}
	mu         sync.RWMutex
	broadcastC chan WSMessage
	stopC      chan struct{}
	stopOnce   sync.Once // ensures stop() is idempotent
	stopped    int32     // atomic: 1 once stop() has been called
	// seqMu guards sessionSeq and sessionReplay. We use a separate mutex so
	// broadcastToSession can hold the RLock on mu (for clients) while atomically
	// incrementing the per-session sequence counter.
	seqMu      sync.Mutex
	sessionSeq map[string]uint64
	// sessionReplay holds the per-session replay ring of recently broadcast
	// messages, recorded at the same place Seq numbers are stamped. Used by the
	// "resume" WS message to recover events missed during a disconnect.
	sessionReplay     map[string]*replayRing
	wsDroppedMessages atomic.Int64
}

func newWSHub() *WSHub {
	return &WSHub{
		clients:       make(map[*wsClient]struct{}),
		broadcastC:    make(chan WSMessage, 256),
		stopC:         make(chan struct{}),
		sessionSeq:    make(map[string]uint64),
		sessionReplay: make(map[string]*replayRing),
	}
}

// recordForReplayLocked appends msg to the session's replay ring, creating the
// ring on first use. Caller must hold seqMu.
func (h *WSHub) recordForReplayLocked(sessionID string, msg WSMessage) {
	ring := h.sessionReplay[sessionID]
	if ring == nil {
		ring = newReplayRing(wsReplayBufferSize)
		h.sessionReplay[sessionID] = ring
	}
	ring.add(msg)
}

// stampAndRecord assigns the next sequence number (and the process epoch) to a
// session-scoped message and records it in the session's replay ring.
func (h *WSHub) stampAndRecord(sessionID string, msg WSMessage) WSMessage {
	msg.SessionID = sessionID
	msg.Epoch = serverEpoch
	h.seqMu.Lock()
	h.sessionSeq[sessionID]++
	msg.Seq = h.sessionSeq[sessionID]
	h.recordForReplayLocked(sessionID, msg)
	h.seqMu.Unlock()
	return msg
}

// replaySince returns the buffered messages for sessionID with Seq > lastSeq
// (oldest first), the session's current sequence number, and gap=true when the
// replay buffer cannot cover everything the client missed — in which case the
// client should re-fetch history via REST.
func (h *WSHub) replaySince(sessionID string, lastSeq uint64) (msgs []WSMessage, currentSeq uint64, gap bool) {
	h.seqMu.Lock()
	defer h.seqMu.Unlock()
	currentSeq = h.sessionSeq[sessionID]
	if lastSeq >= currentSeq {
		// Client is up to date — or ahead of the server, which means the server
		// restarted and seq counters reset; flag that as a gap so the client
		// re-fetches history rather than trusting stale local state.
		return nil, currentSeq, lastSeq > currentSeq
	}
	ring := h.sessionReplay[sessionID]
	if ring == nil {
		return nil, currentSeq, true
	}
	msgs = ring.since(lastSeq)
	if oldest := ring.oldestSeq(); oldest == 0 || oldest > lastSeq+1 {
		gap = true
	}
	return msgs, currentSeq, gap
}

func (h *WSHub) run() {
	for {
		select {
		case <-h.stopC:
			return
		case msg := <-h.broadcastC:
			h.mu.RLock()
			for c := range h.clients {
				h.trySendOrEvict(c, msg, c.sessionID)
			}
			h.mu.RUnlock()
		}
	}
}

// stop signals the hub's run goroutine to exit and cancels all active
// per-connection contexts so that in-flight chat goroutines are notified.
// It drains any pending messages in broadcastC before stopping so that
// messages enqueued just before shutdown are delivered to connected clients.
// stop is idempotent — calling it more than once is safe.
func (h *WSHub) stop() {
	h.stopOnce.Do(func() {
		atomic.StoreInt32(&h.stopped, 1)
		// Drain any messages queued in broadcastC before we cancel clients.
		// We hold the RLock while draining so delivery is atomic with respect
		// to client registration changes.
		h.mu.RLock()
		for {
			select {
			case msg := <-h.broadcastC:
				for c := range h.clients {
					select {
					case c.send <- msg:
					default:
					}
				}
			default:
				goto drained
			}
		}
	drained:
		for c := range h.clients {
			if c.cancel != nil {
				c.cancel()
			}
		}
		h.mu.RUnlock()
		close(h.stopC)
	})
}

// trySendOrEvict attempts a non-blocking send to c, tracking consecutive drops
// and evicting the client (close code 4002) after wsMaxDrops in a row.
// Callers hold h.mu.RLock; eviction happens in a goroutine to avoid deadlock.
func (h *WSHub) trySendOrEvict(c *wsClient, msg WSMessage, sessionID string) {
	select {
	case c.send <- msg:
		atomic.StoreInt32(&c.consecutiveDrops, 0)
	default:
		// Slow client — buffer full, message dropped.
		h.wsDroppedMessages.Add(1)
		drops := atomic.AddInt32(&c.consecutiveDrops, 1)
		if drops == wsMaxDrops {
			slog.Error("ws: slow client evicted after repeated drops",
				"session_id", sessionID,
				"msg_type", msg.Type,
				"drops", drops,
				"total_dropped", h.wsDroppedMessages.Load())
			go func(evict *wsClient) {
				if evict.conn != nil {
					_ = evict.conn.WriteControl(websocket.CloseMessage,
						websocket.FormatCloseMessage(4002, "slow_client_eviction"),
						time.Now().Add(wsWriteWait))
				}
				h.unregisterClient(evict)
			}(c)
		} else if drops < wsMaxDrops {
			slog.Warn("ws: slow client, message dropped",
				"session_id", sessionID,
				"msg_type", msg.Type,
				"consecutive_drops", drops,
				"total_dropped", h.wsDroppedMessages.Load())
		}
	}
}

func (h *WSHub) broadcast(msg WSMessage) {
	slog.Debug("ws: broadcasting message", "type", msg.Type)
	select {
	case h.broadcastC <- msg:
	default:
		// Session-scoped messages are still stamped and recorded in the replay
		// buffer so a later "resume" can recover them even though the live
		// broadcast was dropped.
		if msg.SessionID != "" {
			h.stampAndRecord(msg.SessionID, msg)
		}
		slog.Warn("ws: broadcast channel full, dropping message", "type", msg.Type)
	}
}

// registerWithSession registers a client scoped to a specific session.
// Clients with empty sessionID receive all broadcasts (wildcard behavior
// preserved for non-session WebSocket connections).
// Registration is synchronous (holds the lock directly) so that a subsequent
// broadcastToSession call is guaranteed to see the client in the map.
// If the hub has already been stopped, the client context is cancelled
// immediately and the client is not added to the hub's client map.
func (h *WSHub) registerWithSession(c *wsClient, sessionID string) {
	c.sessionID = sessionID
	if atomic.LoadInt32(&h.stopped) == 1 {
		// Hub is stopped — cancel the client immediately so it knows not to use
		// this connection, and don't add it to the client map.
		if c.cancel != nil {
			c.cancel()
		}
		return
	}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

// broadcastToSession sends a message only to clients registered for sessionID,
// plus any wildcard clients (empty sessionID). Sets msg.SessionID, Epoch, and
// a monotonically increasing Seq automatically, and records the message in the
// session's replay ring for resume recovery.
func (h *WSHub) broadcastToSession(sessionID string, msg WSMessage) {
	h.broadcastToSessionFrom(sessionID, msg, nil)
}

// broadcastToSessionFrom is broadcastToSession with an optional origin client.
// The origin (the client whose inbound message triggered this broadcast) is
// guaranteed delivery exactly once: registered origins receive the message
// through the normal subscriber loop; origins not registered with the hub
// (e.g. already unregistered, or test clients) get a direct fallback send.
// This keeps streamed chat events flowing to every client subscribed to the
// session — including ones that reconnect mid-run — without ever double-sending
// to the originator.
func (h *WSHub) broadcastToSessionFrom(sessionID string, msg WSMessage, origin *wsClient) {
	msg = h.stampAndRecord(sessionID, msg)
	originSeen := false
	h.mu.RLock()
	for c := range h.clients {
		if c.sessionID == "" || c.sessionID == sessionID {
			if c == origin {
				originSeen = true
			}
			h.trySendOrEvict(c, msg, sessionID)
		}
	}
	h.mu.RUnlock()
	if origin != nil && !originSeen {
		origin.safeSend(msg)
	}
}

// hasSessionSubscribers reports whether any registered client would receive a
// broadcastToSession for sessionID — an exact session match, or a wildcard
// client (empty sessionID) that receives every session's traffic. Used by the
// permission prompt bridge to fail closed immediately instead of blocking on a
// prompt no one can answer.
func (h *WSHub) hasSessionSubscribers(sessionID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if c.sessionID == "" || c.sessionID == sessionID {
			return true
		}
	}
	return false
}

// WSDroppedMessages returns the total count of messages dropped due to slow
// client send buffers being full. Monotonically increasing.
func (h *WSHub) WSDroppedMessages() int64 {
	return h.wsDroppedMessages.Load()
}

// unregisterClient synchronously removes a client from the hub and cancels its
// per-connection context (which propagates to ws loops and in-flight chat goroutines).
// It intentionally does not close c.send to avoid send-on-closed-channel races
// with in-flight broadcasters.
func (h *WSHub) unregisterClient(c *wsClient) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
}

// DeleteSessionSeq removes the sequence counter and replay buffer for
// sessionID. Call this when a session is permanently deleted so the entries
// do not accumulate and so a recycled session ID starts fresh.
func (h *WSHub) DeleteSessionSeq(sessionID string) {
	h.seqMu.Lock()
	delete(h.sessionSeq, sessionID)
	delete(h.sessionReplay, sessionID)
	h.seqMu.Unlock()
}

// isLocalhostOrigin returns true when the origin URL refers to a loopback
// address (127.x.x.x / ::1 / localhost).
func isLocalhostOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// checkOrigin validates the WebSocket Origin header against the server's
// AllowedOrigins config.
//
//   - No Origin header → always allowed (non-browser / curl clients).
//   - Loopback origin  → always allowed regardless of AllowedOrigins.
//   - AllowedOrigins contains "*" → allow all (opt-in permissive mode).
//   - AllowedOrigins is nil/empty  → allow all (backwards-compat default).
//   - Otherwise → only origins in the explicit list are allowed.
func (s *Server) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // non-browser client
	}
	if isLocalhostOrigin(origin) {
		return true
	}
	allowed := s.cfg.WebUI.AllowedOrigins
	if len(allowed) == 0 {
		return true // backwards-compat: allow all when list is empty
	}
	for _, a := range allowed {
		if a == "*" {
			return true
		}
		if strings.EqualFold(a, origin) {
			return true
		}
	}
	return false
}

// sendPersistenceError sends a user-friendly error message to the WebSocket
// client when a storage operation fails. The raw Go error is not exposed to
// the client to avoid leaking internal implementation details.
func sendPersistenceError(c *wsClient, errCtx string, _ error) {
	msg := WSMessage{
		Type:    "error",
		Content: "A storage error occurred. Please try again.",
		Payload: map[string]any{
			"context": errCtx,
		},
	}
	select {
	case c.send <- msg:
	case <-c.ctx.Done():
		// client disconnected — do not block
	}
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Validate the token via the "huginn-token.<token>" Sec-WebSocket-Protocol
	// subprotocol (used by the web client) or, failing that, the legacy
	// ?token= query param (TUI client / back-compat). See wsAuthorizeToken.
	ok, subprotocol := wsAuthorizeToken(r, s.token)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// RFC 6455: a server that accepts a subprotocol MUST echo it back in the
	// response, or the browser rejects the handshake. The upgrader has no
	// static Subprotocols list (the token varies per request), so it's set
	// directly on the response header here.
	var respHeader http.Header
	if subprotocol != "" {
		respHeader = http.Header{"Sec-WebSocket-Protocol": []string{subprotocol}}
	}
	conn, err := s.upgrader.Upgrade(w, r, respHeader)
	if err != nil {
		return
	}

	// Protocol-level keepalive: set the initial read deadline so that a client
	// that never sends a pong (or any message) is detected and closed after
	// wsPingInterval + wsPongWait. The pong handler resets this deadline on
	// every pong response, keeping the connection alive for well-behaved clients.
	conn.SetReadDeadline(time.Now().Add(wsPingInterval + wsPongWait)) //nolint:errcheck
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(wsPingInterval + wsPongWait))
	})

	sessionID := r.URL.Query().Get("session_id") // optional; empty = all sessions
	ctx, cancel := context.WithCancel(context.Background())
	client := &wsClient{
		conn:           conn,
		send:           make(chan WSMessage, wsSendBufSize),
		ctx:            ctx,
		cancel:         cancel,
		msgWindowStart: time.Now(),
	}
	s.wsHub.registerWithSession(client, sessionID)

	go s.wsPingLoop(client)
	go s.wsWritePump(client)
	s.wsReadPump(client) // blocking
}

// wsPingLoop sends RFC 6455 Ping control frames every wsPingInterval.
// gorilla/websocket allows WriteControl concurrent with WriteMessage, so this
// goroutine is safe alongside wsWritePump. Exits when c.ctx is cancelled
// (client disconnected / hub stopping) or when a ping write fails (dead link).
func (s *Server) wsPingLoop(c *wsClient) {
	ticker := time.NewTicker(wsPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			deadline := time.Now().Add(wsPongWait)
			if err := c.conn.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
				slog.Debug("ws: ping failed, closing connection",
					"session_id", c.sessionID, "err", err)
				c.conn.Close() // causes wsReadPump to detect error and unregister
				return
			}
		}
	}
}

func (s *Server) wsWritePump(c *wsClient) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("wsWritePump panic recovered", "err", r)
		}
		c.conn.Close()
	}()
	for {
		var msg WSMessage
		select {
		case <-c.ctx.Done():
			return
		case m := <-c.send:
			msg = m
		}
		// Set a per-write deadline to prevent a slow network path from stalling
		// this goroutine indefinitely. The deadline is reset each iteration.
		c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait)) //nolint:errcheck
		data, _ := json.Marshal(msg)
		if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return
		}
	}
}

func (s *Server) wsReadPump(c *wsClient) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("wsReadPump panic recovered", "err", r)
		}
		s.wsHub.unregisterClient(c)
		c.conn.Close()
	}()
	// Limit inbound message size to 1 MB to prevent OOM on large payloads.
	c.conn.SetReadLimit(1 << 20)
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		// Per-connection rate limiting: 30 messages per 10 seconds.
		if !c.wsRateAllow() {
			atomic.AddInt64(&s.wsRateLimitExceeded, 1)
			logger.Warn("ws rate limit exceeded", "session_id", c.sessionID)
			// Send an error frame back to the client rather than silently dropping.
			select {
			case c.send <- WSMessage{
				Type:    "error",
				Content: "rate limit exceeded: too many messages, slow down",
			}:
			default:
			}
			continue
		}
		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		// Handle inbound messages
		s.handleWSMessage(c, msg)
	}
}

// parseBoolPayload converts a WebSocket payload value to bool.
// Handles native bool, JSON numbers (1/0), and strings ("true"/"false"/"1"/"0").
// Returns false for any unrecognised type.
func parseBoolPayload(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case float64:
		return val != 0
	case int:
		return val != 0
	case string:
		return val == "true" || val == "1"
	}
	return false
}

// payloadString safely extracts a string value from a payload map. Returns ""
// if the map is nil, the key is absent, or the value is nil — never "<nil>".
func payloadString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// logToolPermissionAudit writes denied tool-permission events to audit_log.
// It is intentionally no-op for non-denied tool events.
func logToolPermissionAudit(a *auditLogger, payload map[string]any) {
	if a == nil || payload == nil || !parseBoolPayload(payload["permission_denied"]) {
		return
	}
	toolName := payloadString(payload, "tool")
	if toolName == "" {
		toolName = "unknown_tool"
	}
	reasonCode := payloadString(payload, "reason_code")
	reasonText := payloadString(payload, "reason")
	reason := reasonText
	if reasonCode != "" {
		if reason != "" {
			reason = reasonCode + ": " + reason
		} else {
			reason = reasonCode
		}
	}
	if strings.TrimSpace(reason) == "" {
		reason = "permission denied"
	}
	a.Log("tool_permission", toolName, false, reason)
}

// streamEventToWS converts a backend.StreamEvent to a WSMessage.
// statusForToolCall derives a phase-true status message from a tool_call
// StreamEvent's payload ({"id", "tool", "args"} — see the chatOnToolCall /
// executeSingle wiring in internal/agent). Returns ("", false) for tool
// calls that don't map to a user-facing phase (most local tools), so the
// caller falls back to the last status already on the wire (default
// "thinking…"). This is intentionally a pure, ws.go-local translation —
// it reads the existing tool_call wire shape rather than requiring any
// change to the agent engine.
// newWSToolEventHandler builds the callback passed as ChatWithAgent's
// onToolEvent parameter. That parameter carries BOTH prefetch-phase calls
// (automatic muninn_recall / muninn_where_left_off / muninn_guide, run
// silently before the visible turn starts) and every regular in-loop tool
// call — agent_dispatcher.go's chatOnToolCall / chatOnToolDone route through
// onToolEvent instead of onEvent whenever onToolEvent is non-nil, for both
// kinds alike. Passing onToolEvent=nil (the previous behavior at every
// ChatWithAgent call site) silently drops prefetch events entirely —
// chatPrefetchCallback no-ops when onToolEvent is nil — so the client never
// saw "recalling memory" for automatic recall.
//
// Prefetch calls — marked by the producer with "phase": "prefetch" — surface
// only via setStatus("recalling memory"), no tool chip: the turn hasn't
// started yet, so a chip would appear before any assistant activity. The
// marker is set by the producer rather than inferred from the tool name
// because muninn_recall is ALSO a tool the agent calls deliberately mid-loop;
// a name-based test would swallow those real calls.
//
// Every other tool call is re-expressed as the same backend.StreamToolCall /
// StreamToolResult events onEvent already handles, so persistence
// (collectedToolCalls), the phase-true status derivation (statusForToolCall),
// and permission audit logging all run exactly as they did when these tools
// went through onEvent directly. The engine carries the LLM's real callID in
// the payload's "id", so tool_call/tool_result pair exactly even when tools
// run in parallel (see dispatchTools). The per-tool-name FIFO below is only a
// fallback for producers that send no id.
func newWSToolEventHandler(setStatus func(string), onEvent func(backend.StreamEvent)) func(eventType string, payload map[string]any) {
	var mu sync.Mutex
	pending := map[string][]string{}
	var nextID int64

	return func(eventType string, payload map[string]any) {
		toolName, _ := payload["tool"].(string)
		// Prefetch is identified by the producer-set phase marker, NOT by tool
		// name: muninn_recall is also a tool the agent calls deliberately
		// mid-loop, and matching on the name would swallow those real calls.
		if phase, _ := payload["phase"].(string); phase == "prefetch" {
			if eventType == "tool_call" {
				setStatus("recalling memory")
			}
			return
		}
		switch eventType {
		case "tool_call":
			id, _ := payload["id"].(string)
			if id == "" {
				// Fallback for producers that carry no callID: pair by
				// per-tool-name FIFO. Only correct while no two same-name
				// calls are in flight at once.
				mu.Lock()
				nextID++
				id = fmt.Sprintf("wsToolEvent-%d", nextID)
				pending[toolName] = append(pending[toolName], id)
				mu.Unlock()
			}
			args, _ := payload["args"].(map[string]any)
			onEvent(backend.StreamEvent{
				Type:    backend.StreamToolCall,
				Payload: map[string]any{"id": id, "tool": toolName, "args": args},
			})
		case "tool_result":
			id, _ := payload["id"].(string)
			if id == "" {
				mu.Lock()
				if ids := pending[toolName]; len(ids) > 0 {
					id = ids[0]
					pending[toolName] = ids[1:]
				}
				mu.Unlock()
			}
			permissionDenied, _ := payload["permission_denied"].(bool)
			// Prefer the producer's own success flag: a tool that failed with
			// an error but was NOT permission-denied must not report success.
			success := !permissionDenied
			if s, ok := payload["success"].(bool); ok {
				success = s && !permissionDenied
			}
			resultPayload := map[string]any{
				"id":      id,
				"tool":    toolName,
				"success": success,
				"result":  payload["result"],
			}
			if args, ok := payload["args"].(map[string]any); ok {
				resultPayload["args"] = args
			}
			if md, ok := payload["metadata"]; ok {
				resultPayload["metadata"] = md
			}
			if permissionDenied {
				resultPayload["permission_denied"] = true
				resultPayload["reason_code"] = payload["reason_code"]
				resultPayload["reason"] = payload["reason"]
			}
			onEvent(backend.StreamEvent{
				Type:    backend.StreamToolResult,
				Payload: resultPayload,
			})
		}
	}
}

func statusForToolCall(payload map[string]any) (string, bool) {
	tool, _ := payload["tool"].(string)
	switch tool {
	case "delegate_to_agent":
		if args, ok := payload["args"].(map[string]any); ok {
			if target, ok := args["agent"].(string); ok {
				if target = strings.TrimSpace(target); target != "" {
					return fmt.Sprintf("asking %s…", target), true
				}
			}
		}
		return "asking a teammate…", true
	case "muninn_where_left_off", "muninn_recall":
		return "recalling memory", true
	case "read_file":
		if name := argBasename(payload, "file_path"); name != "" {
			return fmt.Sprintf("reading %s…", name), true
		}
		return "reading a file…", true
	case "write_file":
		if name := argBasename(payload, "file_path"); name != "" {
			return fmt.Sprintf("writing %s…", name), true
		}
		return "writing a file…", true
	case "edit_file":
		if name := argBasename(payload, "file_path"); name != "" {
			return fmt.Sprintf("editing %s…", name), true
		}
		return "editing a file…", true
	case "bash":
		if args, ok := payload["args"].(map[string]any); ok {
			if cmd, ok := args["command"].(string); ok {
				if snippet := sanitizeCommandSnippet(cmd); snippet != "" {
					return fmt.Sprintf("running: %s…", snippet), true
				}
			}
		}
		return "running a command…", true
	case "list_dir":
		return "exploring files…", true
	default:
		return "", false
	}
}

// maxStatusBasename caps how much of a model-supplied path may reach the
// status line. Everything statusForToolCall interpolates is model/tool
// output, so it is bounded and scrubbed here rather than trusted.
const maxStatusBasename = 40

// sanitizeStatusText makes an arbitrary model-supplied string safe to
// interpolate into a one-line status message: C0/C1 control characters
// (newlines, NUL, ANSI escape introducers, BEL) become spaces, runs of
// whitespace collapse to one, and the result is truncated to maxLen runes
// (rune-wise, so multi-byte characters are never split). The client renders
// status as text ({{ latestRunStatus }} — no v-html), so this is about
// keeping the line one line and bounded, not about markup escaping.
func sanitizeStatusText(s string, maxLen int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	if r := []rune(s); len(r) > maxLen {
		return string(r[:maxLen])
	}
	return s
}

// argBasename extracts a string argument by key from a tool_call payload's
// "args" map and returns just its basename (e.g. "internal/tools/bash.go"
// -> "bash.go"), suitable for compact phase text. The basename is scrubbed
// and length-capped by sanitizeStatusText — a model can pass a path with
// embedded newlines or a 400-character filename, and neither may become the
// status line. Returns "" if the arg is missing, not a string, or blank.
func argBasename(payload map[string]any, argKey string) string {
	args, ok := payload["args"].(map[string]any)
	if !ok {
		return ""
	}
	v, ok := args[argKey].(string)
	if !ok {
		return ""
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	return sanitizeStatusText(filepath.Base(v), maxStatusBasename)
}

// sanitizeCommandSnippet collapses a shell command to a single line and
// truncates it to a short, safe-to-display prefix for status text (e.g.
// "go test ./...\nother" -> "go test ./..."). Returns "" for a blank command.
func sanitizeCommandSnippet(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	// Collapse to a single line: take everything up to the first newline,
	// then scrub remaining control characters (a command may carry ANSI
	// escapes or a BEL), collapse whitespace, and cap the length.
	if idx := strings.IndexByte(cmd, '\n'); idx >= 0 {
		cmd = cmd[:idx]
	}
	const maxLen = 30
	return sanitizeStatusText(cmd, maxLen)
}

func streamEventToWS(ev backend.StreamEvent, sessionID string) WSMessage {
	// Normalize streaming text and thought events to "token" so that the
	// frontend can use a single type to identify token stream messages.
	msgType := string(ev.Type)
	// StreamThought (extended thinking) is normalised to "token" so the frontend
	// renders it inline. StreamText is NOT normalised here — the onToken callback
	// already emits a "token" WS message for each text chunk; normalising StreamText
	// to "token" as well causes every word to appear twice (word-doubling bug, #30).
	if ev.Type == backend.StreamThought {
		msgType = "token"
	}
	return WSMessage{
		Type:      msgType,
		Content:   ev.Content,
		Payload:   ev.Payload,
		SessionID: sessionID,
	}
}

// resolveAgent loads the agent to use for a chat request. It is a convenience
// wrapper around resolveAgentForMessage for callers that don't have message
// content (e.g. non-chat paths). For the chat path use resolveAgentForMessage
// directly so that channel @mention routing is applied.
func (s *Server) resolveAgent(sessionID string) *agents.Agent {
	return s.resolveAgentForMessage(sessionID, "")
}

// resolveAgentForMessage loads the agent for a chat message, reading fresh from
// disk so that model changes made via the UI take effect immediately without a
// server restart.
//
// Resolution order:
//  1. User @mention addressee — the first @Name in the message that is
//     allowed to take this turn. When the space has a roster (DM = that
//     one agent; channel = lead+members), only roster names may be the
//     addressee. Standalone session-mode with no roster still matches any
//     known agent. This is stateless per-message and does NOT require
//     PrimaryAgent to be empty. A Chris-led channel with Manifest.Agent=Chris
//     still routes "@Steve do X" to Steve when Steve is on the roster.
//  2. Session's primary agent (set via "set_primary_agent" or stamped at
//     session-creation time from the space's lead agent)
//  3. Space lead agent — defence-in-depth for sessions created before
//     fix #33 or where space lookup failed at session creation.
//  4. First agent marked IsDefault in the config
//  5. First agent in the config (last resort)
//
// Returns nil only if no agents are configured or the config cannot be loaded,
// in which case callers should fall back to Orchestrator.Chat().
func (s *Server) resolveAgentForMessage(sessionID, content string) *agents.Agent {
	loader := s.agentLoader
	if loader == nil {
		loader = agents.LoadAgents
	}
	cfg, err := loader()
	if err != nil || len(cfg.Agents) == 0 {
		return nil
	}

	var loadedSess *session.Session
	if s.store != nil && sessionID != "" {
		if sess, loadErr := s.store.Load(sessionID); loadErr == nil {
			loadedSess = sess
		}
	}

	var memberNames []string
	var spaceLead string
	if s.spaceStore != nil && loadedSess != nil && loadedSess.Manifest.SpaceID != "" {
		if sp, spErr := s.spaceStore.GetSpace(loadedSess.Manifest.SpaceID); spErr == nil && sp != nil {
			spaceLead = sp.LeadAgent
			memberNames = spaceMemberNames(sp)
		}
	}

	// 1. User @mention addressee — wins over the stamped lead / primary agent.
	if content != "" {
		if ag := resolveMentionAddressee(content, cfg, memberNames); ag != nil {
			return ag
		}
	}

	// 2. Session primary agent
	if loadedSess != nil {
		if agentName := loadedSess.PrimaryAgentID(); agentName != "" {
			if ag := agentFromConfig(cfg, agentName); ag != nil {
				return ag
			}
			logger.Warn("resolveAgentForMessage: primary agent not found in config", "agent", agentName, "session_id", sessionID)
		}
	}

	// 3. Space lead agent
	if spaceLead != "" {
		if ag := agentFromConfig(cfg, spaceLead); ag != nil {
			return ag
		}
		if loadedSess != nil {
			logger.Warn("resolveAgentForMessage: space lead agent not found in config",
				"agent", spaceLead, "space_id", loadedSess.Manifest.SpaceID)
		}
	}

	// 4. Default agent
	for _, def := range cfg.Agents {
		if def.IsDefault {
			return agentFromDefWithVault(def)
		}
	}

	// 5. First agent
	return agentFromDefWithVault(cfg.Agents[0])
}

// spaceMemberNames returns the addressee roster for a space.
// DMs are 1:1 — only the DM agent. Channels are lead + members.
// An empty result means "no roster" (standalone / lookup failed) and
// mention routing may fall back to any known agent.
func spaceMemberNames(sp *spaces.Space) []string {
	if sp == nil {
		return nil
	}
	if sp.Kind == spaces.KindDM {
		if sp.LeadAgent == "" {
			return nil
		}
		return []string{sp.LeadAgent}
	}
	out := make([]string, 0, len(sp.Members)+1)
	if sp.LeadAgent != "" {
		out = append(out, sp.LeadAgent)
	}
	for _, m := range sp.Members {
		if !strings.EqualFold(m, sp.LeadAgent) {
			out = append(out, m)
		}
	}
	return out
}

func agentFromConfig(cfg *agents.AgentsConfig, name string) *agents.Agent {
	if cfg == nil || name == "" {
		return nil
	}
	for _, def := range cfg.Agents {
		if strings.EqualFold(def.Name, name) {
			return agentFromDefWithVault(def)
		}
	}
	return nil
}

// resolveMentionAddressee returns the agent addressed by the first @mention
// in content that is allowed to take this turn. When memberNames is
// non-empty (the space has a roster), only those names may be the
// addressee — a known agent who is not in the room does not win.
// When memberNames is empty (standalone session-mode), any known agent
// still matches. Mentions are walked in order so the first allowed @
// is the addressee.
func resolveMentionAddressee(content string, cfg *agents.AgentsConfig, memberNames []string) *agents.Agent {
	mentions := extractMentionNames(content)
	if len(mentions) == 0 || cfg == nil {
		return nil
	}
	restrictToRoster := len(memberNames) > 0
	members := make(map[string]bool, len(memberNames))
	for _, m := range memberNames {
		members[strings.ToLower(m)] = true
	}
	for _, name := range mentions {
		if restrictToRoster && !members[strings.ToLower(name)] {
			continue
		}
		if ag := agentFromConfig(cfg, name); ag != nil {
			return ag
		}
	}
	return nil
}

// additionalMentionNames returns @mentions after the addressee that may
// extra-spawn. The first allowed mention is the addressee and is omitted
// so CreateFromMentions can spawn threads for the rest. When memberNames
// is non-empty, extras are also restricted to the roster.
func additionalMentionNames(content, addressee string, cfg *agents.AgentsConfig, memberNames []string) []string {
	mentions := extractMentionNames(content)
	if len(mentions) == 0 {
		return nil
	}
	addr := resolveMentionAddressee(content, cfg, memberNames)
	addrName := addressee
	if addr != nil {
		addrName = addr.Name
	}
	restrictToRoster := len(memberNames) > 0
	members := make(map[string]bool, len(memberNames))
	for _, m := range memberNames {
		members[strings.ToLower(m)] = true
	}
	var extra []string
	seen := map[string]bool{}
	if addrName != "" {
		seen[strings.ToLower(addrName)] = true
	}
	for _, name := range mentions {
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		if restrictToRoster && !members[key] {
			continue
		}
		if ag := agentFromConfig(cfg, name); ag != nil {
			seen[key] = true
			extra = append(extra, ag.Name)
		}
	}
	// Hallway "@Winston Ask Steve hostname": Steve has no @, but he is
	// named on the roster. Extra-spawn him so the CoS is not the one
	// who runs bash. Standalone (no roster) stays @-only.
	if restrictToRoster {
		for _, name := range memberNames {
			key := strings.ToLower(name)
			if seen[key] {
				continue
			}
			if !spaces.NameAppears(content, name) {
				continue
			}
			if ag := agentFromConfig(cfg, name); ag != nil {
				seen[key] = true
				extra = append(extra, ag.Name)
			}
		}
	}
	return extra
}

// spawnAdditionalUserMentions fans additional user @mentions out through the
// existing CreateFromMentions path. The first matching @ is already the
// addressee for this turn; DedupMentions would strip those user-typed names
// from an assistant reply, so we pass only the leftover @names here.
func (s *Server) spawnAdditionalUserMentions(ctx context.Context, sessionID, userMsg, parentMsgID string, addressee *agents.Agent) {
	if s.mentionDelegate == nil || userMsg == "" {
		return
	}
	loader := s.agentLoader
	if loader == nil {
		loader = agents.LoadAgents
	}
	cfg, err := loader()
	if err != nil || cfg == nil {
		return
	}
	var memberNames []string
	if s.store != nil && sessionID != "" && s.spaceStore != nil {
		if sess, loadErr := s.store.Load(sessionID); loadErr == nil && sess.Manifest.SpaceID != "" {
			if sp, spErr := s.spaceStore.GetSpace(sess.Manifest.SpaceID); spErr == nil {
				memberNames = spaceMemberNames(sp)
			}
		}
	}
	addresseeName := ""
	if addressee != nil {
		addresseeName = addressee.Name
	}
	extras := additionalMentionNames(userMsg, addresseeName, cfg, memberNames)
	if len(extras) == 0 {
		return
	}
	var b strings.Builder
	for i, name := range extras {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteByte('@')
		b.WriteString(name)
	}
	b.WriteString(" ")
	b.WriteString(userMsg)
	// originalUserMsg is empty so DedupMentions will not strip the leftover
	// user-typed names we are deliberately spawning.
	s.mentionDelegate(ctx, sessionID, b.String(), "", parentMsgID)
}

// extractMentionNames returns every @Name token in content, in order.
// Valid agent names start with a letter and contain only letters, digits,
// hyphens, and underscores (max 64 chars).
func extractMentionNames(content string) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	var names []string
	seen := map[string]bool{}
	for i := 0; i < len(content); i++ {
		if content[i] != '@' {
			continue
		}
		if i > 0 && isAgentNameChar(content[i-1]) {
			// Email-style "alice@Bob" — not a mention.
			continue
		}
		rest := content[i+1:]
		if len(rest) == 0 || !isAgentNameStart(rest[0]) {
			continue
		}
		end := 1
		for end < len(rest) && isAgentNameChar(rest[end]) {
			end++
		}
		if end > 64 {
			continue
		}
		name := rest[:end]
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		names = append(names, name)
		i += end
	}
	return names
}

// extractLeadMention returns the first @mention in content. Kept as a thin
// wrapper so existing unit tests and call sites keep a single entry point.
func extractLeadMention(content string) string {
	names := extractMentionNames(content)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func isAgentNameStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isAgentNameChar(c byte) bool {
	return isAgentNameStart(c) || (c >= '0' && c <= '9') || c == '-' || c == '_'
}

// InjectSpaceContext builds and attaches space context (team roster, descriptions,
// delegation instructions) to the context so that ChatWithAgent can inject it into
// the system prompt. This is the critical wiring that makes agents aware of their
// team members and their capabilities in channel contexts.
//
// For non-space sessions or when the space store is not configured, the original
// context is returned unchanged.

// modelInfoFn resolves model IDs to capability info for roster cards.
func (s *Server) modelInfoFn() agents.ModelInfoFn {
	if s != nil && s.orch != nil {
		return s.orch.ModelInfoFn()
	}
	return agents.InferModelInfo
}

func (s *Server) capabilityCardMap(cfg *agents.AgentsConfig) map[string]string {
	cardMap := make(map[string]string)
	if cfg == nil {
		return cardMap
	}
	infoFn := s.modelInfoFn()
	for _, def := range cfg.Agents {
		cardMap[def.Name] = agents.BuildCapabilityCard(agents.CapabilityCardInput{
			Name:         def.Name,
			SystemPrompt: def.SystemPrompt,
			Description:  def.Description,
			ModelID:      def.Model,
			LocalTools:   def.LocalTools,
			Toolbelt:     def.Toolbelt,
			Skills:       def.Skills,
			MemoryMode:   def.MemoryMode,
		}, infoFn)
	}
	return cardMap
}

func (s *Server) appendDeskFloorContext(ctx context.Context, selfName string) context.Context {
	type deskFloor interface {
		DeskPeerNames() ([]string, error)
	}
	floor, ok := s.spaceStore.(deskFloor)
	if !ok {
		return ctx
	}
	peers, err := floor.DeskPeerNames()
	if err != nil || len(peers) == 0 {
		return ctx
	}
	loader := s.agentLoader
	if loader == nil {
		loader = agents.LoadAgents
	}
	cfg, cfgErr := loader()
	var cardMap map[string]string
	if cfgErr == nil {
		cardMap = s.capabilityCardMap(cfg)
	} else {
		cardMap = map[string]string{}
	}
	members := make([]agent.SpaceMember, 0, len(peers))
	for _, name := range peers {
		members = append(members, agent.SpaceMember{Name: name, Description: cardMap[name]})
	}
	block := agent.BuildDeskFloorContextBlock(selfName, members)
	if block == "" {
		return ctx
	}
	if existing := workforce.GetSpaceContext(ctx); existing != "" {
		block = existing + block
	}
	return workforce.WithSpaceContext(ctx, block)
}

func (s *Server) InjectSpaceContext(ctx context.Context, sessionID string, ag *agents.Agent) context.Context {
	if s.spaceStore == nil || s.store == nil || sessionID == "" {
		return ctx
	}
	sess, loadErr := s.store.Load(sessionID)
	if loadErr != nil || sess.SpaceID() == "" {
		return ctx
	}
	ctx = agent.SetSpaceID(ctx, sess.SpaceID())
	sp, spErr := s.spaceStore.GetSpace(sess.SpaceID())
	if spErr != nil || sp == nil {
		return ctx
	}

	// DMs get cross-space channel awareness so the lead agent knows about
	// channels they participate in and can delegate work to team members.
	if sp.Kind == spaces.KindDM {
		selfName := ""
		if ag != nil {
			selfName = ag.Name
		}
		if selfName != "" {
			channels, chErr := s.spaceStore.GetChannelsForAgent(selfName)
			if chErr == nil && len(channels) > 0 {
				// Load agent descriptions for all members.
				loader := s.agentLoader
				if loader == nil {
					loader = agents.LoadAgents
				}
				cfg, cfgErr := loader()
				var cardMap map[string]string
				if cfgErr == nil {
					cardMap = s.capabilityCardMap(cfg)
				} else {
					cardMap = map[string]string{}
				}
				var rosters []agent.ChannelRoster
				for _, ch := range channels {
					var members []agent.SpaceMember
					// Include lead agent
					members = append(members, agent.SpaceMember{
						Name: ch.LeadAgent, Description: cardMap[ch.LeadAgent],
					})
					for _, m := range ch.Members {
						if !strings.EqualFold(m, ch.LeadAgent) {
							members = append(members, agent.SpaceMember{
								Name: m, Description: cardMap[m],
							})
						}
					}
					rosters = append(rosters, agent.ChannelRoster{
						Name: ch.Name, LeadAgent: ch.LeadAgent, Members: members,
					})
				}
				block := agent.BuildDMCrossSpaceContextBlock(selfName, rosters)
				if block != "" {
					ctx = workforce.WithSpaceContext(ctx, block)
				}
			}
		}
		if spaces.IsDeskDM(sp) {
			ctx = s.appendDeskFloorContext(ctx, selfName)
		}
		return ctx
	}

	// Only channels with members get full team context.
	if sp.Kind != spaces.KindChannel || len(sp.Members) == 0 {
		return ctx
	}

	// Load agent descriptions from config for each member.
	loader := s.agentLoader
	if loader == nil {
		loader = agents.LoadAgents
	}
	cfg, cfgErr := loader()
	var cardMap map[string]string
	if cfgErr == nil {
		cardMap = s.capabilityCardMap(cfg)
	} else {
		cardMap = map[string]string{}
	}

	// Build SpaceMember list with descriptions.
	selfName := ""
	if ag != nil {
		selfName = ag.Name
	}
	var members []agent.SpaceMember
	for _, m := range sp.Members {
		members = append(members, agent.SpaceMember{
			Name:        m,
			Description: cardMap[m],
		})
	}
	// Include lead agent if not already in members list.
	leadInMembers := false
	for _, m := range sp.Members {
		if strings.EqualFold(m, sp.LeadAgent) {
			leadInMembers = true
			break
		}
	}
	if !leadInMembers {
		members = append([]agent.SpaceMember{{
			Name:        sp.LeadAgent,
			Description: cardMap[sp.LeadAgent],
		}}, members...)
	}

	block := agent.BuildSpaceContextBlock(sp.Name, sp.Kind, selfName, sp.LeadAgent, members)
	var channelNames []string
	for _, m := range members {
		if n := strings.TrimSpace(m.Name); n != "" {
			channelNames = append(channelNames, n)
		}
	}
	if len(channelNames) > 0 {
		ctx = workforce.WithChannelMembers(ctx, channelNames)
	}
	if name := s.lookupCompanyName(sp.CompanyID); name != "" {
		if block == "" {
			block = "\n\n[Team Context]\n"
		}
		block += "**Company:** " + name + "\nThis channel is in the " + name + " company. When asked what company this channel is in, name it.\n"
	}
	if rosters := s.companyRosterMap(); len(rosters) > 0 {
		ctx = workforce.WithCompanyRosters(ctx, rosters)
	}
	if block != "" {
		ctx = workforce.WithSpaceContext(ctx, block)
	}

	// Attach replication context so OnToolDone can fan out memory writes to all
	// channel members' vaults. cfg/cfgErr are already loaded above — reuse them.
	if cfgErr == nil && len(sp.Members) > 1 {
		username := memory.ResolveUsername("")
		var replMembers []workforce.ReplicationMember
		for _, memberName := range sp.Members {
			for _, def := range cfg.Agents {
				if strings.EqualFold(def.Name, memberName) {
					replMembers = append(replMembers, workforce.ReplicationMember{
						AgentName: def.Name,
						VaultName: def.ResolvedVaultName(username),
					})
					break
				}
			}
		}
		if len(replMembers) > 1 {
			ctx = workforce.WithReplicationContext(ctx, &workforce.MemReplicationContext{
				SpaceID:   sp.ID,
				SpaceName: sp.Name,
				Members:   replMembers,
			})
		}
	}

	// Build channel-recent summary from the last few messages.
	if msgResult, msgErr := s.spaceStore.ListSpaceMessages(sp.ID, nil, 15); msgErr == nil && len(msgResult.Messages) > 0 {
		var recentBuf strings.Builder
		recentBuf.WriteString("\n\n[Recent Channel Messages]\n")
		for _, m := range msgResult.Messages {
			speaker := m.Agent
			if speaker == "" {
				speaker = m.Role
			}
			content := m.Content
			if len(content) > 200 {
				content = content[:200] + "…"
			}
			fmt.Fprintf(&recentBuf, "**%s**: %s\n", speaker, content)
		}
		ctx = workforce.WithChannelRecent(ctx, recentBuf.String())
	}

	return ctx
}

// chatQueueBehindThreshold is the FIFO queue depth (admitted-but-not-finished
// chat runs for one session, this one included) past which runWSChat tells
// the user honestly that it is behind, rather than leaving a deep backlog to
// read as a hang. Messages are never dropped regardless of depth.
const chatQueueBehindThreshold = 5

// chatRunHandle tracks the context/cancel func of an in-flight or
// admitted-but-not-yet-started WS chat run so that an explicit "chat_cancel"
// message (or server shutdown) can stop it. Handles are compared by pointer
// identity so a finished run never deregisters a newer run that replaced it
// for the same session.
type chatRunHandle struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

// reserveChatRun admits a new chat run for sessionID into the session's
// strict FIFO queue and returns immediately (it never blocks). Admission
// order is the caller's call order — the WS read pump calls this
// synchronously, once per "chat" frame, before spawning that turn's
// goroutine, so admission order always matches wire arrival order rather
// than goroutine-scheduling order. The returned handle is registered as the
// session's active/queued run immediately; a run is NEVER cancelled just
// because a newer message was admitted behind it — supersede/cancel only
// happens via an explicit "chat_cancel" (cancelChatRun) or server shutdown.
//
// waitCh is the previous run's done channel (nil if none is queued ahead of
// this one) — callers MUST wait on it via awaitChatTurn before starting
// their turn's work, then MUST call endChatRun when finished. depth is the
// number of runs (including this one) currently admitted-but-not-finished
// for this session, so callers can give an honest "I'm behind" notice once
// a session's backlog grows large instead of ever dropping an ask.
func (s *Server) reserveChatRun(sessionID string) (run *chatRunHandle, waitCh <-chan struct{}, depth int) {
	base := s.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	run = &chatRunHandle{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	s.chatRunsMu.Lock()
	if s.chatRunCancels == nil {
		s.chatRunCancels = make(map[string]*chatRunHandle)
	}
	if s.chatQueueDepth == nil {
		s.chatQueueDepth = make(map[string]int)
	}
	prev := s.chatRunCancels[sessionID]
	s.chatRunCancels[sessionID] = run
	s.chatQueueDepth[sessionID]++
	depth = s.chatQueueDepth[sessionID]
	s.chatRunsMu.Unlock()
	if prev != nil {
		waitCh = prev.done
	}
	return run, waitCh, depth
}

// chatTurnWaitCeiling bounds a single awaitChatTurn wait for the prior
// FIFO turn. NOT a hard failure ceiling: a legitimately slow predecessor
// (a slow local model can take minutes) elapsing this ceiling only means
// this goroutine stops blocking here and falls through to its own
// session-level exclusive-run wait in ChatWithAgent, which retries past
// its own ceiling too — see runWSChat's isQueueBusy retry loop. Var (not
// const) so tests can shrink it to exercise that retry path quickly.
var chatTurnWaitCeiling = 120 * time.Second

// chatQueueRetryWait bounds each WaitForSessionIdle call in runWSChat's
// queue-busy retry loop (how long one re-enqueue attempt waits for the
// predecessor to free the session before looping again). Var so tests can
// shrink it to exercise many retry iterations quickly.
var chatQueueRetryWait = 30 * time.Second

// awaitChatTurn blocks until it is run's turn to execute: the prior run in
// the FIFO chain finished (waitCh closed), run's own context was cancelled
// (explicit chat_cancel / shutdown while queued), or a generous ceiling
// elapses so a stuck predecessor can never wedge the session forever.
// A nil waitCh (nothing queued ahead) returns immediately.
func (s *Server) awaitChatTurn(run *chatRunHandle, waitCh <-chan struct{}) {
	if waitCh == nil {
		return
	}
	select {
	case <-waitCh:
	case <-time.After(chatTurnWaitCeiling):
	case <-run.ctx.Done():
	}
}

// beginChatRun derives a connection-independent context for a chat run from
// the server lifecycle context (NOT the originating client's connection
// context), so a client disconnect or tab close no longer cancels an
// in-flight LLM run mid-stream. It reserves this session's next FIFO slot
// and immediately waits for it — the combination reserveChatRun +
// awaitChatTurn split out below is what lets the WS read pump reserve a
// slot synchronously (preserving wire arrival order) while waiting inside
// the run's own goroutine. Cancel via cancelChatRun, release via
// endChatRun.
func (s *Server) beginChatRun(sessionID, userMsg string) (context.Context, *chatRunHandle) {
	_ = userMsg // admission is unconditional FIFO now; kept for call-site compatibility.
	run, waitCh, _ := s.reserveChatRun(sessionID)
	s.awaitChatTurn(run, waitCh)
	return run.ctx, run
}

// endChatRun releases run's context resources and deregisters it if it is
// still the session's active run (a newer run may have replaced it).
func (s *Server) endChatRun(sessionID string, run *chatRunHandle) {
	s.chatRunsMu.Lock()
	if s.chatRunCancels[sessionID] == run {
		delete(s.chatRunCancels, sessionID)
	}
	if s.chatQueueDepth != nil {
		if s.chatQueueDepth[sessionID] > 0 {
			s.chatQueueDepth[sessionID]--
		}
		if s.chatQueueDepth[sessionID] <= 0 {
			delete(s.chatQueueDepth, sessionID)
		}
	}
	s.chatRunsMu.Unlock()
	if run.done != nil {
		select {
		case <-run.done:
		default:
			close(run.done)
		}
	}
	run.cancel()
}

// cancelChatRun cancels the active chat run for sessionID, if any.
// Returns true when a run was found and cancelled.
func (s *Server) cancelChatRun(sessionID string) bool {
	s.chatRunsMu.Lock()
	run := s.chatRunCancels[sessionID]
	delete(s.chatRunCancels, sessionID)
	s.chatRunsMu.Unlock()
	if run == nil {
		return false
	}
	run.cancel()
	return true
}

func (s *Server) handleWSMessage(c *wsClient, msg WSMessage) {
	switch msg.Type {
	case "chat":
		// Route to orchestrator. Always resolve the agent fresh from disk so
		// that model changes made via the UI take effect without a restart.
		if s.orch == nil {
			c.safeSend(WSMessage{Type: "error", Content: "orchestrator not initialized"})
			return
		}
		sessionID := msg.SessionID
		if sessionID == "" {
			sessionID = s.orch.SessionID()
		}
		// Echo run_id back so the client can correlate done/error events to the
		// specific run that triggered them and avoid stale-event mis-fires.
		runID := msg.RunID
		userMsg := msg.Content
		intent := ""
		if rawIntent, ok := msg.Payload["intent"].(string); ok {
			intent = strings.ToLower(strings.TrimSpace(rawIntent))
		}
		updateRoute := ""
		if rawRoute, ok := msg.Payload["update_route"].(string); ok {
			updateRoute = strings.ToLower(strings.TrimSpace(rawRoute))
		}
		targetAgent := ""
		if rawTarget, ok := msg.Payload["target_agent"].(string); ok {
			targetAgent = strings.TrimSpace(rawTarget)
		}
		// Reserve this turn's FIFO slot synchronously, on the read pump,
		// BEFORE spawning its goroutine. handleWSMessage is called once
		// per inbound frame from a single-threaded read loop (see
		// wsReadPump), so admission order here always matches wire
		// arrival order — a fast burst of messages can never race the Go
		// scheduler into running turns out of order.
		run, waitCh, depth := s.reserveChatRun(sessionID)
		go s.runWSChat(c, sessionID, userMsg, runID, intent, updateRoute, targetAgent, run, waitCh, depth)

	case "chat_cancel":
		// Explicit user-initiated cancellation of the session's in-flight chat
		// run. Since chat runs derive from the server lifecycle context (not the
		// connection), this is the only client-driven way to stop a run.
		sessionID := msg.SessionID
		if sessionID == "" {
			sessionID = payloadString(msg.Payload, "session_id")
		}
		if sessionID == "" {
			return
		}
		cancelled := s.cancelChatRun(sessionID)
		c.safeSend(WSMessage{
			Type:      "chat_cancel_result",
			SessionID: sessionID,
			Payload:   map[string]any{"cancelled": cancelled},
		})

	case "resume":
		// Client → server after a reconnect: replay session-scoped events the
		// client missed while disconnected (payload: {session_id, last_seq,
		// epoch}). Buffered messages with Seq > last_seq are re-sent in order,
		// followed by a "resume_ok" carrying the current seq and gap=true when
		// the replay buffer could not cover everything (client should re-fetch
		// history via REST).
		sessionID := msg.SessionID
		if sessionID == "" {
			sessionID = payloadString(msg.Payload, "session_id")
		}
		if sessionID == "" {
			return
		}
		var lastSeq uint64
		if v, ok := msg.Payload["last_seq"].(float64); ok && v > 0 {
			lastSeq = uint64(v)
		}
		var clientEpoch uint64
		if v, ok := msg.Payload["epoch"].(float64); ok && v > 0 {
			clientEpoch = uint64(v)
		}
		replayed, currentSeq, gap := s.wsHub.replaySince(sessionID, lastSeq)
		if clientEpoch != 0 && clientEpoch != serverEpoch {
			// Server restarted since the client last saw this session: seq
			// numbers are not comparable across epochs, so skip the replay and
			// tell the client to re-fetch history instead.
			replayed, gap = nil, true
		}
		for _, m := range replayed {
			if !c.safeSend(m) {
				return // client gone mid-replay
			}
		}
		c.safeSend(WSMessage{
			Type:      "resume_ok",
			SessionID: sessionID,
			Epoch:     serverEpoch,
			Payload: map[string]any{
				"seq":      currentSeq,
				"gap":      gap,
				"replayed": len(replayed),
			},
		})

	case "permission_response":
		// Client → server: resolves an in-flight "permission_request" prompt
		// emitted by PermissionPromptFunc. Payload: {id, scope} where scope is
		// "once" | "always_agent" | "deny" (legacy clients send {id, approved}).
		s.handlePermissionResponse(msg)

	case "ping":
		c.safeSend(WSMessage{Type: "pong"})

	case "thread_cancel":
		if s.tm == nil {
			return
		}
		threadID, _ := msg.Payload["thread_id"].(string)
		if threadID != "" {
			s.tm.Cancel(threadID)
		}

	case "thread_inject":
		if s.tm == nil {
			return
		}
		threadID, _ := msg.Payload["thread_id"].(string)
		content, _ := msg.Payload["content"].(string)
		if threadID == "" {
			return
		}
		sessionID := msg.SessionID
		if sessionID == "" {
			sessionID, _ = msg.Payload["session_id"].(string)
		}
		sent, found, reason := s.tm.TrySendInput(threadID, sessionID, content)
		if sent {
			deliveredTo, sharedWithActive, _ := s.tm.InjectReceipt(threadID)
			// Ack delivery.
			select {
			case c.send <- WSMessage{
				Type: "thread_inject_ack",
				Payload: map[string]any{
					"thread_id":          threadID,
					"delivered_to_agent": deliveredTo,
					"shared_with_active": sharedWithActive,
				},
			}:
			default:
			}
			return
		}
		if !found {
			reason = "not_found"
		}
		if reason == "" {
			reason = "not_waiting"
		}
		select {
		case c.send <- WSMessage{
			Type: "thread_inject_error",
			Payload: map[string]any{
				"thread_id": threadID,
				"reason":    reason,
			},
		}:
		default:
		}

	case "delegation_preview_ack":
		if s.previewGate == nil {
			return
		}
		threadID, _ := msg.Payload["thread_id"].(string)
		// parseBoolPayload handles bool, numeric (1/0), and string ("true"/"false")
		// representations so clients sending JSON-encoded numbers or strings still work.
		approved := parseBoolPayload(msg.Payload["approved"])
		sessionID := msg.SessionID
		if sessionID == "" {
			sessionID, _ = msg.Payload["session_id"].(string)
		}
		if threadID == "" || sessionID == "" {
			return
		}
		matched := s.previewGate.Ack(sessionID, threadID, approved)
		status := "accepted"
		if !matched {
			status = "not_found"
		}
		c.safeSend(WSMessage{
			Type:      "delegation_preview_ack_result",
			SessionID: sessionID,
			Payload: map[string]any{
				"thread_id": threadID,
				"approved":  approved,
				"status":    status,
			},
		})

	case "set_primary_agent":
		agentName, _ := msg.Payload["agent"].(string)
		sessionID := msg.SessionID
		if sessionID == "" {
			sessionID, _ = msg.Payload["session_id"].(string)
		}
		if agentName == "" || sessionID == "" || s.store == nil {
			return
		}
		// Load returns a fresh Session from disk. We mutate the copy and persist it.
		// Callers that need the updated primary agent must re-load from store.
		//
		// Trade-off: the load-mutate-save pattern is not atomic. A concurrent
		// set_primary_agent for the same session could race between Load and
		// SaveManifest and cause one update to be silently dropped. This is
		// an acceptable trade-off for the MVP because primary-agent changes are
		// infrequent (user-driven) and the last writer wins, which is safe.
		// A future improvement would be to use a per-session mutex in the store.
		sess, err := s.store.Load(sessionID)
		if err != nil {
			logger.Error("set_primary_agent: load session", "session_id", sessionID, "err", err)
			return
		}
		// Guard: DM spaces are strictly 1:1 — agent switching is not permitted.
		// Fail closed: if the space cannot be read, block the switch rather than
		// silently allowing it (consistent with the DM immutability principle).
		if sess.Manifest.SpaceID != "" && s.spaceStore != nil {
			sp, spErr := s.spaceStore.GetSpace(sess.Manifest.SpaceID)
			if spErr != nil {
				logger.Error("set_primary_agent: cannot verify space kind, blocking switch",
					"space_id", sess.Manifest.SpaceID, "err", spErr)
				c.safeSend(WSMessage{Type: "error", Content: "unable to verify space type"})
				return
			}
			if sp.Kind == spaces.KindDM {
				c.safeSend(WSMessage{Type: "error", Content: "cannot change agent in a DM"})
				return
			}
		}
		sess.SetPrimaryAgent(agentName)
		if err := s.store.SaveManifest(sess); err != nil {
			logger.Error("set_primary_agent: save manifest", "session_id", sessionID, "err", err)
			return
		}
		s.wsHub.broadcastToSession(sessionID, WSMessage{
			Type: "primary_agent_changed",
			Payload: map[string]any{
				"agent": agentName,
			},
		})
	}
}

// visibleTokenGate tracks the visible content actually sent to the client
// for one streaming run, so runWSChat's onToken can self-correct when a
// mid-stream rewrite changes a prefix that was already emitted (e.g. the
// harness clock label "Local time now: Friday, ...ET" only becomes "It's
// Friday, ...ET." once the stamp finishes streaming — see
// backend.stripHarnessClockLabel). Without this, a divergence (new visible
// content that is not a suffix-extension of what shipped) would either be
// silently dropped or have its emitted-so-far bookkeeping quietly
// overwritten as if the client had received it, permanently wedging the
// live bubble on stale partial text (huginn: hallway fast-path prefix
// drop). Emitted must only ever be updated to a value that was actually
// handed to emit().
type visibleTokenGate struct {
	emitted string
}

// next reports what to send for the newly computed visible content.
//   - visible == emitted: nothing changed, emit is false.
//   - visible extends emitted: delta is the plain append suffix.
//   - visible diverges from emitted (not a suffix-extension — a rewrite
//     changed earlier characters): delta is the full visible content and
//     replace is true, telling the client to repaint from scratch instead
//     of appending. This is the only correct way to fix already-emitted
//     characters over an append-only "token" stream.
func (g *visibleTokenGate) next(visible string) (delta string, replace bool, emit bool) {
	if visible == g.emitted {
		return "", false, false
	}
	if g.emitted == "" || strings.HasPrefix(visible, g.emitted) {
		delta = strings.TrimPrefix(visible, g.emitted)
		g.emitted = visible
		return delta, false, delta != ""
	}
	g.emitted = visible
	return visible, true, true
}

// runWSChat executes one chat turn for sessionID. It runs under a context
// derived from the server lifecycle (see beginChatRun), NOT the originating
// client's connection context, so the run survives client disconnects and tab
// closes. Streamed tokens/events are broadcast to every client subscribed to
// the session — including clients that reconnect mid-run — with a direct-send
// fallback to the originating client when it is not registered with the hub.
// Persistence of accumulated content runs on every exit path.
// run/waitCh/queueDepth come from a reserveChatRun call made synchronously
// on the WS read pump before this goroutine was spawned (see
// handleWSMessage's "chat" case), so this turn's FIFO position is fixed by
// wire arrival order before any of the work below (agent resolution, space
// context injection, LLM call) begins.
func (s *Server) runWSChat(c *wsClient, sessionID, userMsg, runID, intent, updateRoute, targetAgent string, run *chatRunHandle, waitCh <-chan struct{}, queueDepth int) {
	// assistantBuf accumulates response tokens for persistence after completion.
	var assistantBuf strings.Builder
	// collectedToolCalls accumulates tool results for persistence with the assistant message.
	var collectedToolCalls []session.PersistedToolCall
	// Pre-generate assistant message ID so done payload + persistence agree.
	assistantMsgID := session.NewID()

	ag := s.resolveAgentForMessage(sessionID, userMsg)
	agentName := ""
	if ag != nil {
		agentName = strings.TrimSpace(ag.Name)
	}

	// emit streams a run event to all clients subscribed to this session.
	emit := func(msg WSMessage) {
		if strings.TrimSpace(msg.Agent) == "" && agentName != "" {
			msg.Agent = agentName
		}
		s.wsHub.broadcastToSessionFrom(sessionID, msg, c)
	}
	var tokenGate visibleTokenGate
	// wroteFirstToken gates the one-shot "writing" status transition — fired
	// the moment the first visible token reaches the wire, so the UI moves
	// off "thinking…"/"asking …" the instant the reply actually starts.
	var wroteFirstToken bool
	// lastStatus keeps the heartbeat honest. The 15s keep-alive ticker below
	// re-emits a status so a long turn never reads as hung; without tracking
	// the current phase here it would re-emit a hardcoded "thinking" and
	// stomp a phase-true status ("asking Steve…") every 15 seconds during a
	// long delegation, flip-flopping the UI. Guarded because the heartbeat
	// runs on its own goroutine while setStatus runs on the run goroutine.
	var statusMu sync.Mutex
	lastStatus := "thinking"
	// updateLastStatus records the current phase without emitting — shared by
	// setStatus (tool-call-derived statuses) and onEvent's StreamStatus branch
	// below (plain engine statuses: "thinking", "hiring", "continuing",
	// model-loading) so the heartbeat always re-emits the true current phase
	// instead of a stale one.
	updateLastStatus := func(content string) {
		statusMu.Lock()
		lastStatus = content
		statusMu.Unlock()
	}
	setStatus := func(content string) {
		updateLastStatus(content)
		emit(WSMessage{Type: "status", Content: content, SessionID: sessionID})
	}
	currentStatus := func() string {
		statusMu.Lock()
		defer statusMu.Unlock()
		return lastStatus
	}
	onToken := func(token string) {
		assistantBuf.WriteString(token)
		raw := assistantBuf.String()
		if backend.PendingHarnessClockPrefix(raw) {
			return
		}
		visible := backend.VisibleAssistantContent(raw)
		if !backend.IsTimeAsk(userMsg) && backend.IsLeftoverClockSpeech(visible) {
			visible = ""
		}
		if !agent.IsTrivialPingAsk(userMsg) && backend.IsLeftoverPongSpeech(visible) {
			visible = ""
		}
		delta, replace, doEmit := tokenGate.next(visible)
		if !doEmit {
			return
		}
		if !wroteFirstToken {
			wroteFirstToken = true
			setStatus("writing")
		}
		msg := WSMessage{Type: "token", Content: delta}
		if replace {
			msg.Payload = map[string]any{"replace": true}
		}
		emit(msg)
	}
	onEvent := func(ev backend.StreamEvent) {
		// StreamDone is an internal backend signal. parseSSE emits it at the
		// end of each ChatCompletion; forwarding it as a client "done"
		// (no run_id) closes the space-timeline stream- row early, so a
		// leftover suffix lands as a nameless orphan bubble.
		if ev.Type == backend.StreamDone {
			return
		}
		// Plain StreamStatus events (agent_dispatcher's "thinking", hire_ask's
		// "hiring", the loop-nudge's "continuing", external.go's model-loading
		// status) previously went straight to emit without updating lastStatus,
		// so the 15s heartbeat below re-emitted the OLD phase and stomped these
		// within 15s. Route the content through the same tracking setStatus
		// uses, before emitting, so the heartbeat sees the true current phase.
		if ev.Type == backend.StreamStatus {
			updateLastStatus(ev.Content)
		}
		// Stamp the emitting agent onto tool_call/tool_result payloads (not
		// just the WSMessage-level Agent field emit() sets) so the client can
		// scope its live activeToolCalls ticker to the message it belongs to.
		// Without this, two agents streaming concurrently in one space share
		// one ticker and each bubble shows the other's tool calls. agentName
		// is this run's own agent (ag.Name), resolved once above — delegate
		// threads run their own RunLoop/onEvent closure with their own name,
		// so this never mixes agents within a single onEvent instance.
		if (ev.Type == backend.StreamToolCall || ev.Type == backend.StreamToolResult) && agentName != "" {
			if _, ok := ev.Payload["agent"]; !ok {
				// Copy rather than mutate ev.Payload in place: it may be the
				// same map instance the producer (agent_dispatcher, the
				// onToolEvent bridge) still holds a reference to.
				withAgent := make(map[string]any, len(ev.Payload)+1)
				for k, v := range ev.Payload {
					withAgent[k] = v
				}
				withAgent["agent"] = agentName
				ev.Payload = withAgent
			}
		}
		emit(streamEventToWS(ev, sessionID))
		// Phase-true status translation layer: a tool_call event already
		// carries the tool name and (for delegate_to_agent) the target
		// agent — derive a status line from it here rather than changing
		// the agent engine. See statusForToolCall.
		if ev.Type == backend.StreamToolCall {
			if statusContent, ok := statusForToolCall(ev.Payload); ok {
				setStatus(statusContent)
			}
		}
		// Capture tool results so they're persisted with the assistant message.
		if ev.Type == backend.StreamToolResult && ev.Payload != nil {
			tc := session.PersistedToolCall{
				ID:     payloadString(ev.Payload, "id"),
				Name:   payloadString(ev.Payload, "tool"),
				Result: payloadString(ev.Payload, "result"),
			}
			if args, ok := ev.Payload["args"].(map[string]any); ok {
				tc.Args = args
			}
			if md, ok := ev.Payload["metadata"].(map[string]any); ok {
				if diff, ok := md["diff"].(map[string]any); ok {
					tc.Diff = diff
				}
			}
			collectedToolCalls = append(collectedToolCalls, tc)
			logToolPermissionAudit(s.auditLog, ev.Payload)
		}
	}

	// onToolEvent handles ChatWithAgent's tool-event channel; see
	// newWSToolEventHandler's doc comment for why it exists and how it
	// avoids regressing regular (non-prefetch) tool events.
	onToolEvent := newWSToolEventHandler(setStatus, onEvent)

	// Emit thinking immediately so the 60s client watchdog does not fire
	// during context prep / model load (before the first token). Routed
	// through setStatus so the heartbeat's notion of the current phase and
	// the wire agree from the very first status.
	setStatus("thinking")

	// A deep FIFO backlog is queued, never dropped — but silence past a few
	// messages reads as broken, not busy. Say so honestly once the backlog
	// is more than chatQueueBehindThreshold turns deep (this one included).
	if queueDepth > chatQueueBehindThreshold {
		emit(WSMessage{
			Type:    "warning",
			Content: "I'm behind — answering in order.",
			RunID:   runID,
		})
	}

	// Hallway / desk-DM ChatWithAgent never went through wakeSpaceThreadAgent,
	// so space_reply_typing never fired and left-nav rows stayed still during
	// "Winston is responding… Preparing context…". Put thinking on the wire.
	if ag != nil && strings.TrimSpace(ag.Name) != "" {
		spaceID := s.sessionSpaceID(sessionID)
		s.emitAgentThinking(spaceID, sessionID, ag.Name, true)
		defer s.emitAgentThinking(spaceID, sessionID, ag.Name, false)
	}

	// Pre-generate the user message ID so the delegate_to_agent tool can
	// thread replies under it. Persist at accept so mid-turn Appends cannot
	// win seq before the prompt (reload would otherwise play cause after effect).
	userMsgID := session.NewID()
	userPersisted := s.persistInboundUserMessage(sessionID, userMsgID, userMsg)

	// First @ is the addressee (ag). Additional user @names spawn threads
	// via the existing mention/CreateFromMentions path.
	if s.mentionDelegate != nil {
		spawnCtx := s.Context()
		if spawnCtx == nil {
			spawnCtx = context.Background()
		}
		s.spawnAdditionalUserMentions(spawnCtx, sessionID, userMsg, userMsgID, ag)
	}

	// Wait for this session's FIFO turn (reserved synchronously by
	// handleWSMessage before this goroutine was spawned — see runWSChat's
	// doc comment). The run's context derives from the server lifetime, not
	// this client's connection, and is registered as the session's active
	// run so a "chat_cancel" message can stop it explicitly.
	s.awaitChatTurn(run, waitCh)
	chatCtx := run.ctx
	defer s.endChatRun(sessionID, run)

	// Build space context (channel team roster + descriptions) and inject
	// it into the context so BuildPersonaPromptWithMemory can pick it up.
	// This is the critical wiring that makes the lead agent aware of its
	// team members and their capabilities for intelligent delegation.
	chatCtx = s.InjectSpaceContext(chatCtx, sessionID, ag)
	chatCtx = agent.SetParentMessageID(chatCtx, userMsgID)
	// Set calling agent so DelegateFn can record the delegation's FromAgent.
	if ag != nil {
		chatCtx = threadmgr.SetCallingAgent(chatCtx, ag.Name)
	}

	runChat := func() error {
		if ag != nil {
			return s.orch.ChatWithAgent(chatCtx, ag, userMsg, sessionID, onToken, onToolEvent, onEvent)
		}
		// No agents configured — fall back to generic Chat.
		return s.orch.Chat(chatCtx, userMsg, onToken, onEvent)
	}

	// wsHeartbeatInterval is a var (not a const) purely so tests can shorten
	// it; production behaviour is unchanged at 15s.
	// Heartbeat: DEFECT A made hallway turns look dead for tens of seconds
	// while runChat is in flight but nothing has streamed yet (or a whole
	// terminal turn was gated and only flushes at the very end). Keep the
	// "thinking" status alive on the wire so the UI never reads as hung.
	heartbeatDone := make(chan struct{})
	// Read wsHeartbeatInterval synchronously here, before spawning the
	// goroutine below, and capture it in heartbeatInterval. The goroutine
	// itself may not get scheduled until well after this function (and even
	// the whole test) returns, so a direct `wsHeartbeatInterval` read inside
	// it races against the next test's `wsHeartbeatInterval = ...` under
	// `go test -race` even though heartbeatDone has already been closed.
	heartbeatInterval := wsHeartbeatInterval
	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatDone:
				return
			case <-ticker.C:
				// Re-emit the CURRENT phase, not a hardcoded "thinking" —
				// otherwise the keep-alive stomps "asking Steve…"/"writing".
				emit(WSMessage{Type: "status", Content: currentStatus(), SessionID: sessionID})
			}
		}
	}()

	// isQueueBusy reports a queue-wait-ceiling error from ChatWithAgent —
	// the predecessor turn is still legitimately running (a slow local
	// model can take minutes), not a real failure. errors.Is is the
	// primary check; the string fallback covers any legacy caller that
	// still returns the old "already running" text. This class of error
	// must NEVER reach the err != nil branch below (which would persist it
	// as "⚠️ <err>" assistant speech, a Tier-0 violation) — the loop below
	// keeps re-enqueuing until it succeeds or the run is truly cancelled.
	isQueueBusy := func(e error) bool {
		if e == nil {
			return false
		}
		return errors.Is(e, agent.ErrQueueWaitTimeout) ||
			strings.Contains(e.Error(), "already running") ||
			strings.Contains(e.Error(), "still busy after queue wait")
	}

	err := runChat()
	for queueRetries := 0; isQueueBusy(err); queueRetries++ {
		warnContent := "Another response is still finishing — queued your message and retrying."
		if queueRetries == 0 && intent == "update_active_work" && s.tm != nil {
			switch updateRoute {
			case "lead_only":
				warnContent = "Queued a lead follow-up to your update."
			case "specific_delegate":
				if targetAgent != "" {
					activeDelegates := s.tm.PublishSessionGuidanceTarget(sessionID, "operator", targetAgent, "main-channel update: "+userMsg)
					if activeDelegates > 0 {
						warnContent = fmt.Sprintf("Shared your update with @%s and queued a lead follow-up.", targetAgent)
					} else {
						warnContent = fmt.Sprintf("No active delegate matched @%s — queued a lead follow-up.", targetAgent)
					}
					break
				}
				fallthrough
			case "", "all_active":
				activeDelegates := s.tm.PublishSessionGuidance(sessionID, "operator", "main-channel update: "+userMsg)
				if activeDelegates > 0 {
					if activeDelegates == 1 {
						warnContent = "Shared your update with 1 active delegate and queued a lead follow-up."
					} else {
						warnContent = fmt.Sprintf("Shared your update with %d active delegates and queued a lead follow-up.", activeDelegates)
					}
				}
			default:
				warnContent = "Queued a lead follow-up to your update."
			}
		} else if queueRetries > 0 {
			// Re-enqueue: the backlog is deeper than one 30s wait — say so
			// honestly instead of ever giving up and speaking the raw
			// queue-wait error. Throttled so a long backlog does not spam
			// the client with a warning every 30s.
			if queueRetries%4 == 0 {
				warnContent = fmt.Sprintf("Still working through the backlog — %s is queued.", strings.TrimSpace(userMsg))
			} else {
				warnContent = ""
			}
		}
		// Slack-like behavior: treat quick follow-up user messages as queued
		// guidance when another run is still winding down.
		if warnContent != "" {
			emit(WSMessage{
				Type:    "warning",
				Content: warnContent,
				RunID:   runID,
			})
		}
		// chatCtx derives from the server lifetime, not this ceiling — only
		// an explicit chat_cancel or server shutdown ends the re-enqueue
		// loop early. A merely slow predecessor is not a reason to give up.
		if chatCtx.Err() != nil {
			break
		}
		waitCtx, waitCancel := context.WithTimeout(chatCtx, chatQueueRetryWait)
		s.orch.WaitForSessionIdle(sessionID, waitCtx)
		waitCancel()
		err = runChat()
	}
	close(heartbeatDone)
	// persistAccumulated saves whatever assistant content/tool-calls have
	// been accumulated so far. The inbound user row is written at accept;
	// this is the fallback if that write failed. Called on success (done),
	// cancellation (chat_cancel / server shutdown), and real errors — it
	// must run regardless of client disconnects.
	persistAccumulated := func(errContent string) {
		if s.store == nil || sessionID == "" {
			return
		}
		// Bind persist to THIS inbound user row — never a leftover ping ask
		// or leftover assistant stream from an earlier turn in the session.
		// This run is never superseded by a fast-follow message anymore
		// (reserveChatRun always queues FIFO, it never cancels an earlier
		// run) — the only way chatCtx.Err() is non-nil here is an explicit
		// chat_cancel or server shutdown, handled below.
		thisTurnAsk := userMsg
		thisTurnUserID := userMsgID
		sess, loadErr := s.store.Load(sessionID)
		if loadErr != nil {
			return
		}
		latestUserID, latestUserAsk := thisTurnUserID, thisTurnAsk
		if tailer, ok := s.store.(interface {
			TailMessages(string, int) ([]session.SessionMessage, error)
		}); ok {
			if tail, err := tailer.TailMessages(sessionID, 24); err == nil {
				for i := len(tail) - 1; i >= 0; i-- {
					if tail[i].Role == "user" {
						latestUserID, latestUserAsk = tail[i].ID, tail[i].Content
						break
					}
				}
			}
		}
		// Compute persist first so a cancelled ping/headcount still keeps
		// its harness fill. Leftover-clock-only stays empty and is skipped.
		// BindPersistToThisTurn already drops a leftover-Pong-only stream
		// when a later, non-queued-ping user row is already the tail (a
		// cancelled ping followed by a real ask) — real content it returns
		// always persists here, whether or not this run was cancelled, per
		// this function's contract: it must run regardless of client
		// disconnects, cancellation, or real errors.
		early, write := backend.BindPersistToThisTurn(thisTurnUserID, thisTurnAsk, latestUserID, latestUserAsk, assistantBuf.String())
		if !write && errContent == "" {
			return
		}
		if early == "" {
			early = s.fillEmptyHarnessPersist(early, thisTurnAsk, sess)
		}
		agentName := ""
		if ag != nil {
			agentName = ag.Name
		}
		if !userPersisted {
			if appendErr := s.store.Append(sess, session.SessionMessage{
				ID: thisTurnUserID, Role: "user", Content: thisTurnAsk, Ts: time.Now().UTC(),
			}); appendErr != nil {
				logger.Error("ws chat: failed to persist user message", "session_id", sessionID, "err", appendErr)
			} else {
				userPersisted = true
			}
		}
		// When we have sayable leftover (or leftover-empty teammate rewrite)
		// or tool calls, persist them. Never persist an empty leftover-only row.
		persistedContent := early
		if persistedContent == "" {
			persistedContent, _ = backend.BindPersistToThisTurn(thisTurnUserID, thisTurnAsk, latestUserID, latestUserAsk, assistantBuf.String())
		}
		if persistedContent == "" {
			persistedContent = s.fillEmptyHarnessPersist(persistedContent, thisTurnAsk, sess)
		}
		if persistedContent != "" || len(collectedToolCalls) > 0 {
			// Turn is over: leftover deny/helpdesk speech is residue even
			// when toolsCalled is empty (live Lab Winston Steve-deny).
			assistantMsg := session.SessionMessage{
				ID:        assistantMsgID,
				Role:      "assistant",
				Content:   persistedContent,
				Agent:     agentName,
				Ts:        time.Now().UTC(),
				ToolCalls: collectedToolCalls,
			}
			s.applyKnownUsage(&assistantMsg, sessionID, persistModelName(sess, ag))
			if appendErr := s.store.Append(sess, assistantMsg); appendErr != nil {
				logger.Error("ws chat: failed to persist assistant message", "session_id", sessionID, "err", appendErr)
			}
		} else if errContent != "" {
			// No accumulated content but there was an error — persist
			// teammate speech, never a raw Go keyring dump.
			_ = s.store.Append(sess, session.SessionMessage{
				ID: session.NewID(), Role: "assistant",
				Content: errContent,
				Agent:   agentName,
				Ts:      time.Now().UTC(),
			})
		}
		s.emitSpaceActivity(sess.SpaceID())
	}

	if err != nil {
		// Distinguish explicit run cancellation (chat_cancel message or server
		// shutdown) from real backend errors. Client disconnects no longer
		// cancel the run — chatCtx derives from the server lifecycle, not the
		// connection. Backend timeouts or API errors are still reported as
		// errors even if they wrap context.DeadlineExceeded.
		isCancelled := chatCtx.Err() != nil

		if isCancelled {
			logger.Info("ws chat: run cancelled mid-stream, persisting accumulated content",
				"session_id", sessionID, "buf_len", assistantBuf.Len(), "tool_calls", len(collectedToolCalls))
			// Persist BEFORE signaling completion: a client (or test) that acts
			// on "done" the instant it arrives — e.g. re-fetching history, or a
			// test tearing down its temp session dir — must never race the
			// store.Append writes below. persistAccumulated only reads state
			// already captured in this closure, so computing it first changes
			// nothing about what gets persisted, only when the wire is told.
			persistAccumulated("")
			// Signal completion so any connected client stops its spinner.
			emit(WSMessage{Type: "done", RunID: runID, Payload: map[string]any{
				"message_id": assistantMsgID,
				"cancelled":  true,
			}})
		} else {
			logger.Error("chat completion", "session_id", sessionID, "err", err)
			agentName := ""
			if ag != nil {
				agentName = ag.Name
			}
			errSpeech := err.Error()
			if backend.IsKeyMiss(err) {
				errSpeech = backend.PersistKeyMissSpeech(agentName, "", err)
			} else {
				errSpeech = "⚠️ " + err.Error()
			}
			persistAccumulated(errSpeech)
			emit(WSMessage{Type: "error", Content: errSpeech, RunID: runID})
		}
	} else {
		// Persist assistant content BEFORE emitting "done" — see the isCancelled
		// branch above for why ordering here matters, not just style. The user
		// row was written at accept so mid-turn announcements keep chronological
		// seq. Also emits space_activity for unseen badges.
		persistAccumulated("")

		emit(WSMessage{Type: "done", RunID: runID, Payload: map[string]any{
			"message_id": assistantMsgID,
		}})
	}

	logger.Info("ws chat done",
		"session_id", sessionID,
		"assistant_response_len", assistantBuf.Len(),
		"had_error", err != nil,
	)
}

// wsHeartbeatInterval is how often runWSChat re-emits the current status so a
// long turn never reads as hung. A var rather than a const so tests can shorten
// it; production behaviour is unchanged.
var wsHeartbeatInterval = 15 * time.Second
