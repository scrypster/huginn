package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/models"
	"github.com/scrypster/huginn/internal/notification"
	"github.com/scrypster/huginn/internal/relay"
	"github.com/scrypster/huginn/internal/runtime"
	"github.com/scrypster/huginn/internal/scheduler"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/sqlitedb"
	"github.com/scrypster/huginn/internal/stats"
	"github.com/scrypster/huginn/internal/threadmgr"
)

// workstreamStore is the interface satisfied by *spaces.WorkstreamStore and test
// doubles. It covers all methods called by the workstream HTTP handlers.
type workstreamStore interface {
	Create(ctx context.Context, name, description string) (*spaces.Workstream, error)
	List(ctx context.Context) ([]*spaces.Workstream, error)
	Get(ctx context.Context, id string) (*spaces.Workstream, error)
	Delete(ctx context.Context, id string) error
	TagSession(ctx context.Context, workstreamID, sessionID string) error
	ListSessions(ctx context.Context, workstreamID string) ([]string, error)
}

// Server is the Huginn HTTP + WebSocket server.
type Server struct {
	cfg       config.Config
	orch      *agent.Orchestrator
	store     session.StoreInterface
	token     string
	huginnDir string
	addr      string
	mu        sync.Mutex
	srv       *http.Server
	wsHub     *WSHub

	// agentLoader loads the agent config. Nil uses agents.LoadAgents (production default).
	// Override in tests to inject a known configuration without touching the filesystem.
	agentLoader func() (*agents.AgentsConfig, error)

	// backendCache is the live BackendCache wired to the orchestrator.
	// Set via WithBackendCache() at startup. Used by handleUpdateConfig to push
	// provider key changes into the running cache without requiring a restart.
	backendCache *backend.BackendCache

	connMgr       *connections.Manager
	connStore     connections.StoreInterface
	connProviders map[connections.Provider]connections.IntegrationProvider
	oauthLimiter  *flowRateLimiter
	authLimiter   *authFailLimiter // per-server IP-based auth-failure rate limiter

	// Per-endpoint HTTP rate limiters (per-IP sliding window).
	sessionCreateLimiter *endpointRateLimiter
	spaceCreateLimiter   *endpointRateLimiter
	workflowRunLimiter   *endpointRateLimiter
	mutationLimiter      *endpointRateLimiter

	// wsRateLimitExceeded counts total WebSocket messages dropped due to rate limiting.
	wsRateLimitExceeded int64

	cloudRegistrar interface{ DeliverCode(string) }
	brokerClient   BrokerClient // nil = local flow; non-nil = route through HuginnCloud broker

	relayTokenStorer relay.TokenStorer // nil if not registered with HuginnCloud
	jwtSecret        string            // used to verify relay JWTs
	registering      bool              // true if registration flow is in progress

	// openBrowserFn overrides the Registrar's OpenBrowserFn when set.
	// Tests set this to a no-op to prevent real browser windows from opening
	// during handleCloudConnect's background registration goroutine.
	openBrowserFn func(string) error

	tm          *threadmgr.ThreadManager         // may be nil if multi-agent not configured
	previewGate *threadmgr.DelegationPreviewGate // may be nil if preview not configured
	ca          *threadmgr.CostAccumulator       // may be nil if cost tracking not configured

	// delegationStore persists agent delegation records. nil if the underlying
	// store doesn't implement session.DelegationStore (e.g. in-memory store in tests).
	delegationStore session.DelegationStore

	// mentionDelegate is called after each chat message to parse @Agent mentions
	// and spawn threads for any matched agents. Used as a fallback for models
	// that don't support tool calling. parentMsgID is the session message ID of
	// the triggering message (empty if unknown).
	mentionDelegate func(ctx context.Context, sessionID, userMsg, parentMsgID string)

	runtimeMgr *runtime.Manager // may be nil if built-in llama.cpp not configured
	modelStore *models.Store    // may be nil if built-in llama.cpp not configured

	notifStore       notification.StoreInterface // nil if notification storage not configured
	sched            *scheduler.Scheduler       // nil if scheduler not configured
	workflowRunStore scheduler.WorkflowRunStoreInterface // nil if not configured

	satellite *relay.Satellite // nil if not registered with HuginnCloud
	outbox    *relay.Outbox   // nil if outbox not wired (no store path)

	// relayKeys maps provider name → base64url-encoded relay_key for in-progress cloud OAuth flows.
	// Protected by relayKeysMu.
	relayKeys   map[string]string
	relayKeysMu sync.RWMutex

	muninnCfgPath string // path to ~/.config/huginn/muninn.json

	// vaultProberFn probes MCP vault connectivity for handleVaultTest.
	// When nil the production implementation (agent.ProbeVaultConnectivity) is used.
	// Tests override this to avoid real network connections.
	// Returns (toolsCount, warning, error): warning is non-empty when the token
	// is about to expire but the connection succeeded.
	vaultProberFn func(ctx context.Context, cfgPath, vaultName string) (int, string, error)

	// skillsBaseURL overrides the registry raw base URL used by handleSkillsInstall.
	// Empty string means use the default (skills.SkillsRawBaseURL).
	// Tests set this to a local httptest server URL.
	skillsBaseURL string

	// configPath overrides where cfg.Save() writes to.
	// When empty (production), cfg.Save() uses the default ~/.huginn/config.json path.
	// Tests set this to a temp file path so they never corrupt the real config.
	configPath string

	// keyStorerFn overrides backend.StoreAPIKey for storing API keys in the OS keychain.
	// When nil (production), backend.StoreAPIKey is used.
	// Tests set this to a no-op to avoid writing to the real macOS Keychain.
	keyStorerFn func(slot, value string) (string, error)

	spaceStore spaces.StoreInterface // nil if spaces not configured

	// db is the SQLite database used by thread/message handlers. nil if not configured.
	db *sqlitedb.DB

	// artifactStore handles workforce artifact persistence. nil if not configured.
	artifactStore artifactStore

	// symbolStore and symbolCache back the /api/v1/symbols/* handlers.
	symbolStore  symbolQuerier
	symbolCache  *symbolIndexCache

	// ctx is the server lifecycle context stored at Start(). Used by long-running
	// goroutines (e.g. SpawnThread) that must outlive individual HTTP requests.
	ctx context.Context

	// spawnWg tracks in-flight SpawnThread goroutines so Stop() can drain them.
	spawnWg sync.WaitGroup

	// statsReg is the stats registry wired for the /api/v1/metrics endpoint.
	// nil if metrics are not configured.
	statsReg *stats.Registry

	// prometheusSt holds the Prometheus registry and handler for /api/v1/metrics/prometheus.
	// Initialised alongside statsReg; nil if metrics are not configured.
	prometheusSt *promState

	// statsPersister flushes stats + cost records to SQLite every 5 minutes.
	// nil if not configured. Must be closed before the HTTP server shuts down.
	statsPersister *stats.Persister

	// auditLog writes permission gate decisions to the SQLite audit_log table.
	// nil if not configured.
	auditLog *auditLogger

	// workstreamStore is the workstream store wired for the /api/v1/workstreams endpoints.
	// nil if workstreams are not configured.
	workstreamStore workstreamStore

	// originWarnOnce ensures the "no AllowedOrigins configured" warning is
	// emitted at most once per server lifetime to avoid log spam.
	originWarnOnce sync.Once

	// upgrader is the WebSocket upgrader initialised in New() with s.checkOrigin
	// so that AllowedOrigins config is honoured. Never use a global upgrader.
	upgrader websocket.Upgrader

	// swarmSnapshots stores the final swarm_complete payload keyed by sessionID.
	// Used by handleSessionActiveState to provide reconnect recovery state.
	// Entries are evicted after swarmSnapshotTTL (1h) by a background goroutine.
	// Value type: swarmSnapshotEntry
	swarmSnapshots sync.Map
}

