package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/scrypster/huginn/internal/backend"
)

// ChatIdleTimeout is the maximum time a chat or run-agent session can go
// without emitting a token before the session context is cancelled.
// Configurable so tests can shrink it without touching production defaults.
var ChatIdleTimeout = 5 * time.Minute

// tokenPumpSize is the capacity of the per-session non-blocking token channel.
const tokenPumpSize = 1024

// isTerminalMessageType returns true for messages whose loss would prevent the
// remote side from knowing a session has ended.
func isTerminalMessageType(mt MessageType) bool {
	switch mt {
	case MsgDone, MsgAgentResult:
		return true
	default:
		return false
	}
}

// ModelInfo describes a single model available from a provider.
type ModelInfo struct {
	Name         string `json:"name"`
	Size         string `json:"size,omitempty"`
	Quantization string `json:"quantization,omitempty"`
}

// ModelProviderInfo describes one LLM provider and its available models.
type ModelProviderInfo struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Endpoint  string      `json:"endpoint"`
	APIKey    string      `json:"api_key"`
	Connected bool        `json:"connected"`
	Models    []ModelInfo `json:"models"`
}

// DispatcherConfig holds all dependencies for NewDispatcher.
// Phase 3 fields are nil-safe: when nil, the corresponding handler is disabled.
type DispatcherConfig struct {
	MachineID   string
	DeliverPerm func(requestID string, approved bool) bool
	Hub         Hub
	Store       *SessionStore
	Outbox      *Outbox // optional; when set, failed hub.Send calls are queued here for retry

	// DroppedMessages is incremented atomically whenever both hub.Send and
	// outbox.Enqueue fail (message is unrecoverably lost). Optional; nil = not tracked.
	DroppedMessages *atomic.Uint64

	// Phase 3 — nil disables handler
	ChatSession func(ctx context.Context, sessionID, userMsg string,
		onToken func(string),
		onToolEvent func(eventType string, payload map[string]any),
		onEvent func(backend.StreamEvent)) error
	NewSession func(id string) string // returns actual sessionID used
	ListModels func() []string
	Active     *ActiveSessions // nil = cancel not tracked

	// Shell handles PTY terminal messages (shell_start, shell_input, shell_resize, shell_exit).
	// When nil, shell messages are ignored with a warning.
	Shell *ShellManager

	// HTTPProxy proxies http_request messages to the satellite's local REST API.
	// method, path, body come from the relay message payload.
	// Returns the HTTP status code and response body to send back as http_response.
	// When nil, http_request messages are ignored with a warning.
	HTTPProxy func(method, path string, body []byte) (status int, responseBody []byte, err error)

	// RunAgent executes a named agent with the given prompt and streams results.
	// onToken is called for each streamed token. When nil, MsgRunAgent messages
	// are rejected with an error MsgAgentResult sent back to the hub.
	RunAgent func(ctx context.Context, agentName, prompt, sessionID string, onToken func(string)) error

	// Model provider config callbacks — nil disables handler (logs warning)
	GetModelProviders func() []ModelProviderInfo
	GetModelConfig    func(provider string) (*ModelProviderInfo, error)
	UpdateModelConfig func(provider, endpoint, apiKey string) error
	PullModel         func(name string) error
}

// safeFloat extracts a float64 from a map[string]any value, returning
// fallback if the value is missing or not a number.
func safeFloat(v any, fallback float64) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return fallback
}

// ActiveSessions tracks goroutines spawned by MsgChatMessage.
// Uses a generation counter so Remove only removes the entry it actually created,
// preventing a race when Start cancels-and-replaces a running session.
type ActiveSessions struct {
	mu      sync.Mutex
	active  map[string]sessionEntry
	nextGen uint64
}

type sessionEntry struct {
	cancel context.CancelFunc
	gen    uint64
}

// NewActiveSessions creates a new ActiveSessions tracker.
func NewActiveSessions() *ActiveSessions {
	return &ActiveSessions{active: make(map[string]sessionEntry)}
}

