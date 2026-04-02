package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/scrypster/huginn/internal/agent/session"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/compact"
	mem "github.com/scrypster/huginn/internal/memory"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/relay"
	"github.com/scrypster/huginn/internal/repo"
	huginsession "github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/skills"
	"github.com/scrypster/huginn/internal/stats"
	"github.com/scrypster/huginn/internal/tools"
)

// sessionIDCtxKey is an unexported context key type to avoid collisions.
type sessionIDCtxKey struct{}

// SetSessionID returns a new context with the given session ID attached.
// Used by the WS handler to propagate session ID through tool calls.
func SetSessionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, sessionIDCtxKey{}, id)
}

// GetSessionID retrieves the session ID set by SetSessionID. Returns "" if not set.
func GetSessionID(ctx context.Context) string {
	v, _ := ctx.Value(sessionIDCtxKey{}).(string)
	return v
}

type parentMessageIDCtxKey struct{}

// SetParentMessageID returns a new context with the given parent message ID attached.
// Used by the WS handler so the delegate_to_agent tool can thread replies under
// the user message that triggered delegation.
func SetParentMessageID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, parentMessageIDCtxKey{}, id)
}

// GetParentMessageID retrieves the parent message ID set by SetParentMessageID. Returns "" if not set.
func GetParentMessageID(ctx context.Context) string {
	v, _ := ctx.Value(parentMessageIDCtxKey{}).(string)
	return v
}

// State represents the orchestrator's current phase.
type State int

const (
	StateIdle      State = iota // Waiting for user input
	StateIterating              // Iteration loop running
	StateAgentLoop              // Running agentic tool loop
)

// defaultSessionIdleTTL is the default duration after which an idle (not running)
// session is evicted from the in-memory sessions map by startSessionCleanup.
const defaultSessionIdleTTL = 2 * time.Hour

// Orchestrator manages agent-loop conversations and iterative refinement.
type Orchestrator struct {
	mu               sync.RWMutex
	backend          backend.Backend
	models           *modelconfig.Models
	idx              *repo.Index
	registry         *modelconfig.ModelRegistry
	sessions         map[string]*Session
	defaultSessionID string
	contextBuilder   *ContextBuilder
	toolRegistry     *tools.Registry
	permGate         *permissions.Gate
	agentReg         *agents.AgentRegistry
	sc               stats.Collector
	machineID        string
	memoryStore      agents.MemoryStoreIface
	relayHub         relay.Hub // nil = InProcessHub (default behavior)
	compactor        *compact.Compactor
	backendCache     *backend.BackendCache
	muninnCfgPath    string // set by SetMuninnConfigPath; path to ~/.config/huginn/muninn.json
	workspaceRoot    string // set by SetGitRoot; used to load .huginn.md project instructions
	huginnHome       string // set by SetHuginnHome; used to locate agent memory files
	skillsReg        *skills.SkillRegistry // set by SetSkillsRegistry; used for per-agent skill injection

	// defaultModel is the fallback model name when no agent registry is configured.
	defaultModel string

	// sessionStore is the persistent session store for history hydration.
	sessionStore huginsession.StoreInterface

	// memoryReplicator handles channel vault replication (nil = disabled).
	memoryReplicator *MemoryReplicator

	// wsBroadcast emits typed WS events to browser clients (nil = disabled).
	wsBroadcast func(sessionID, msgType string, payload map[string]any)

	// spaceID and spaceName identify the channel/space this orchestrator serves.
	spaceID   string
	spaceName string

	// maxDelegationDepthCfg caps agent-to-agent delegation chains (0 = default 5).
	maxDelegationDepthCfg int

	// memoryPrefetchCache caches MuninnDB memory briefing results per agent/session key.
	memoryPrefetchCache *prefetchCache

	// semanticPrefetchCache caches semantic search results per query key.
	semanticPrefetchCache *prefetchCache

	// SessionIdleTTL controls how long an inactive (not running) session stays in
	// memory before being evicted by the cleanup goroutine. Default: 2 hours.
	// Active sessions (running > 0) are never evicted regardless of this value.
	SessionIdleTTL time.Duration

	lastUsagePrompt     atomic.Int64
	lastUsageCompletion atomic.Int64
}