// SetDB wires the SQLite database for thread/message handlers.
func (s *Server) SetDB(db *sqlitedb.DB) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db = db
}

// SetArtifactStore wires the artifact store for workforce artifact handlers.
func (s *Server) SetArtifactStore(store artifactStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.artifactStore = store
}

// WithBackendCache wires the live BackendCache so that handleUpdateConfig can
// push provider key changes into running backends without requiring a restart.
func (s *Server) WithBackendCache(bc *backend.BackendCache) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.backendCache = bc
	return s
}

// swarmSnapshotEntry is the stored value for swarmSnapshots.
type swarmSnapshotEntry struct {
	payload  map[string]any
	storedAt time.Time
}

// swarmSnapshotTTL is how long a swarm state snapshot is kept for reconnect recovery.
const swarmSnapshotTTL = 1 * time.Hour

// evictSwarmSnapshots runs until ctx is cancelled, pruning swarmSnapshots entries
// that are older than swarmSnapshotTTL. It runs every 15 minutes.
func (s *Server) evictSwarmSnapshots(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			s.swarmSnapshots.Range(func(key, val any) bool {
				if e, ok := val.(swarmSnapshotEntry); ok {
					if now.Sub(e.storedAt) > swarmSnapshotTTL {
						s.swarmSnapshots.Delete(key)
					}
				}
				return true
			})
		}
	}
}

// New creates a new Server. Call Start() to begin serving.
func New(
	cfg config.Config,
	orch *agent.Orchestrator,
	store session.StoreInterface,
	token string,
	huginnDir string,
	connMgr *connections.Manager,
	connStore connections.StoreInterface,
	providers []connections.IntegrationProvider,
) *Server {
	pm := make(map[connections.Provider]connections.IntegrationProvider, len(providers))
	for _, p := range providers {
		pm[p.Name()] = p
	}
	s := &Server{
		cfg:           cfg,
		orch:          orch,
		store:         store,
		token:         token,
		huginnDir:     huginnDir,
		wsHub:         newWSHub(),
		connMgr:       connMgr,
		connStore:     connStore,
		connProviders: pm,
		oauthLimiter:  newFlowRateLimiter(),
		authLimiter:   newAuthFailLimiter(),
		relayKeys:     make(map[string]string),

		// Enterprise-safe rate limits (per-IP, sliding window).
		sessionCreateLimiter: newEndpointRateLimiter(10, time.Minute),
		spaceCreateLimiter:   newEndpointRateLimiter(20, time.Minute),
		workflowRunLimiter:   newEndpointRateLimiter(30, time.Minute),
		mutationLimiter:      newEndpointRateLimiter(60, time.Minute),
	}
	// Initialise the WebSocket upgrader using s.checkOrigin so AllowedOrigins
	// config is honoured. Must happen after s is initialised (checkOrigin reads s.cfg).
	s.upgrader = websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     s.checkOrigin,
	}
	// Wire the thread reply-count WS hook if the store supports it.
	// This broadcasts thread_reply_updated to all session clients when a
	// thread reply is appended, keeping the frontend badge count in sync.
	if sqlStore, ok := store.(*session.SQLiteSessionStore); ok {
		sqlStore.OnThreadReply = func(sessionID, parentMsgID string, newCount int64) {
			s.BroadcastToSession(sessionID, "thread_reply_updated", map[string]any{
				"message_id":  parentMsgID,
				"reply_count": newCount,
				"session_id":  sessionID,
			})
		}
	}
	// Wire delegation persistence and run startup reconciliation if the store supports it.
	if ds, ok := store.(session.DelegationStore); ok {
		s.delegationStore = ds
		go func() {
			if err := ds.ReconcileOrphanDelegations(); err != nil {
				log.Printf("warn: delegation: reconcile orphans: %v", err)
			}
		}()
	}
	return s
}