// Start registers cancel for sessionID. If a session is already active it is
// cancelled before the new one is registered. Returns the generation token that
// must be passed to Remove, and whether a previous session was replaced.
func (a *ActiveSessions) Start(sessionID string, cancel context.CancelFunc) (gen uint64, replaced bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if old, ok := a.active[sessionID]; ok {
		old.cancel()
		replaced = true
	}
	a.nextGen++
	a.active[sessionID] = sessionEntry{cancel: cancel, gen: a.nextGen}
	return a.nextGen, replaced
}

// Cancel cancels a session by ID and removes it. Returns true if found.
func (a *ActiveSessions) Cancel(sessionID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	entry, ok := a.active[sessionID]
	if ok {
		entry.cancel()
		delete(a.active, sessionID)
	}
	return ok
}

// Remove removes the session only if its generation matches gen.
// This prevents a replaced session's goroutine from removing its successor.
func (a *ActiveSessions) Remove(sessionID string, gen uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	entry, ok := a.active[sessionID]
	if ok && entry.gen == gen {
		delete(a.active, sessionID)
	}
}

// CancelAll cancels all active sessions. Used during graceful shutdown.
func (a *ActiveSessions) CancelAll() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, entry := range a.active {
		entry.cancel()
	}
	a.active = make(map[string]sessionEntry)
}