// NewOrchestrator creates an Orchestrator ready for use.
func NewOrchestrator(b backend.Backend, models *modelconfig.Models, idx *repo.Index, registry *modelconfig.ModelRegistry, sc stats.Collector, compactor *compact.Compactor) (*Orchestrator, error) {
	if sc == nil {
		sc = stats.NoopCollector{}
	}
	sessionID, err := generateSessionID()
	if err != nil {
		return nil, err
	}
	sess := newSession(sessionID)
	return &Orchestrator{
		backend:          b,
		models:           models,
		idx:              idx,
		registry:         registry,
		sessions:         map[string]*Session{sess.ID: sess},
		defaultSessionID: sess.ID,
		contextBuilder:   NewContextBuilder(idx, registry, sc),
		sc:               sc,
		compactor:        compactor,
	}, nil
}

// StartSessionCleanup starts a background goroutine that evicts idle sessions from
// the sessions map every 10 minutes. A session is eligible for eviction when:
//   - it is not the default session
//   - it has not been active (running == 0) for longer than SessionIdleTTL (default 2h)
//
// The goroutine stops when ctx is cancelled.
func (o *Orchestrator) StartSessionCleanup(ctx context.Context) {
	ttl := o.SessionIdleTTL
	if ttl <= 0 {
		ttl = defaultSessionIdleTTL
	}
	// Evict sessions that already exceeded the TTL at startup (e.g. sessions that
	// survived a crash without being cleaned up).
	go o.evictIdleSessions(ttl)

	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				o.evictIdleSessions(ttl)
			}
		}
	}()
}

// evictIdleSessions removes sessions that have been idle for longer than ttl.
// It never removes the default session or any session with active goroutines.
func (o *Orchestrator) evictIdleSessions(ttl time.Duration) {
	now := time.Now()
	o.mu.Lock()
	defaultID := o.defaultSessionID
	// Collect candidates outside the lock after snapshot to avoid calling
	// session methods (which acquire sess.mu) while holding o.mu.
	type candidate struct {
		id   string
		sess *Session
	}
	var candidates []candidate
	for id, sess := range o.sessions {
		if id != defaultID {
			candidates = append(candidates, candidate{id, sess})
		}
	}
	o.mu.Unlock()

	var evicted []string
	for _, c := range candidates {
		if c.sess.isRunning() {
			continue
		}
		idle := now.Sub(c.sess.getLastUsed())
		if idle > ttl {
			evicted = append(evicted, c.id)
			slog.Info("orchestrator: evicting idle session",
				"session_id", c.id,
				"idle_for", idle.Round(time.Second))
		}
	}

	if len(evicted) == 0 {
		return
	}
	o.mu.Lock()
	for _, id := range evicted {
		delete(o.sessions, id)
	}
	o.mu.Unlock()

	// Cascade delete: remove evicted sessions from the persistent store so that
	// messages and history don't accumulate indefinitely on disk after the session
	// has been evicted from the in-memory registry.
	if o.sessionStore != nil {
		for _, id := range evicted {
			if delErr := o.sessionStore.Delete(id); delErr != nil {
				slog.Warn("agent: session eviction: failed to delete from store",
					"session_id", id, "err", delErr)
			}
		}
	}
}

// Iterate runs N refinement passes on a section using the reasoner model.
func (o *Orchestrator) Iterate(ctx context.Context, n int, section string, onToken func(string)) error {
	o.mu.RLock()
	sess := o.defaultSession()
	o.mu.RUnlock()
	sess.setState(StateIterating)

	defer sess.setState(StateIdle)

	o.mu.RLock()
	iterBackend := o.backend
	iterCache := o.backendCache
	o.mu.RUnlock()
	if iterCache != nil {
		if b, e := iterCache.For("", "", "", ""); e == nil {
			iterBackend = b
		}
	}

	stream := func(ctx context.Context, msgs []string, onTok func(string)) error {
		backendMsgs := make([]backend.Message, len(msgs))
		for i, m := range msgs {
			role := "user"
			if i%2 == 1 {
				role = "assistant"
			}
			backendMsgs[i] = backend.Message{Role: role, Content: m}
		}
		_, err := iterBackend.ChatCompletion(ctx, backend.ChatRequest{
			Model:    o.defaultModelName(),
			Messages: backendMsgs,
			OnToken:  onTok,
		})
		return err
	}

	result, err := iterate(ctx, n, section, stream)
	if err != nil {
		return err
	}
	if onToken != nil {
		onToken(result)
	}
	return nil
}