// Start begins listening. Binds to cfg.WebUI.Bind (default 127.0.0.1).
// If cfg.WebUI.Port == 0, uses a dynamically allocated port.
func (s *Server) Start(ctx context.Context) error {
	s.ctx = ctx
	bind := s.cfg.WebUI.Bind
	if bind == "" {
		bind = "127.0.0.1"
	}
	port := s.cfg.WebUI.Port
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", bind, port))
	if err != nil {
		return fmt.Errorf("server: listen: %w", err)
	}
	s.mu.Lock()
	s.addr = ln.Addr().String()
	s.mu.Unlock()

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	// Build CSRF origin allowlist from the bound address.
	// Non-browser clients (CLI, relay, MCP) send no Origin header and bypass naturally.
	allowedOrigins := map[string]bool{
		fmt.Sprintf("http://%s", ln.Addr().String()): true,
	}
	// Also allow the configured bind+port in case ln.Addr() differs (e.g. 0.0.0.0 vs 127.0.0.1)
	if bind != "" && port != 0 {
		allowedOrigins[fmt.Sprintf("http://%s:%d", bind, port)] = true
	}

	s.srv = &http.Server{
		Handler:      csrfMiddleware(allowedOrigins)(mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second, // generous for streaming responses
		IdleTimeout:  120 * time.Second,
	}
	go s.srv.Serve(ln)
	go s.wsHub.run()
	go s.evictSwarmSnapshots(ctx)
	return nil
}

// Addr returns the address the server is listening on (e.g. "127.0.0.1:8477").
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// Stop gracefully shuts down the server and stops background workers.
// It also waits up to 30 seconds for in-flight SpawnThread goroutines to finish.
//
// Shutdown ordering (enforced here):
//  1. statsPersister.Close() — drain in-flight stats/cost records to SQLite
//  2. auditLog.Close()       — drain audit events to SQLite
//  3. wsHub.stop()           — close WS connections
//  4. http.Server.Shutdown() — stop accepting new requests
//  (caller's cleanup fn closes db after Stop returns)
func (s *Server) Stop(ctx context.Context) error {
	// Flush the stats persister before the HTTP server shuts down so that
	// in-flight cost/stats records reach SQLite while the DB is still open.
	s.mu.Lock()
	persister := s.statsPersister
	auditLog := s.auditLog
	s.mu.Unlock()
	if persister != nil {
		persister.Close()
	}
	if auditLog != nil {
		auditLog.Close()
	}

	if s.wsHub != nil {
		s.wsHub.stop()
	}

	// Drain in-flight thread goroutines with a 30-second timeout.
	spawnDone := make(chan struct{})
	go func() {
		s.spawnWg.Wait()
		close(spawnDone)
	}()
	select {
	case <-spawnDone:
	case <-time.After(30 * time.Second):
		// Log but don't block shutdown indefinitely.
	}

	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}

// WSRateLimitExceeded returns the total number of WS messages dropped due to
// per-connection rate limiting since the server started.
func (s *Server) WSRateLimitExceeded() int64 {
	return atomic.LoadInt64(&s.wsRateLimitExceeded)
}

// SetRelayConfig wires the relay token storer and JWT secret used by the
// /oauth/relay endpoint to verify HuginnCloud-signed relay tokens.
func (s *Server) SetRelayConfig(storer relay.TokenStorer, jwtSecret string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.relayTokenStorer = storer
	s.jwtSecret = jwtSecret
}

// BroadcastWS sends a message to all connected WebSocket clients.
// It is a no-op when wsHub is nil (e.g. during testing or early startup).
func (s *Server) BroadcastWS(msg WSMessage) {
	if s.wsHub == nil {
		return
	}
	s.wsHub.broadcast(msg)
}

// SendRelay sends a relay message to HuginnCloud if the satellite is connected.
// It is a no-op when the satellite is nil, not connected, or has no active hub.
func (s *Server) SendRelay(msg relay.Message) {
	s.mu.Lock()
	sat := s.satellite
	s.mu.Unlock()
	if sat == nil {
		return
	}
	hub := sat.ActiveHub()
	if hub == nil {
		return
	}
	_ = hub.Send("", msg) // best-effort; errors are not fatal
}

// SetMentionDelegate installs a function that is called after each chat message
// to parse @AgentName mentions and spawn threads for matching agents. This acts
// as a fallback delegation path for models that don't support tool calling.
// parentMsgID is the session message ID of the triggering message (empty if unknown).
func (s *Server) SetMentionDelegate(fn func(ctx context.Context, sessionID, userMsg, parentMsgID string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mentionDelegate = fn
}

// BroadcastToSession sends a typed event to all WS clients subscribed to sessionID.
// Used by the delegate tool to push thread lifecycle events from goroutines.
func (s *Server) BroadcastToSession(sessionID, msgType string, payload map[string]any) {
	if s.wsHub == nil || sessionID == "" {
		return
	}
	s.wsHub.broadcastToSession(sessionID, WSMessage{Type: msgType, Payload: payload})
}

// ResolveAgent returns the primary agent for the given session, delegating to
// the internal resolveAgent method. Returns nil if no agent can be resolved.
func (s *Server) ResolveAgent(sessionID string) *agents.Agent {
	return s.resolveAgent(sessionID)
}

// ResolveAgentForSpace returns the lead agent for the given space (channel), or
// falls back to the session's primary agent. Use this for follow-up messages in
// channel sessions so the correct lead agent (e.g. Tom) synthesizes, not the
// default/first agent (e.g. Alice).
func (s *Server) ResolveAgentForSpace(sessionID, spaceID string) *agents.Agent {
	// Fall back to the session-level resolver; space-specific lead-agent
	// resolution can be added here in the future if needed.
	return s.resolveAgent(sessionID)
}

// BroadcastPlanning emits a "planning" event to the given session.
// Call this just before initiating the primary agent LLM call.
// Returns early if sessionID is empty to prevent wildcard broadcasts that would
// reach all connected clients regardless of their session subscription.
func (s *Server) BroadcastPlanning(sessionID, agentName string) {
	if s.wsHub == nil || sessionID == "" {
		return
	}
	s.wsHub.broadcastToSession(sessionID, WSMessage{
		Type:    "planning",
		Payload: map[string]any{"agent": agentName},
	})
}

// BroadcastPlanningDone emits a "planning_done" event to the given session.
// Call this after the primary agent response is complete.
func (s *Server) BroadcastPlanningDone(sessionID string) {
	if s.wsHub == nil {
		return
	}
	s.wsHub.broadcastToSession(sessionID, WSMessage{
		Type:    "planning_done",
		Payload: map[string]any{},
	})
}

// BroadcastNotification pushes notification_new and inbox_badge WS events
// to locally connected browsers, and forwards the notification to HuginnCloud
// via the relay buffer so offline browsers receive it on reconnect.
func (s *Server) BroadcastNotification(n *notification.Notification, pendingCount int) {
	if s.wsHub != nil {
		s.wsHub.broadcast(WSMessage{
			Type: WSEventNotificationNew,
			Payload: map[string]any{
				"notification": n,
			},
		})
		s.wsHub.broadcast(WSMessage{
			Type: WSEventInboxBadge,
			Payload: map[string]any{
				"pending_count": pendingCount,
			},
		})
	}
	// Forward to HuginnCloud relay buffer so offline browsers get the notification
	// (including the current pending_count for badge sync) when they reconnect.
	if n != nil {
		s.SendRelay(BuildNotificationRelayMsg(n, pendingCount))
	}
}

// SetSatellite stores the HuginnCloud satellite so the /api/v1/cloud/status
// endpoint can reflect its current registration and connection state.
func (s *Server) SetSatellite(sat *relay.Satellite) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.satellite = sat
}

// Satellite returns the stored satellite (may be nil).
func (s *Server) Satellite() *relay.Satellite {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.satellite
}

// SetOutbox stores the relay outbox so /api/v1/health can report
// outbox_depth and outbox_dropped counters.
func (s *Server) SetOutbox(ob *relay.Outbox) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outbox = ob
}