// NewDispatcher returns an OnMessage handler that routes inbound cloud messages
// to the appropriate local subsystem.
//
// cfg.MachineID is this satellite's registered machine ID. Messages that carry a
// non-empty MachineID field that don't match are silently dropped — defense
// in depth against a misbehaving relay server sending cross-machine messages.
//
// cfg.DeliverPerm is called when a permission_response arrives from HuginnCloud;
// it should delegate to permissions.Gate.DeliverRelayResponse.
//
// cfg.Hub is used to send responses back to HuginnCloud. cfg.Store provides access
// to session metadata. Both are optional (nil-safe) for backward compatibility.
//
// cfg.Outbox is the durable outbox for retry on send failure. When set, any
// hub.Send() error causes the message to be queued in the outbox so it will be
// retried by the periodic flush goroutine in Runner.
//
// Phase 3 Implementation Status:
// - Implemented: MsgPermissionResp, MsgSessionResume, MsgChatMessage, MsgCancelSession,
//   MsgSessionStart, MsgSessionListRequest, MsgModelListRequest, MsgRunAgent
// - MsgAgentResult: satellite → cloud only (sent by this dispatcher; never received)
func NewDispatcher(cfg DispatcherConfig) func(context.Context, Message) {
	// sendOrEnqueue attempts hub.Send; on failure it queues the message in the
	// outbox (if wired) so the periodic flush can retry it.
	// For terminal messages (MsgDone, MsgAgentResult) the outbox enqueue is
	// retried once after a short back-off before giving up.
	// When both paths fail the dropped counter (if wired) is incremented and
	// the failure is logged at Error level.
	sendOrEnqueue := func(hub Hub, msg Message) {
		if err := hub.Send("", msg); err == nil {
			return
		}
		slog.Warn("relay: send failed, queuing in outbox",
			"type", msg.Type)
		if cfg.Outbox == nil {
			slog.Error("relay: outbox not wired — message lost",
				"type", msg.Type)
			if cfg.DroppedMessages != nil {
				cfg.DroppedMessages.Add(1)
			}
			return
		}
		enqErr := cfg.Outbox.Enqueue(msg)
		if enqErr == nil {
			return
		}
		// Retry once for terminal messages — they must not be silently lost.
		if isTerminalMessageType(msg.Type) {
			time.Sleep(50 * time.Millisecond)
			if retryErr := cfg.Outbox.Enqueue(msg); retryErr == nil {
				return
			}
		}
		slog.Error("relay: outbox enqueue failed — message lost",
			"type", msg.Type, "err", enqErr)
		if cfg.DroppedMessages != nil {
			cfg.DroppedMessages.Add(1)
		}
	}

	return func(ctx context.Context, msg Message) {
		// Machine ID validation (defense in depth)
		if msg.MachineID != "" && msg.MachineID != cfg.MachineID {
			slog.Warn("relay: dropping message addressed to wrong machine",
				"want", cfg.MachineID, "got", msg.MachineID, "type", msg.Type)
			return
		}

		hub := cfg.Hub // local alias for clarity

		switch msg.Type {
		case MsgPermissionResp:
			requestID, _ := msg.Payload["request_id"].(string)
			approved, _ := msg.Payload["approved"].(bool)
			if requestID == "" {
				slog.Warn("relay: permission_response missing request_id")
				return
			}
			if cfg.DeliverPerm != nil && !cfg.DeliverPerm(requestID, approved) {
				// Unknown ID — could be a stale response after timeout, or spoofing attempt.
				slog.Warn("relay: permission_response for unknown request_id", "id", requestID)
			}

		case MsgSessionResume:
			// Re-attach to interrupted session and send back session metadata.
			// Idempotency: If the ack is lost and the cloud replays this message,
			// the handler is idempotent — we simply retrieve and re-ack the same session.
			// No new session is created. SessionStore.Save uses Pebble's upsert semantics,
			// so even if a duplicate session_resume somehow triggered a Save with the same ID,
			// the second write would overwrite the first, leaving exactly one session in the store.
			if hub == nil || cfg.Store == nil {
				slog.Warn("relay: session_resume received but hub or store is nil")
				return
			}

			sessionID, _ := msg.Payload["session_id"].(string)
			if sessionID == "" {
				slog.Warn("relay: session_resume missing session_id")
				return
			}

			sess, err := cfg.Store.Get(sessionID)
			if err != nil {
				slog.Warn("relay: session_resume: session not found", "session_id", sessionID, "err", err)
				return // cloud may retry
			}

			ackMsg := Message{
				Type: MsgSessionResumeAck,
				Payload: map[string]any{
					"session_id": sess.ID,
					"status":     sess.Status,
					"last_seq":   sess.LastSeq,
				},
			}
			sendOrEnqueue(hub, ackMsg)

		case MsgChatMessage:
			sessionID, _ := msg.Payload["session_id"].(string)
			if sessionID == "" {
				sessionID = msg.SessionID // fall back to envelope-level field (huginncloud-app path)
			}
			content, _ := msg.Payload["content"].(string)
			if content == "" {
				content, _ = msg.Payload["text"].(string) // huginncloud-app sends "text" not "content"
			}
			if sessionID == "" || content == "" {
				slog.Warn("relay: chat_message: missing session_id or content")
				return
			}
			if hub == nil || cfg.ChatSession == nil {
				slog.Warn("relay: chat_message: not wired", "session_id", sessionID)
				return
			}

			sessCtx, cancel := context.WithCancel(ctx)
			var gen uint64
			if cfg.Active != nil {
				var replaced bool
				gen, replaced = cfg.Active.Start(sessionID, cancel)
				if replaced {
					slog.Warn("relay: chat_message: cancelled previous session", "session_id", sessionID)
				}
			}

			go func() {
				defer func() {
					cancel()
					if cfg.Active != nil {
						cfg.Active.Remove(sessionID, gen)
					}
				}()

				// --- idle-timeout watchdog ---
				// The timer is reset on every token/event; if it fires we cancel the
				// session and report a failure back to the cloud.
				var idleExpired atomic.Bool
				idleTimer := time.AfterFunc(ChatIdleTimeout, func() {
					idleExpired.Store(true)
					slog.Warn("relay: chat_message: idle timeout — cancelling session",
						"session_id", sessionID, "idle", ChatIdleTimeout)
					cancel()
				})
				defer idleTimer.Stop()

				// --- non-blocking token pump ---
				// A buffered channel absorbs bursts from the LLM stream goroutine so that
				// the onToken callback never blocks the underlying SSE parser.
				// A dedicated sender goroutine drains the channel into sendOrEnqueue.
				type tokenMsg struct {
					msg     Message
					dropped int // >0: some earlier tokens were dropped to make room
				}
				tokenCh := make(chan tokenMsg, tokenPumpSize)
				var pumpDropped atomic.Int64 // tokens dropped due to full channel

				// Sender goroutine — drains tokenCh until it is closed.
				pumpDone := make(chan struct{})
				go func() {
					defer close(pumpDone)
					for tm := range tokenCh {
						if tm.dropped > 0 {
							slog.Warn("relay: chat token pump overflow — dropped oldest tokens",
								"session_id", sessionID, "dropped", tm.dropped)
						}
						sendOrEnqueue(hub, tm.msg)
					}
				}()

				// enqueueToken does a non-blocking send into the token channel.
				// On overflow it drops the OLDEST pending token to make room.
				enqueueToken := func(m Message) {
					tm := tokenMsg{msg: m}
					select {
					case tokenCh <- tm:
					default:
						// Channel full — drop the oldest entry and push ours.
						dropped := int(pumpDropped.Add(1))
						select {
						case <-tokenCh: // discard oldest
						default:
						}
						tm.dropped = dropped
						select {
						case tokenCh <- tm:
						default:
							// Still can't send (very unlikely); just count the drop.
							pumpDropped.Add(1)
						}
					}
				}

				// Run the chat session, catching any panics so we can still send MsgDone.
				var chatErr error
				func() {
					defer func() {
						if r := recover(); r != nil {
							slog.Error("relay: MsgChatMessage goroutine panicked", "session_id", sessionID, "recover", r)
							chatErr = fmt.Errorf("internal server error")
						}
					}()
					chatErr = cfg.ChatSession(sessCtx, sessionID, content,
						func(token string) {
							idleTimer.Reset(ChatIdleTimeout)
							enqueueToken(Message{
								Type:    MsgToken,
								Payload: map[string]any{"session_id": sessionID, "text": token},
							})
						},
						func(eventType string, payload map[string]any) {
							idleTimer.Reset(ChatIdleTimeout)
							mt := MessageType(eventType)
							if mt == MsgToolCall || mt == MsgToolResult {
								payload["session_id"] = sessionID
								sendOrEnqueue(hub, Message{
									Type:    mt,
									Payload: payload,
								})
							}
						},
						func(ev backend.StreamEvent) {
							idleTimer.Reset(ChatIdleTimeout)
							switch ev.Type {
							case backend.StreamWarning:
								sendOrEnqueue(hub, Message{
									Type:    MsgWarning,
									Payload: map[string]any{"session_id": sessionID, "text": ev.Content},
								})
							case backend.StreamStatus:
								sendOrEnqueue(hub, Message{
									Type:    MsgStatus,
									Payload: map[string]any{"session_id": sessionID, "content": ev.Content},
								})
							}
						},
					)
				}()

				// Determine whether the session ended due to idle timeout (context was
				// cancelled by our watchdog) vs. external cancel or normal completion.
				isIdleTimeout := idleExpired.Load() && errors.Is(chatErr, context.Canceled)

				// Persist final status
				if cfg.Store != nil {
					status := "completed"
					if chatErr != nil && !errors.Is(chatErr, context.Canceled) {
						status = "failed"
					} else if isIdleTimeout {
						status = "failed"
					}
					if err := cfg.Store.Save(SessionMeta{ID: sessionID, Status: status}); err != nil {
						slog.Warn("dispatcher: failed to persist session status", "session_id", sessionID, "status", status, "err", err)
					}
				}

				// Drain the token channel fully before sending MsgDone so that
				// completion never races ahead of in-flight content.
				close(tokenCh)
				<-pumpDone

				totalDropped := pumpDropped.Load()
				donePayload := map[string]any{"session_id": sessionID}
				if isIdleTimeout {
					donePayload["status"] = "failed"
					donePayload["error"] = "idle timeout"
				} else if chatErr != nil && !errors.Is(chatErr, context.Canceled) {
					donePayload["error"] = chatErr.Error()
				}
				if totalDropped > 0 {
					donePayload["dropped_tokens"] = totalDropped
				}
				sendOrEnqueue(hub, Message{Type: MsgDone, Payload: donePayload})
			}()

		case MsgCancelSession:
			sessionID, _ := msg.Payload["session_id"].(string)
			if sessionID == "" {
				slog.Warn("relay: cancel_session: missing session_id")
				return
			}
			if cfg.Active == nil {
				slog.Warn("relay: cancel_session: ActiveSessions not wired")
				return
			}
			if !cfg.Active.Cancel(sessionID) {
				slog.Warn("relay: cancel_session: session not found", "session_id", sessionID)
			}

		case MsgSessionStart:
			if cfg.NewSession == nil || hub == nil {
				slog.Warn("relay: session_start: not wired")
				return
			}
			reqID, _ := msg.Payload["session_id"].(string)
			sessID := cfg.NewSession(reqID)
			if cfg.Store != nil {
				if err := cfg.Store.Save(SessionMeta{
					ID:        sessID,
					StartedAt: time.Now(),
					Status:    "active",
				}); err != nil {
					slog.Warn("dispatcher: failed to persist session status", "session_id", sessID, "status", "active", "err", err)
				}
			}
			sendOrEnqueue(hub, Message{
				Type:    MsgSessionStartAck,
				Payload: map[string]any{"session_id": sessID, "status": "created"},
			})

		case MsgSessionListRequest:
			if cfg.Store == nil || hub == nil {
				slog.Warn("relay: session_list_request: not wired")
				return
			}
			sessions, err := cfg.Store.List()
			if err != nil {
				slog.Warn("relay: session_list_request: store error", "err", err)
				sendOrEnqueue(hub, Message{
					Type:    MsgSessionListResult,
					Payload: map[string]any{"error": err.Error()},
				})
				return
			}
			sendOrEnqueue(hub, Message{
				Type:    MsgSessionListResult,
				Payload: map[string]any{"sessions": sessions},
			})

		case MsgModelListRequest:
			if cfg.ListModels == nil || hub == nil {
				slog.Warn("relay: model_list_request: not wired")
				return
			}
			sendOrEnqueue(hub, Message{
				Type:    MsgModelListResult,
				Payload: map[string]any{"models": cfg.ListModels()},
			})

		case MsgModelProviderListRequest:
			if cfg.GetModelProviders == nil || hub == nil {
				slog.Warn("relay: model_provider_list_request: not wired")
				return
			}
			providers := cfg.GetModelProviders()
			// Redact api_key, convert to JSON-safe types for round-trip.
			redacted := make([]ModelProviderInfo, len(providers))
			copy(redacted, providers)
			for i := range redacted {
				redacted[i].APIKey = ""
				// NOTE: Models slice shares the underlying array (shallow copy);
				// it is not mutated here, so this is safe.
			}
			b, marshalErr := json.Marshal(redacted)
			if marshalErr != nil {
				slog.Warn("relay: model_provider_list_request: marshal error", "err", marshalErr)
				sendOrEnqueue(hub, Message{Type: MsgModelProviderListResult, Payload: map[string]any{"error": "internal error"}})
				return
			}
			var list []any
			if unmarshalErr := json.Unmarshal(b, &list); unmarshalErr != nil {
				slog.Warn("relay: model_provider_list_request: unmarshal error", "err", unmarshalErr)
				sendOrEnqueue(hub, Message{Type: MsgModelProviderListResult, Payload: map[string]any{"error": "internal error"}})
				return
			}
			sendOrEnqueue(hub, Message{
				Type:    MsgModelProviderListResult,
				Payload: map[string]any{"providers": list},
			})

		case MsgModelConfigGetRequest:
			if cfg.GetModelConfig == nil || hub == nil {
				slog.Warn("relay: model_config_get_request: not wired")
				return
			}
			provider, _ := msg.Payload["provider"].(string)
			if provider == "" {
				slog.Warn("relay: model_config_get_request: missing provider")
				return
			}
			info, err := cfg.GetModelConfig(provider)
			if err != nil {
				sendOrEnqueue(hub, Message{
					Type:    MsgModelConfigGetResult,
					Payload: map[string]any{"error": err.Error()},
				})
				return
			}
			if info == nil {
				slog.Warn("relay: model_config_get_request: callback returned nil info")
				sendOrEnqueue(hub, Message{
					Type:    MsgModelConfigGetResult,
					Payload: map[string]any{"error": "provider not found"},
				})
				return
			}
			safe := *info
			safe.APIKey = "[REDACTED]"
			b, marshalErr := json.Marshal(safe)
			if marshalErr != nil {
				slog.Warn("relay: model_config_get_request: marshal error", "err", marshalErr)
				sendOrEnqueue(hub, Message{Type: MsgModelConfigGetResult, Payload: map[string]any{"error": "internal error"}})
				return
			}
			var m map[string]any
			if unmarshalErr := json.Unmarshal(b, &m); unmarshalErr != nil {
				slog.Warn("relay: model_config_get_request: unmarshal error", "err", unmarshalErr)
				sendOrEnqueue(hub, Message{Type: MsgModelConfigGetResult, Payload: map[string]any{"error": "internal error"}})
				return
			}
			sendOrEnqueue(hub, Message{
				Type:    MsgModelConfigGetResult,
				Payload: map[string]any{"provider": m},
			})

		case MsgModelConfigUpdateRequest:
			if cfg.UpdateModelConfig == nil || hub == nil {
				slog.Warn("relay: model_config_update_request: not wired")
				return
			}
			provider, _ := msg.Payload["provider"].(string)
			endpoint, _ := msg.Payload["endpoint"].(string)
			apiKey, _ := msg.Payload["api_key"].(string)
			if provider == "" {
				slog.Warn("relay: model_config_update_request: missing provider")
				return
			}
			if apiKey == "[REDACTED]" {
				apiKey = ""
			}
			if err := cfg.UpdateModelConfig(provider, endpoint, apiKey); err != nil {
				sendOrEnqueue(hub, Message{
					Type:    MsgModelConfigUpdateResult,
					Payload: map[string]any{"ok": false, "error": err.Error()},
				})
				return
			}
			sendOrEnqueue(hub, Message{
				Type:    MsgModelConfigUpdateResult,
				Payload: map[string]any{"ok": true, "error": ""},
			})

		case MsgModelPullRequest:
			if cfg.PullModel == nil || hub == nil {
				slog.Warn("relay: model_pull_request: not wired")
				return
			}
			model, _ := msg.Payload["model"].(string)
			if model == "" {
				slog.Warn("relay: model_pull_request: missing model")
				return
			}
			if err := cfg.PullModel(model); err != nil {
				sendOrEnqueue(hub, Message{
					Type:    MsgModelPullResult,
					Payload: map[string]any{"ok": false, "error": err.Error()},
				})
				return
			}
			sendOrEnqueue(hub, Message{
				Type:    MsgModelPullResult,
				Payload: map[string]any{"ok": true, "error": ""},
			})

		case MsgShellStart:
			if cfg.Shell == nil {
				slog.Warn("relay: shell_start: ShellManager not wired")
				return
			}
			if hub == nil {
				slog.Warn("relay: shell_start: hub not wired")
				return
			}
			cols := uint16(safeFloat(msg.Payload["cols"], 220))
			rows := uint16(safeFloat(msg.Payload["rows"], 50))
			cfg.Shell.Start(hub, cols, rows)

		case MsgShellInput:
			if cfg.Shell == nil {
				return
			}
			data, _ := msg.Payload["data"].(string)
			cfg.Shell.Input(data)

		case MsgShellResize:
			if cfg.Shell == nil {
				return
			}
			cols := uint16(safeFloat(msg.Payload["cols"], 220))
			rows := uint16(safeFloat(msg.Payload["rows"], 50))
			cfg.Shell.Resize(cols, rows)

		case MsgShellExit:
			if cfg.Shell == nil {
				return
			}
			cfg.Shell.Exit()

		case MsgShellEchoOff:
			if cfg.Shell != nil {
				cfg.Shell.SetEcho(false)
			}

		case MsgShellEchoOn:
			if cfg.Shell != nil {
				cfg.Shell.SetEcho(true)
			}

		case MsgHTTPRequest:
			slog.Debug("relay: http_request received", "machine_id", msg.MachineID, "proxy_wired", cfg.HTTPProxy != nil)
			if cfg.HTTPProxy == nil || hub == nil {
				slog.Warn("relay: http_request: HTTPProxy not wired")
				return
			}
			requestID, _ := msg.Payload["request_id"].(string)
			method, _ := msg.Payload["method"].(string)
			path, _ := msg.Payload["path"].(string)
			if requestID == "" || method == "" || path == "" {
				slog.Warn("relay: http_request: missing request_id, method, or path",
					"payload_keys", func() []string {
						keys := make([]string, 0, len(msg.Payload))
						for k := range msg.Payload {
							keys = append(keys, k)
						}
						return keys
					}())
				return
			}
			var body []byte
			if b, ok := msg.Payload["body"].(string); ok && b != "" {
				body = []byte(b)
			}
			slog.Info("relay: http_request received", "method", method, "path", path, "request_id", requestID)
			// NOTE: HTTPProxy goroutine does not currently support context cancellation.
			// The HTTPProxy callback signature does not accept a context parameter, so this
			// goroutine cannot be cancelled externally. Future refactor should update the
			// DispatcherConfig.HTTPProxy type signature to accept context.Context and thread
			// it through to the implementation in main.go::makeLocalHTTPProxy().
			go func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("relay: HTTPProxy goroutine panicked", "method", method, "path", path, "recover", r)
						sendOrEnqueue(hub, Message{Type: MsgHTTPResponse, Payload: map[string]any{"request_id": requestID, "status": 500, "error": "internal server error"}})
					}
				}()
				status, respBody, proxyErr := cfg.HTTPProxy(method, path, body)
				slog.Info("relay: http_request proxied", "method", method, "path", path,
					"status", status, "response_len", len(respBody), "err", proxyErr)
				payload := map[string]any{
					"request_id": requestID,
					"status":     status,
					"body":       string(respBody),
				}
				if proxyErr != nil {
					payload["error"] = proxyErr.Error()
					if status == 0 {
						payload["status"] = 500
					}
				}
				slog.Debug("relay: http_response sending", "request_id", requestID, "status", payload["status"])
				sendOrEnqueue(hub, Message{Type: MsgHTTPResponse, Payload: payload})
			}()

		case MsgRunAgent:
			runID, _ := msg.Payload["run_id"].(string)
			agentName, _ := msg.Payload["agent_name"].(string)
			prompt, _ := msg.Payload["prompt"].(string)
			sessionID, _ := msg.Payload["session_id"].(string)

			if hub == nil {
				slog.Warn("relay: run_agent: hub not wired", "run_id", runID)
				return
			}

			// sendAgentResult is a convenience helper to send an MsgAgentResult frame.
			// Tagged PriorityHigh so it drains before bulk messages under outbox backpressure.
			sendAgentResult := func(token string, done bool, errMsg string) {
				sendOrEnqueue(hub, Message{
					Type:     MsgAgentResult,
					Priority: PriorityHigh,
					Payload: map[string]any{
						"run_id": runID,
						"token":  token,
						"done":   done,
						"error":  errMsg,
					},
				})
			}

			if agentName == "" || prompt == "" || runID == "" {
				slog.Warn("relay: run_agent: missing required fields",
					"run_id", runID, "agent_name", agentName, "has_prompt", prompt != "")
				sendAgentResult("", true, "run_agent: missing required fields (run_id, agent_name, prompt)")
				return
			}

			if cfg.RunAgent == nil {
				slog.Warn("relay: run_agent: RunAgent not wired", "run_id", runID)
				sendAgentResult("", true, "run_agent: not configured on this satellite")
				return
			}

			runCtx, runCancel := context.WithCancel(ctx)
			var gen uint64
			if cfg.Active != nil {
				// Use run_id as the tracking key so cancel_session can interrupt it.
				var replaced bool
				gen, replaced = cfg.Active.Start(runID, runCancel)
				if replaced {
					slog.Warn("relay: run_agent: cancelled previous run with same run_id", "run_id", runID)
				}
			}

			go func() {
				defer func() {
					runCancel()
					if cfg.Active != nil {
						cfg.Active.Remove(runID, gen)
					}
				}()

				// --- idle-timeout watchdog ---
				var idleExpiredRun atomic.Bool
				idleTimer := time.AfterFunc(ChatIdleTimeout, func() {
					idleExpiredRun.Store(true)
					slog.Warn("relay: run_agent: idle timeout — cancelling run",
						"run_id", runID, "idle", ChatIdleTimeout)
					runCancel()
				})
				defer idleTimer.Stop()

				// --- non-blocking token pump ---
				type agentTokenMsg struct {
					token   string
					dropped int
				}
				tokenCh := make(chan agentTokenMsg, tokenPumpSize)
				var pumpDropped atomic.Int64

				pumpDone := make(chan struct{})
				go func() {
					defer close(pumpDone)
					for tm := range tokenCh {
						if tm.dropped > 0 {
							slog.Warn("relay: run_agent token pump overflow — dropped oldest tokens",
								"run_id", runID, "dropped", tm.dropped)
						}
						sendAgentResult(tm.token, false, "")
					}
				}()

				enqueueAgentToken := func(token string) {
					tm := agentTokenMsg{token: token}
					select {
					case tokenCh <- tm:
					default:
						dropped := int(pumpDropped.Add(1))
						select {
						case <-tokenCh:
						default:
						}
						tm.dropped = dropped
						select {
						case tokenCh <- tm:
						default:
							pumpDropped.Add(1)
						}
					}
				}

				// Run the agent, catching any panics so we can still send the done frame.
				var runErr error
				func() {
					defer func() {
						if r := recover(); r != nil {
							slog.Error("relay: MsgRunAgent goroutine panicked", "run_id", runID, "agent", agentName, "recover", r)
							runErr = fmt.Errorf("internal server error")
						}
					}()
					runErr = cfg.RunAgent(runCtx, agentName, prompt, sessionID,
						func(token string) {
							idleTimer.Reset(ChatIdleTimeout)
							enqueueAgentToken(token)
						},
					)
				}()

				isIdleTimeout := idleExpiredRun.Load() && errors.Is(runErr, context.Canceled)

				// Drain the token pump before sending the done frame.
				close(tokenCh)
				<-pumpDone

				totalDropped := pumpDropped.Load()
				errMsg := ""
				if isIdleTimeout {
					errMsg = "idle timeout"
				} else if runErr != nil && !errors.Is(runErr, context.Canceled) {
					errMsg = runErr.Error()
					slog.Warn("relay: run_agent: run failed", "run_id", runID, "agent", agentName, "err", runErr)
				}
				// Send the final done frame with dropped token count if any.
				doneMsg := Message{
					Type:     MsgAgentResult,
					Priority: PriorityHigh,
					Payload: map[string]any{
						"run_id": runID,
						"token":  "",
						"done":   true,
						"error":  errMsg,
					},
				}
				if totalDropped > 0 {
					doneMsg.Payload["dropped_tokens"] = totalDropped
				}
				sendOrEnqueue(hub, doneMsg)
			}()

		case MsgAgentResult:
			// MsgAgentResult flows satellite → cloud only; the satellite never receives it.
			// Log at debug level in case of relay misconfiguration.
			slog.Debug("relay: agent_result received by satellite (unexpected)", "payload", msg.Payload)

		default:
			// Log so we have visibility into unexpected message types from HuginnCloud.
			slog.Debug("relay: unhandled message type", "type", msg.Type)
		}
	}
}

// Dispatcher wraps a dispatcher function for testing and fuzz testing.
// The field is exported so tests can construct it directly if needed.
type Dispatcher struct {
	Fn func(context.Context, Message)
}

// DispatchRaw parses data as JSON into a Message, then dispatches it.
// It is safe for arbitrary input — parse errors are returned, not panicked.
// This is the entry point for fuzz testing.
func (d *Dispatcher) DispatchRaw(ctx context.Context, data []byte) error {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("relay: dispatch raw: %w", err)
	}
	d.Fn(ctx, msg)
	return nil
}

// Dispatch dispatches a Message through the dispatcher.
func (d *Dispatcher) Dispatch(ctx context.Context, msg Message) error {
	d.Fn(ctx, msg)
	return nil
}