// CodeWithAgent runs the agent loop using the given agent's persona and model.
func (o *Orchestrator) CodeWithAgent(
	ctx context.Context,
	ag *agents.Agent,
	userMsg string,
	maxTurns int,
	onToken func(string),
	onToolCall func(string, map[string]any),
	onToolDone func(string, tools.ToolResult),
	onPermDenied func(string),
	onEvent func(backend.StreamEvent),
) error {
	o.mu.RLock()
	reg := o.toolRegistry
	gate := o.permGate
	sess := o.defaultSession()
	o.mu.RUnlock()
	sess.setState(StateAgentLoop)

	defer sess.setState(StateIdle)

	if reg == nil {
		return o.ChatWithAgent(ctx, ag, userMsg, GetSessionID(ctx), onToken, nil, onEvent)
	}

	// Connect to MuninnDB vault for this session — forks the shared registry so
	// vault tools are isolated per session. Always safe; degrades gracefully.
	vr := o.connectAgentVault(ctx, ag, reg)
	defer vr.cancel()

	if vr.warning != "" && onEvent != nil {
		onEvent(backend.StreamEvent{
			Type:    backend.StreamWarning,
			Content: fmt.Sprintf("\u26a0\ufe0f Memory vault unavailable: %s. Memory features are disabled for this session.", vr.warning),
		})
	}

	ctxText := o.contextBuilder.Build(userMsg, o.defaultModelName())
	recentSummaries := o.loadAgentSummaries(ctx, ag.Name)
	systemPrompt := agents.BuildPersonaPromptWithMemory(ag, ctxText, recentSummaries)
	// Inject memory mode instructions only when vault tools are available this session.
	if _, ok := vr.sessionReg.Get("muninn_recall"); ok {
		systemPrompt += memoryModeInstruction(ag.MemoryMode, ag.VaultName, ag.VaultDescription)
	}

	history := sess.snapshotHistory()

	messages := []backend.Message{{Role: "system", Content: systemPrompt}}
	messages = append(messages, history...)
	messages = append(messages, backend.Message{Role: "user", Content: userMsg})

	schemas, agentGate := applyToolbelt(ag, vr.sessionReg, gate)

	// Create isolated session environment for this agent run.
	agentSess, sessErr := session.BuildAndSetup(agentToolbelt(ag))
	if sessErr != nil {
		// Non-fatal: log warning but continue without session isolation.
		slog.Warn("agent session setup failed", "agent", ag.Name, "err", sessErr)
		agentSess = &session.Session{} // empty session
	}
	defer agentSess.Teardown()
	ctx = session.WithEnv(ctx, agentSess.Env)

	agCodeBackend, agCodeErr := o.backendFor(ag)
	if agCodeErr != nil {
		return agCodeErr
	}
	cfg := RunLoopConfig{
		MaxTurns:           maxTurns,
		Messages:           messages,
		Tools:              vr.sessionReg,
		ToolSchemas:        schemas,
		Gate:               agentGate,
		Backend:            agCodeBackend,
		ModelName:          ag.GetModelID(),
		OnToken:            onToken,
		OnToolCall:         onToolCall,
		OnToolDone:         onToolDone,
		OnPermissionDenied: onPermDenied,
		OnEvent:            onEvent,
		VaultReconnector:   vr.reconnector,
	}

	agentLoopStart := time.Now().UnixNano()
	loopResult, err := RunLoop(ctx, cfg)
	o.recordLLMLatency(agentLoopStart, "agent-loop")
	if err != nil {
		return fmt.Errorf("code(%s): %w", ag.Name, err)
	}

	initialCount := 1 + len(history) + 1 // system msg + history msgs + user msg
	if loopResult.Messages != nil && len(loopResult.Messages) > initialCount {
		sess.appendHistory(loopResult.Messages[initialCount:]...)
	} else {
		sess.appendHistory(
			backend.Message{Role: "user", Content: userMsg},
			backend.Message{Role: "assistant", Content: loopResult.FinalContent},
		)
	}
	o.compactHistory(ctx, sess)
	return nil
}

// notepadContextForAgent returns a mem.NotesPromptBlock for the default agent
// if ContextNotesEnabled is set. Used by AgentChat and ChatForSession.
func (o *Orchestrator) notepadContextForAgent(ag *agents.Agent) string {
	if ag == nil || !ag.ContextNotesEnabled {
		return ""
	}
	o.mu.RLock()
	home := o.huginnHome
	o.mu.RUnlock()
	if home == "" {
		return ""
	}
	return mem.NotesPromptBlock(home, ag.Name)
}