// SetCloudRegistrar sets the registrar that receives cloud callback codes.
func (s *Server) SetCloudRegistrar(r interface{ DeliverCode(string) }) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cloudRegistrar = r
}

// BrokerClient starts an OAuth flow via the HuginnCloud broker.
// Satisfied by *connections/broker.Client.
type BrokerClient interface {
	Start(ctx context.Context, provider, relayChallenge string, port int) (string, error)
	// StartCloudFlow initiates the cloud-UI OAuth flow — tokens are delivered via
	// relay JWT through the cloud app WebSocket instead of an ephemeral local server.
	StartCloudFlow(ctx context.Context, provider, relayKey string) (string, error)
}

// SetBrokerClient wires the HuginnCloud broker client used by POST /api/v1/connections/start.
// When set, OAuth flows are routed through the broker instead of the local /oauth/callback.
func (s *Server) SetBrokerClient(c BrokerClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.brokerClient = c
}

// SetThreadManager wires the ThreadManager used for multi-agent thread management.
func (s *Server) SetThreadManager(tm *threadmgr.ThreadManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tm = tm
}

// SetPreviewGate wires the DelegationPreviewGate for delegation approval.
func (s *Server) SetPreviewGate(g *threadmgr.DelegationPreviewGate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.previewGate = g
}

// SetCostAccumulator wires the CostAccumulator for session cost tracking.
func (s *Server) SetCostAccumulator(ca *threadmgr.CostAccumulator) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ca = ca
}

// SetRuntimeManager wires the built-in llama.cpp runtime Manager.
func (s *Server) SetRuntimeManager(mgr *runtime.Manager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runtimeMgr = mgr
}

// SetModelStore wires the built-in llama.cpp model Store.
func (s *Server) SetModelStore(store *models.Store) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.modelStore = store
}

// SetNotificationStore wires the Notification store.
func (s *Server) SetNotificationStore(store notification.StoreInterface) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifStore = store
}

// SetScheduler wires the Routine Scheduler.
func (s *Server) SetScheduler(sched *scheduler.Scheduler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sched = sched
}

// SetWorkflowRunStore wires the WorkflowRunStore.
func (s *Server) SetWorkflowRunStore(store scheduler.WorkflowRunStoreInterface) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workflowRunStore = store
}

// SetMuninnConfigPath sets the path to the muninn.json global config.
func (s *Server) SetMuninnConfigPath(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.muninnCfgPath = path
	if s.orch != nil {
		s.orch.SetMuninnConfigPath(path)
	}
}

// SetSpaceStore wires the Space store used by the /api/v1/spaces endpoints.
func (s *Server) SetSpaceStore(store spaces.StoreInterface) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spaceStore = store
}

// SetStatsRegistry wires the stats registry for the /api/v1/metrics endpoint
// and initialises the Prometheus collector for /api/v1/metrics/prometheus.
func (s *Server) SetStatsRegistry(reg *stats.Registry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statsReg = reg
	s.prometheusSt = initPromState(reg)
}

// SetStatsPersister wires the stats persister for periodic SQLite flushing.
// Must be called before Start() if persistence is required.
func (s *Server) SetStatsPersister(p *stats.Persister) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statsPersister = p
}

// StartAuditLog creates and starts the audit logger backed by db.
// A no-op when db is nil. Must be called before Start().
func (s *Server) StartAuditLog(db *sqlitedb.DB) {
	if db == nil {
		return
	}
	a := newAuditLogger(db)
	s.mu.Lock()
	s.auditLog = a
	s.mu.Unlock()
}

// SetWorkstreamStore wires the workstream store for the /api/v1/workstreams endpoints.
func (s *Server) SetWorkstreamStore(store workstreamStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workstreamStore = store
}

