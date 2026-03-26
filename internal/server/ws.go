package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/logger"
)

// serverEpoch is a random uint64 generated at process startup. It is stamped
// on every session-scoped WebSocket message so that clients can detect server
// restarts and reset their sequence-number state.
var serverEpoch uint64

func init() {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		serverEpoch = binary.LittleEndian.Uint64(b[:])
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

// WSHub manages all active WebSocket client connections.
type WSHub struct {
	clients    map[*wsClient]struct{}
	mu         sync.RWMutex
	broadcastC chan WSMessage
	stopC      chan struct{}
	stopOnce   sync.Once  // ensures stop() is idempotent
	stopped    int32      // atomic: 1 once stop() has been called
	// seqMu guards sessionSeq. We use a separate mutex so broadcastToSession
	// can hold the RLock on mu (for clients) while atomically incrementing the
	// per-session sequence counter.
	seqMu            sync.Mutex
	sessionSeq       map[string]uint64
	wsDroppedMessages atomic.Int64
}

func newWSHub() *WSHub {
	return &WSHub{
		clients:    make(map[*wsClient]struct{}),
		broadcastC: make(chan WSMessage, 256),
		stopC:      make(chan struct{}),
		sessionSeq: make(map[string]uint64),
	}
}

func (h *WSHub) run() {
	for {
		select {
		case <-h.stopC:
			return
		case msg := <-h.broadcastC:
			h.mu.RLock()
			for c := range h.clients {
				select {
				case c.send <- msg:
					atomic.StoreInt32(&c.consecutiveDrops, 0)
				default:
					// Slow client — buffer full, message dropped.
					h.wsDroppedMessages.Add(1)
					drops := atomic.AddInt32(&c.consecutiveDrops, 1)
					if drops == wsMaxDrops {
						slog.Error("ws: slow client evicted after repeated drops",
							"session_id", c.sessionID,
							"drops", drops,
							"total_dropped", h.wsDroppedMessages.Load())
						go func(evict *wsClient) {
							_ = evict.conn.WriteControl(websocket.CloseMessage,
								websocket.FormatCloseMessage(4002, "slow_client_eviction"),
								time.Now().Add(wsWriteWait))
							h.unregisterClient(evict)
						}(c)
					} else if drops < wsMaxDrops {
						slog.Warn("ws: slow client, message dropped",
							"session_id", c.sessionID,
							"consecutive_drops", drops,
							"total_dropped", h.wsDroppedMessages.Load())
					}
				}
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

func (h *WSHub) broadcast(msg WSMessage) {
	h.broadcastC <- msg
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
// a monotonically increasing Seq automatically.
func (h *WSHub) broadcastToSession(sessionID string, msg WSMessage) {
	msg.SessionID = sessionID
	// Stamp the process-level epoch and a per-session monotonic sequence number.
	msg.Epoch = serverEpoch
	h.seqMu.Lock()
	h.sessionSeq[sessionID]++
	msg.Seq = h.sessionSeq[sessionID]
	h.seqMu.Unlock()
	h.mu.RLock()
	for c := range h.clients {
		if c.sessionID == "" || c.sessionID == sessionID {
			select {
			case c.send <- msg:
				atomic.StoreInt32(&c.consecutiveDrops, 0)
			default:
				// Slow client — buffer full, message dropped.
				h.wsDroppedMessages.Add(1)
				drops := atomic.AddInt32(&c.consecutiveDrops, 1)
				if drops == wsMaxDrops {
					slog.Error("ws: slow client evicted after repeated session drops",
						"session_id", sessionID,
						"msg_type", msg.Type,
						"drops", drops,
						"total_dropped", h.wsDroppedMessages.Load())
					go func(evict *wsClient) {
						_ = evict.conn.WriteControl(websocket.CloseMessage,
							websocket.FormatCloseMessage(4002, "slow_client_eviction"),
							time.Now().Add(wsWriteWait))
						h.unregisterClient(evict)
					}(c)
				} else if drops < wsMaxDrops {
					slog.Warn("ws: slow client, session message dropped",
						"session_id", sessionID,
						"msg_type", msg.Type,
						"consecutive_drops", drops,
						"total_dropped", h.wsDroppedMessages.Load())
				}
			}
		}
	}
	h.mu.RUnlock()
}

// WSDroppedMessages returns the total count of messages dropped due to slow
// client send buffers being full. Monotonically increasing.
func (h *WSHub) WSDroppedMessages() int64 {
	return h.wsDroppedMessages.Load()
}

// unregisterClient synchronously removes a client from the hub, cancels its
// per-connection context (which propagates to any in-flight chat goroutines),
// and closes its send channel. It is safe to call from any goroutine.
func (h *WSHub) unregisterClient(c *wsClient) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	func() {
		defer func() { recover() }() //nolint:errcheck // intentional: close only once
		close(c.send)
	}()
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
	// Validate token via query param (WebSocket upgrades can't set headers from browser).
	// Use constant-time comparison to prevent timing-based token oracle attacks.
	tok := r.URL.Query().Get("token")
	if subtle.ConstantTimeCompare([]byte(tok), []byte(s.token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
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
	for msg := range c.send {
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

// streamEventToWS converts a backend.StreamEvent to a WSMessage.
func streamEventToWS(ev backend.StreamEvent, sessionID string) WSMessage {
	// Normalize streaming text and thought events to "token" so that the
	// frontend can use a single type to identify token stream messages.
	msgType := string(ev.Type)
	if ev.Type == backend.StreamText || ev.Type == backend.StreamThought {
		msgType = "token"
	}
	return WSMessage{
		Type:      msgType,
		Content:   ev.Content,
		Payload:   ev.Payload,
		SessionID: sessionID,
	}
}

// resolveAgent loads the agent to use for a chat request, reading fresh from
// disk so that model changes made via the UI take effect immediately without a
// server restart.
//
// Resolution order:
//  1. Session's primary agent (set via "set_primary_agent" WS message)
//  2. First agent marked IsDefault in the config
//  3. First agent in the config (last resort)
//
// Returns nil only if no agents are configured or the config cannot be loaded,
// in which case callers should fall back to Orchestrator.Chat().
func (s *Server) resolveAgent(sessionID string) *agents.Agent {
	loader := s.agentLoader
	if loader == nil {
		loader = agents.LoadAgents
	}
	cfg, err := loader()
	if err != nil || len(cfg.Agents) == 0 {
		return nil
	}

	// 1. Session primary agent
	if s.store != nil && sessionID != "" {
		if sess, loadErr := s.store.Load(sessionID); loadErr == nil {
			if agentName := sess.PrimaryAgentID(); agentName != "" {
				for _, def := range cfg.Agents {
					if strings.EqualFold(def.Name, agentName) {
						return agents.FromDef(def)
					}
				}
				// Primary agent name saved but not found in config — log and fall through.
				logger.Warn("resolveAgent: primary agent not found in config", "agent", agentName, "session_id", sessionID)
			}
		}
	}

	// 2. Default agent
	for _, def := range cfg.Agents {
		if def.IsDefault {
			return agents.FromDef(def)
		}
	}

	// 3. First agent
	return agents.FromDef(cfg.Agents[0])
}

func (s *Server) handleWSMessage(c *wsClient, msg WSMessage) {
	switch msg.Type {
	case "chat":
		// Route to orchestrator. Always resolve the agent fresh from disk so
		// that model changes made via the UI take effect without a restart.
		if s.orch == nil {
			c.send <- WSMessage{Type: "error", Content: "orchestrator not initialized"}
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
		// Snapshot mentionDelegate under the lock so the goroutine doesn't race.
		s.mu.Lock()
		mentionDelegate := s.mentionDelegate
		s.mu.Unlock()
		go func(runID string) {
			onToken := func(token string) {
				c.send <- WSMessage{Type: "token", Content: token, SessionID: sessionID}
			}
			onEvent := func(ev backend.StreamEvent) {
				c.send <- streamEventToWS(ev, sessionID)
			}

			var err error
			if ag := s.resolveAgent(sessionID); ag != nil {
				err = s.orch.ChatWithAgent(c.ctx, ag, userMsg, sessionID, onToken, nil, onEvent)
			} else {
				// No agents configured — fall back to generic Chat.
				err = s.orch.Chat(c.ctx, userMsg, onToken, onEvent)
			}
			if err != nil {
				logger.Error("chat completion", "session_id", sessionID, "err", err)
				c.send <- WSMessage{Type: "error", Content: err.Error(), SessionID: sessionID, RunID: runID}
				return
			}
			c.send <- WSMessage{Type: "done", SessionID: sessionID, RunID: runID}

			// Parse @AgentName mentions and spawn threads for any matched agents.
			// This is the fallback delegation path for models that don't support tools.
			logger.Info("ws chat done", "session_id", sessionID, "mentionDelegate_set", mentionDelegate != nil, "user_msg", userMsg)
			if mentionDelegate != nil {
				mentionDelegate(c.ctx, sessionID, userMsg, "")
			}
			// Emit space_activity so all connected browser tabs update unseen badges.
			if s.store != nil && sessionID != "" {
				if sess, loadErr := s.store.Load(sessionID); loadErr == nil {
					s.emitSpaceActivity(sess.SpaceID())
				}
			}
		}(runID)
	case "ping":
		c.send <- WSMessage{Type: "pong"}

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
		if ch, ok := s.tm.GetInputCh(threadID); ok && ch != nil {
			select {
			case ch <- content:
				// Ack delivery.
				select {
				case c.send <- WSMessage{
					Type:    "thread_inject_ack",
					Payload: map[string]any{"thread_id": threadID},
				}:
				default:
				}
			default:
				// InputCh buffer full — notify the caller.
				select {
				case c.send <- WSMessage{
					Type: "thread_inject_error",
					Payload: map[string]any{
						"thread_id": threadID,
						"reason":    "buffer_full",
					},
				}:
				default:
				}
			}
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
		s.previewGate.Ack(sessionID, threadID, approved)

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