// MakeThreadEventEmitter returns an EventEmitter that broadcasts thread lifecycle
// events as "thread_event" envelope messages to connected browser WebSocket clients.
// The emitter is session-scoped: each ThreadEvent's SessionID is used to route
// the broadcast only to clients subscribed to that session.
//
// Additionally, when the thread event represents a delegation terminal state
// ("completed" or "error"), the emitter looks up the matching delegation record
// and broadcasts a "thread_update" event so the frontend's delegation history
// panel can update in real-time without polling.
func (s *Server) MakeThreadEventEmitter() *threadmgr.EventEmitter {
	return threadmgr.NewEventEmitter(func(ev threadmgr.ThreadEvent) {
		if s.wsHub == nil || ev.SessionID == "" {
			return
		}
		s.wsHub.broadcastToSession(ev.SessionID, WSMessage{
			Type: "thread_event",
			Payload: map[string]any{
				"event":            ev.Event,
				"thread_id":        ev.ThreadID,
				"agent_id":         ev.AgentID,
				"task":             ev.Task,
				"space_id":         ev.SpaceID,
				"child_session_id": ev.ChildSessionID,
				"text":             ev.Text,
			},
		})

		// Track delegation status transitions in the database and push a
		// real-time "thread_update" event to subscribed browser clients.
		if s.delegationStore != nil && ev.ThreadID != "" {
			switch ev.Event {
			case "started":
				if d, err := s.delegationStore.FindDelegationByThread(ev.ThreadID); err == nil {
					now := time.Now().UTC()
					if err := s.delegationStore.UpdateDelegationStatus(d.ID, "in_progress", "", &now, nil); err != nil {
						log.Printf("warn: delegation: UpdateDelegationStatus %s in_progress: %v", d.ID, err)
					} else {
						s.wsHub.broadcastToSession(ev.SessionID, WSMessage{
							Type: "thread_update",
							Payload: map[string]any{
								"delegation_id": d.ID,
								"thread_id":     ev.ThreadID,
								"status":        "in_progress",
								"result":        "",
							},
						})
					}
				}
			case "completed", "error":
				status := "completed"
				if ev.Event == "error" {
					status = "failed"
				}
				if d, err := s.delegationStore.FindDelegationByThread(ev.ThreadID); err == nil {
					now := time.Now().UTC()
					result := ev.Text
					if err := s.delegationStore.UpdateDelegationStatus(d.ID, status, result, nil, &now); err != nil {
						log.Printf("warn: delegation: UpdateDelegationStatus %s %s: %v", d.ID, status, err)
					} else {
						s.wsHub.broadcastToSession(ev.SessionID, WSMessage{
							Type: "thread_update",
							Payload: map[string]any{
								"delegation_id": d.ID,
								"thread_id":     ev.ThreadID,
								"status":        status,
								"result":        result,
							},
						})
					}
				}
			}
		}
	})
}

// saveConfig persists cfg to disk. When s.configPath is set (tests), it writes
// to that path instead of the default ~/.huginn/config.json.
func (s *Server) saveConfig(cfg *config.Config) error {
	if s.configPath != "" {
		return cfg.SaveTo(s.configPath)
	}
	return cfg.Save()
}

// storeAPIKey stores an API key in the OS keychain (or test double).
// Returns the keyring reference string (e.g. "keyring:huginn:anthropic").
func (s *Server) storeAPIKey(slot, value string) (string, error) {
	if s.keyStorerFn != nil {
		return s.keyStorerFn(slot, value)
	}
	return backend.StoreAPIKey(slot, value)
}

func (s *Server) handleCloudCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code parameter", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	reg := s.cloudRegistrar
	s.mu.Unlock()
	if reg != nil {
		reg.DeliverCode(code)
	}
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<html><body><h1>Registration complete</h1><p>You may close this tab.</p></body></html>`)
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	// withMaxBody applies a per-endpoint body size limit. Applied before the
	// global 10 MiB cap so the more restrictive limit wins.
	withMaxBody := func(limit int64, h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			h(w, r)
		}
	}

	// api wraps a handler with logging, request-ID, auth, and body-size middleware.
	// Request IDs are set before auth so that 401 responses also carry a trace ID.
	// Body size is capped at 10 MiB to prevent DoS from excessively large payloads.
	api := func(h http.HandlerFunc) http.HandlerFunc {
		return loggingMiddleware(requestIDMiddleware(s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10 MiB
			h(w, r)
		})))
	}

	// Unauthenticated endpoints — safe because server binds to 127.0.0.1 only.
	mux.HandleFunc("GET /api/v1/token", loggingMiddleware(s.handleGetToken))
	mux.HandleFunc("GET /api/v1/health", loggingMiddleware(requestIDMiddleware(s.handleHealth)))

	// REST API (auth required)
	mux.HandleFunc("GET /api/v1/sessions", api(s.handleListSessions))
	mux.HandleFunc("GET /api/v1/sessions/search", api(s.handleSearchSessions))
	mux.HandleFunc("POST /api/v1/sessions", api(s.rateLimitMiddleware(func() *endpointRateLimiter { return s.sessionCreateLimiter }, s.handleCreateSession)))
	mux.HandleFunc("GET /api/v1/sessions/{id}", api(s.handleGetSession))
	mux.HandleFunc("PATCH /api/v1/sessions/{id}", api(s.handleUpdateSession))
	mux.HandleFunc("DELETE /api/v1/sessions/{id}", api(s.handleDeleteSession))
	mux.HandleFunc("GET /api/v1/sessions/{id}/threads", api(s.handleListThreads))
	mux.HandleFunc("GET /api/v1/sessions/{id}/messages", api(s.handleGetMessages))
	mux.HandleFunc("POST /api/v1/sessions/{id}/messages", api(s.rateLimitMiddleware(func() *endpointRateLimiter { return s.mutationLimiter }, withMaxBody(50<<10, s.handleSendMessage))))
	mux.HandleFunc("POST /api/v1/sessions/{id}/chat/stream", api(s.handleChatStream))
	mux.HandleFunc("GET /api/v1/agents", api(s.handleListAgents))
	mux.HandleFunc("GET /api/v1/agents/{name}", api(s.handleGetAgent))
	mux.HandleFunc("PUT /api/v1/agents/{name}", api(withMaxBody(1<<20, s.handleUpdateAgent)))
	mux.HandleFunc("DELETE /api/v1/agents/{name}", api(s.handleDeleteAgent))
	mux.HandleFunc("POST /api/v1/agents/{name}/vault/test", api(s.handleVaultTest))
	mux.HandleFunc("GET /api/v1/agents/{name}/vault-status", api(s.handleAgentVaultHealth))
	mux.HandleFunc("GET /api/v1/models", api(s.handleListModels))
	mux.HandleFunc("GET /api/v1/models/available", api(s.handleListAvailableModels))
	mux.HandleFunc("POST /api/v1/models/pull", api(s.handlePullModel))
	mux.HandleFunc("DELETE /api/v1/models/{name}", api(s.handleDeleteOllamaModel))
	mux.HandleFunc("GET /api/v1/runtime/status", api(s.handleRuntimeStatus))
	mux.HandleFunc("GET /api/v1/builtin/status",      api(s.handleBuiltinStatus))
	mux.HandleFunc("POST /api/v1/builtin/download",    api(s.handleBuiltinDownload))
	mux.HandleFunc("GET /api/v1/builtin/models",       api(s.handleBuiltinListModels))
	mux.HandleFunc("GET /api/v1/builtin/catalog",      api(s.handleBuiltinCatalog))
	mux.HandleFunc("POST /api/v1/builtin/models/pull",   api(s.handleBuiltinPullModel))
	mux.HandleFunc("POST /api/v1/builtin/activate",      api(s.handleBuiltinActivate))
	mux.HandleFunc("DELETE /api/v1/builtin/models/{name}", api(s.handleBuiltinDeleteModel))
	mux.HandleFunc("GET /api/v1/stats", api(s.handleStats))
	mux.HandleFunc("GET /api/v1/stats/history", api(s.handleStatsHistory))
	mux.HandleFunc("GET /api/v1/metrics/prometheus", s.handlePrometheusMetrics)
	mux.HandleFunc("GET /api/v1/cost", api(s.handleCost))
	mux.HandleFunc("GET /api/v1/logs", api(s.handleLogs))
	mux.HandleFunc("GET /api/v1/config", api(s.handleGetConfig))
	mux.HandleFunc("PUT /api/v1/config", api(s.handleUpdateConfig))

	// Secrets API (authenticated)
	mux.HandleFunc("GET /api/v1/secrets",          api(s.handleGetSecrets))
	mux.HandleFunc("PUT /api/v1/secrets/{slot}",   api(s.handleSetSecret))
	mux.HandleFunc("DELETE /api/v1/secrets/{slot}", api(s.handleDeleteSecret))

	// Active state API (authenticated)
	mux.HandleFunc("GET /api/v1/active-state",               api(s.handleActiveState))
	mux.HandleFunc("POST /api/v1/active-state/restore",      api(s.handleRestoreActiveState))
	mux.HandleFunc("GET /api/v1/sessions/{id}/active-state", api(s.handleSessionActiveState))

	// Artifacts API (authenticated)
	mux.HandleFunc("GET /api/v1/sessions/{id}/artifacts",                    api(s.handleListArtifacts))
	mux.HandleFunc("POST /api/v1/sessions/{id}/artifacts",                   api(s.handleCreateArtifact))
	mux.HandleFunc("GET /api/v1/sessions/{id}/artifacts/{artifact_id}",      api(s.handleGetArtifact))
	mux.HandleFunc("PUT /api/v1/sessions/{id}/artifacts/{artifact_id}",      api(s.handleUpdateArtifact))
	mux.HandleFunc("DELETE /api/v1/sessions/{id}/artifacts/{artifact_id}",   api(s.handleDeleteArtifact))
	mux.HandleFunc("GET /api/v1/sessions/{id}/artifacts/{artifact_id}/download", api(s.handleDownloadArtifact))
	mux.HandleFunc("POST /api/v1/artifacts",              api(s.handleWorkforceCreateArtifact))
	mux.HandleFunc("GET /api/v1/artifacts/{id}",          api(s.handleWorkforceGetArtifact))
	mux.HandleFunc("PATCH /api/v1/artifacts/{id}/status", api(s.handleWorkforceUpdateArtifactStatus))
	mux.HandleFunc("GET /api/v1/agents/{name}/artifacts", api(s.handleWorkforceListAgentArtifacts))

	// Threads API — message thread and container thread queries (authenticated)
	mux.HandleFunc("GET /api/v1/messages/{id}/thread",             api(s.handleGetMessageThread))
	mux.HandleFunc("GET /api/v1/containers/{id}/threads",          api(s.handleGetContainerThreads))
	mux.HandleFunc("POST /api/v1/sessions/{id}/threads",           api(s.handleCreateThread))
	mux.HandleFunc("GET /api/v1/sessions/{id}/threads/{thread_id}", api(s.handleGetThread))
	mux.HandleFunc("POST /api/v1/sessions/{id}/threads/{thread_id}/reply", api(s.handleReplyThread))
	mux.HandleFunc("DELETE /api/v1/sessions/{id}/threads/{thread_id}", api(s.handleCancelThread))
	mux.HandleFunc("POST /api/v1/sessions/{id}/threads/{thread_id}/archive", api(s.handleArchiveThread))

	// Delegation History API (authenticated)
	mux.HandleFunc("GET /api/v1/sessions/{id}/delegations",                       api(s.handleListDelegations))
	mux.HandleFunc("GET /api/v1/sessions/{id}/delegations/{delegation_id}",       api(s.handleGetDelegation))

	// Workstreams API (authenticated)
	mux.HandleFunc("GET /api/v1/workstreams",                        api(s.handleListWorkstreams))
	mux.HandleFunc("POST /api/v1/workstreams",                       api(s.handleCreateWorkstream))
	mux.HandleFunc("GET /api/v1/workstreams/{id}",                   api(s.handleGetWorkstream))
	mux.HandleFunc("DELETE /api/v1/workstreams/{id}",                api(s.handleDeleteWorkstream))
	mux.HandleFunc("POST /api/v1/workstreams/{id}/sessions",         api(s.handleTagWorkstreamSession))
	mux.HandleFunc("GET /api/v1/workstreams/{id}/sessions",          api(s.handleListWorkstreamSessions))

	// Workflows API
	mux.HandleFunc("GET /api/v1/workflows",             api(s.handleListWorkflows))
	mux.HandleFunc("POST /api/v1/workflows",            api(s.handleCreateWorkflow))
	mux.HandleFunc("GET /api/v1/workflows/templates",   api(s.handleListWorkflowTemplates))
	mux.HandleFunc("GET /api/v1/workflows/{id}",        api(s.handleGetWorkflow))
	mux.HandleFunc("PUT /api/v1/workflows/{id}",        api(s.handleUpdateWorkflow))
	mux.HandleFunc("DELETE /api/v1/workflows/{id}",     api(s.handleDeleteWorkflow))
	mux.HandleFunc("POST /api/v1/workflows/{id}/run",    api(s.rateLimitMiddleware(func() *endpointRateLimiter { return s.workflowRunLimiter }, s.handleRunWorkflow)))
	mux.HandleFunc("POST /api/v1/workflows/{id}/cancel", api(s.handleCancelWorkflow))
	mux.HandleFunc("GET /api/v1/workflows/{id}/runs",           api(s.handleListWorkflowRuns))
	mux.HandleFunc("GET /api/v1/workflows/{id}/runs/{run_id}", api(s.handleGetWorkflowRun))
	mux.HandleFunc("GET /api/v1/delivery-failures",      api(s.handleListDeliveryFailures))
	mux.HandleFunc("POST /api/v1/delivery-failures/retry", api(s.handleRetryDeliveryFailure))

	// Notifications API
	mux.HandleFunc("GET /api/v1/notifications",              api(s.handleListNotifications))
	mux.HandleFunc("GET /api/v1/notifications/{id}",         api(s.handleGetNotification))
	mux.HandleFunc("POST /api/v1/notifications/{id}/action", api(s.handleNotificationAction))
	mux.HandleFunc("GET /api/v1/inbox/summary",              api(s.handleInboxSummary))

	// Code Intelligence API (authenticated)
	mux.HandleFunc("GET /api/v1/symbols/search",          api(s.handleSymbolSearch))
	mux.HandleFunc("GET /api/v1/symbols/impact/{symbol}", api(s.handleSymbolImpact))

	// OAuth callback (no auth — provider redirects here for local flows)
	mux.HandleFunc("GET /oauth/callback", s.handleOAuthCallback)

	// OAuth relay callback (no auth — HuginnCloud broker redirects here via broker flow)
	mux.HandleFunc("GET /oauth/relay", s.handleOAuthRelay)

	// Cloud registration callback (no auth — HuginnCloud redirects here)
	mux.HandleFunc("GET /cloud/callback", s.handleCloudCallback)

	// Connections API (authenticated)
	mux.HandleFunc("GET /api/v1/connections", api(s.handleListConnections))
	mux.HandleFunc("PUT /api/v1/connections/{id}/default", api(s.handleSetDefaultConnection))
	mux.HandleFunc("GET /api/v1/providers", api(s.handleListProviders))
	mux.HandleFunc("POST /api/v1/connections/start", api(s.handleStartOAuth))
	mux.HandleFunc("POST /api/v1/connections/oauth/relay", api(s.handleOAuthRelayFromCloud))
	mux.HandleFunc("DELETE /api/v1/connections/{id}", api(s.handleDeleteConnection))

	// Credentials API — save + test for API-key providers (authenticated)
	// All credential endpoints are capped at 100 KB.
	credBody := func(h http.HandlerFunc) http.HandlerFunc { return api(withMaxBody(100<<10, h)) }
	mux.HandleFunc("POST /api/v1/credentials/datadog",      credBody(s.handleSaveDatadogCredentials))
	mux.HandleFunc("POST /api/v1/credentials/datadog/test", credBody(s.handleTestDatadogCredentials))
	mux.HandleFunc("POST /api/v1/credentials/splunk",       credBody(s.handleSaveSplunkCredentials))
	mux.HandleFunc("POST /api/v1/credentials/splunk/test",  credBody(s.handleTestSplunkCredentials))
	// Tier-2: PagerDuty
	mux.HandleFunc("POST /api/v1/credentials/pagerduty",      credBody(s.handleSavePagerDutyCredentials))
	mux.HandleFunc("POST /api/v1/credentials/pagerduty/test", credBody(s.handleTestPagerDutyCredentials))
	// Tier-2: New Relic
	mux.HandleFunc("POST /api/v1/credentials/newrelic",      credBody(s.handleSaveNewRelicCredentials))
	mux.HandleFunc("POST /api/v1/credentials/newrelic/test", credBody(s.handleTestNewRelicCredentials))
	// Tier-2: Elastic
	mux.HandleFunc("POST /api/v1/credentials/elastic",      credBody(s.handleSaveElasticCredentials))
	mux.HandleFunc("POST /api/v1/credentials/elastic/test", credBody(s.handleTestElasticCredentials))
	// Tier-2: Grafana
	mux.HandleFunc("POST /api/v1/credentials/grafana",      credBody(s.handleSaveGrafanaCredentials))
	mux.HandleFunc("POST /api/v1/credentials/grafana/test", credBody(s.handleTestGrafanaCredentials))
	// Tier-2: CrowdStrike
	mux.HandleFunc("POST /api/v1/credentials/crowdstrike",      credBody(s.handleSaveCrowdStrikeCredentials))
	mux.HandleFunc("POST /api/v1/credentials/crowdstrike/test", credBody(s.handleTestCrowdStrikeCredentials))
	// Tier-2: Terraform Cloud
	mux.HandleFunc("POST /api/v1/credentials/terraform",      credBody(s.handleSaveTerraformCredentials))
	mux.HandleFunc("POST /api/v1/credentials/terraform/test", credBody(s.handleTestTerraformCredentials))
	// Tier-2: ServiceNow
	mux.HandleFunc("POST /api/v1/credentials/servicenow",      credBody(s.handleSaveServiceNowCredentials))
	mux.HandleFunc("POST /api/v1/credentials/servicenow/test", credBody(s.handleTestServiceNowCredentials))
	// Tier-2: Notion
	mux.HandleFunc("POST /api/v1/credentials/notion",      credBody(s.handleSaveNotionCredentials))
	mux.HandleFunc("POST /api/v1/credentials/notion/test", credBody(s.handleTestNotionCredentials))
	// Tier-2: Airtable
	mux.HandleFunc("POST /api/v1/credentials/airtable",      credBody(s.handleSaveAirtableCredentials))
	mux.HandleFunc("POST /api/v1/credentials/airtable/test", credBody(s.handleTestAirtableCredentials))
	// Tier-2: HubSpot
	mux.HandleFunc("POST /api/v1/credentials/hubspot",      credBody(s.handleSaveHubSpotCredentials))
	mux.HandleFunc("POST /api/v1/credentials/hubspot/test", credBody(s.handleTestHubSpotCredentials))
	// Tier-2: Zendesk
	mux.HandleFunc("POST /api/v1/credentials/zendesk",      credBody(s.handleSaveZendeskCredentials))
	mux.HandleFunc("POST /api/v1/credentials/zendesk/test", credBody(s.handleTestZendeskCredentials))
	// Tier-2: Asana
	mux.HandleFunc("POST /api/v1/credentials/asana",      credBody(s.handleSaveAsanaCredentials))
	mux.HandleFunc("POST /api/v1/credentials/asana/test", credBody(s.handleTestAsanaCredentials))
	// Tier-2: Monday.com
	mux.HandleFunc("POST /api/v1/credentials/monday",      credBody(s.handleSaveMondayCredentials))
	mux.HandleFunc("POST /api/v1/credentials/monday/test", credBody(s.handleTestMondayCredentials))

	// Integrations API (authenticated)
	mux.HandleFunc("GET /api/v1/integrations/cli-status", api(s.handleCLIStatus))

	// System tools detection (authenticated)
	mux.HandleFunc("GET /api/v1/system/tools", api(s.handleSystemTools))
	mux.HandleFunc("POST /api/v1/system/github/switch", api(s.handleGitHubSwitch))

	// HuginnCloud satellite status (authenticated)
	mux.HandleFunc("GET /api/v1/cloud/status", api(s.handleCloudStatus))
	mux.HandleFunc("POST /api/v1/cloud/connect", api(s.handleCloudConnect))
	mux.HandleFunc("DELETE /api/v1/cloud/connect", api(s.handleCloudDisconnect))

	// MuninnDB proxy API (authenticated)
	mux.HandleFunc("GET /api/v1/muninn/status",   api(s.handleMuninnStatus))
	mux.HandleFunc("POST /api/v1/muninn/test",    api(s.handleMuninnTest))
	mux.HandleFunc("POST /api/v1/muninn/connect", api(s.handleMuninnConnect))
	mux.HandleFunc("GET /api/v1/muninn/vaults",   api(s.handleMuninnVaultsList))
	mux.HandleFunc("POST /api/v1/muninn/vaults",  api(s.handleMuninnVaultCreate))

	// Spaces API (authenticated)
	// NOTE: route ordering matters in Go 1.22+ ServeMux.
	// Literal-segment routes take priority over wildcard routes.
	// "GET /api/v1/spaces/dm/{agent}" has a literal "dm" segment, so it is
	// more specific than "GET /api/v1/spaces/{id}" for /spaces/dm/... paths.
	// "GET /api/v1/spaces/{spaceID}/sessions" uses a distinct wildcard name
	// to avoid ambiguity with the dm route at registration time.
	mux.HandleFunc("GET /api/v1/spaces", api(s.handleListSpaces))
	mux.HandleFunc("POST /api/v1/spaces", api(s.rateLimitMiddleware(func() *endpointRateLimiter { return s.spaceCreateLimiter }, s.handleCreateSpace)))
	// NOTE: "GET /api/v1/spaces/dm/{agent}" has a literal "dm" segment and is
	// more specific than "GET /api/v1/spaces/{id}" for any /spaces/dm/... path.
	// Go 1.22+ ServeMux resolves this correctly (literal beats wildcard).
	mux.HandleFunc("GET /api/v1/spaces/dm/{agent}", api(s.handleGetOrCreateDM))
	mux.HandleFunc("GET /api/v1/spaces/{id}", api(s.handleGetSpace))
	mux.HandleFunc("PATCH /api/v1/spaces/{id}", api(s.handleUpdateSpace))
	mux.HandleFunc("DELETE /api/v1/spaces/{id}", api(s.handleDeleteSpace))
	mux.HandleFunc("POST /api/v1/spaces/{id}/mark-read", api(s.handleMarkSpaceRead))
	// NOTE: "GET /api/v1/spaces/{id}/sessions" would conflict with
	// "GET /api/v1/spaces/dm/{agent}" in Go 1.22+ ServeMux because the path
	// /spaces/dm/sessions matches both patterns. We use a distinct sub-resource
	// prefix "space-sessions" to avoid the ambiguity without changing the
	// semantic of the endpoint.
	mux.HandleFunc("GET /api/v1/space-sessions/{id}", api(s.handleListSpaceSessions))
	// NOTE: "GET /api/v1/spaces/{id}/messages" would conflict with the dm route
	// for the path /spaces/dm/messages (literal "dm" beats wildcard "{id}").
	// We use the "space-messages" prefix to avoid the ambiguity, mirroring
	// the "space-sessions" pattern used for the sessions endpoint above.
	mux.HandleFunc("GET /api/v1/space-messages/{id}", api(s.handleListSpaceMessages))

	// Skills API (authenticated)
	// Place specific literal routes before wildcard routes for clarity
	mux.HandleFunc("GET /api/v1/skills/registry/search", api(s.handleSkillsRegistrySearch))
	mux.HandleFunc("GET /api/v1/skills/registry/index", api(s.handleSkillsRegistryIndex))
	mux.HandleFunc("POST /api/v1/skills/install", api(s.handleSkillsInstall))
	mux.HandleFunc("GET /api/v1/skills", api(s.handleSkillsList))
	mux.HandleFunc("POST /api/v1/skills", api(s.handleSkillsCreate))
	mux.HandleFunc("GET /api/v1/skills/{name}", api(s.handleSkillsGet))
	mux.HandleFunc("PUT /api/v1/skills/{name}", api(withMaxBody(10<<20, s.handleSkillsUpdate)))
	mux.HandleFunc("POST /api/v1/skills/{name}/execute", api(s.handleSkillsExecute))
	mux.HandleFunc("PUT /api/v1/skills/{name}/enable", api(s.handleSkillsEnable))
	mux.HandleFunc("PUT /api/v1/skills/{name}/disable", api(s.handleSkillsDisable))
	mux.HandleFunc("DELETE /api/v1/skills/{name}", api(s.handleSkillsDelete))

	// WebSocket
	mux.HandleFunc("GET /ws", s.handleWebSocket)

	// Frontend SPA -- catch-all (must be last)
	mux.Handle("/", http.FileServer(staticFS()))
}
