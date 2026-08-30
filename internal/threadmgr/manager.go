package threadmgr

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
)

// SpaceMembershipChecker checks whether an agent is a member of a space.
// Returns (nil, nil) when the space is not found — callers treat that as deny-all.
type SpaceMembershipChecker interface {
	SpaceMembers(spaceID string) ([]string, error)
}

// DeskFloorChecker optionally widens desk-DM A2A to the desk floor.
// SQLiteSpaceStore implements this. Stubs that omit it keep SpaceMembers-only.
type DeskFloorChecker interface {
	DeskPeerNames() ([]string, error)
	SpaceIsDeskDM(spaceID string) (bool, error)
}

// CompanyGate looks up a space's company and whether an agent is seated there.
// Empty company ID means desk-level: callers keep today's space-roster check.
// SQLiteSpaceStore implements this.
type CompanyGate interface {
	SpaceCompanyID(spaceID string) (string, error)
	AgentInCompany(agent, companyID string) (bool, error)
}

// ErrAgentNotSpaceMember is returned by Create when a SpaceID is set and the
// requested agent is not a member of that space.
var ErrAgentNotSpaceMember = errors.New("agent is not a member of the space")

// ErrAgentNotInCompany is returned by Create when the space belongs to a
// company and the target agent is not seated in that company's roster.
var ErrAgentNotInCompany = errors.New("agent is not seated in this company")

// notInCompanyError is the human fail copy the lead agent (and bubble) see.
// Hover/diagnose may still wrap this with DELEGATE_FAIL; speech must not.
type notInCompanyError struct {
	Agent string
}

func (e *notInCompanyError) Error() string {
	name := strings.TrimSpace(e.Agent)
	if name == "" {
		name = "That agent"
	}
	return name + " isn't in this company."
}

func (e *notInCompanyError) Unwrap() error { return ErrAgentNotInCompany }

// ErrCyclicDependency is returned by Create when the requested DependsOn IDs
// would introduce a cycle in the thread dependency graph.
var ErrCyclicDependency = errors.New("cyclic dependency detected in thread DAG")

func newID() string {
	return session.NewID()
}

// DefaultMaxThreadsPerSession is the default cap on active threads per session.
// A rogue primary agent spawning excessive threads could exhaust goroutines and memory;
// this cap is enforced in Create() and returns ErrThreadLimitExceeded.
const DefaultMaxThreadsPerSession = 20

// maxAuditEntries is the maximum number of entries kept in the in-memory audit log.
// When the ring is full the oldest entry is overwritten (ring-buffer semantics).
const maxAuditEntries = 1000

// ErrThreadLimitExceeded is returned by Create when a session already has
// MaxThreadsPerSession active (non-terminal) threads.
var ErrThreadLimitExceeded = fmt.Errorf("thread limit exceeded (max %d active threads per session)", DefaultMaxThreadsPerSession)

// ErrThreadLimitReached is a user-facing variant of ErrThreadLimitExceeded with
// a clear, actionable message suitable for display in the UI or WS error events.
// Create() returns this error (wrapping ErrThreadLimitExceeded) when the cap is hit,
// so errors.Is(err, ErrThreadLimitExceeded) still returns true for backward compat.
var ErrThreadLimitReached = fmt.Errorf("threadmgr: session thread limit (%d) reached; complete or cancel existing threads to create new ones: %w", DefaultMaxThreadsPerSession, ErrThreadLimitExceeded)

// ThreadManager owns all thread lifecycle: create, start, cancel, complete.
// It is the single source of truth for thread state and the dependency DAG.
// All methods are safe for concurrent use.
type ThreadManager struct {
	mu      sync.RWMutex
	threads map[string]*Thread // threadID → Thread

	// store, if non-nil, persists thread state to durable storage (SQLite).
	// When nil the manager operates in in-memory-only mode (backward compat).
	store ThreadStore

	leaseMu   sync.Mutex
	fileLocks map[string]string // filePath → owning threadID

	// MaxThreadsPerSession caps the number of active (non-terminal) threads
	// per session. 0 means use DefaultMaxThreadsPerSession.
	MaxThreadsPerSession int

	// helpResolver, if set, automatically answers thread_help requests using
	// the primary agent. When nil, thread_help is broadcast to the frontend
	// for human input (existing behaviour).
	helpResolver HelpResolver

	// completionNotifier, if set, posts a brief natural-language summary in
	// the main chat after a thread finishes. When nil, no notification is sent.
	completionNotifier *CompletionNotifier

	// emitter, if set, receives ThreadEvents at lifecycle points (spawned,
	// started, completed, error, token) so they can be forwarded to the browser.
	// Nil-safe — no events are emitted when nil.
	emitter *EventEmitter

	// threadBus, if set, stores bounded sibling-context updates so delegated
	// threads can consume recent updates from other subthreads in the same session.
	threadBus *ThreadBus

	// proposalRegistry stores action proposals and scoped approval tokens used
	// by delegated threads when requesting approval for high-risk actions.
	proposalRegistry *ProposalRegistry

	// onCancelMu guards onCancel.
	onCancelMu sync.RWMutex
	// onCancel, if non-nil, is called after a thread transitions to
	// StatusCancelled. The callback receives the sessionID and threadID and
	// should be used to push a WS event to the connected client.
	// Set via SetOnCancel. Nil-safe.
	onCancel func(sessionID, threadID string)

	// statusChangeMu guards statusChangeHooks.
	statusChangeMu    sync.RWMutex
	statusChangeHooks []func(id string, status ThreadStatus)

	// backendFor, if set, resolves the correct backend for a given agent
	// (e.g. Anthropic vs Ollama). When nil, the raw backend passed to
	// SpawnThread is used for all agents. Set via SetBackendResolver.
	backendFor func(provider, endpoint, apiKey, model string) (backend.Backend, error)

	// toolRegistry, if set, provides agent-specific tool schemas and dispatch.
	// Sub-agent threads use this to build per-agent toolbelts filtered by the
	// agent's local_tools config. Set via SetToolRegistry.
	toolRegistry ToolRegistryIface

	// toolExecutor, if set, is the gate-wrapped executor for sub-agent tool
	// calls. Captures the permission gate at wiring time. Set via SetToolExecutor.
	toolExecutor ToolExecutorFn

	// runtimePreparer, if set, is invoked once when a thread spawns to build
	// the agent's per-thread execution context: tool schemas (toolbelt +
	// MuninnDB vault), gate-wrapped tool executor, persona prompt addendum,
	// and a cleanup callback. Per-agent runtime is what gives a delegated
	// worker its own MuninnDB tools, skills/connections-derived schemas, and
	// memory_mode prompt — instead of inheriting only the orchestrator's
	// global toolset. When nil the legacy toolRegistry/toolExecutor path is
	// used. Set via SetAgentRuntimePreparer.
	runtimePreparer AgentRuntimePreparer

	// memberChecker, if set, validates that the AgentID in CreateParams is a
	// member of the given SpaceID before creating the thread.
	memberChecker SpaceMembershipChecker

	// companyGate, if set, isolates A2A delegation to agents seated in the
	// space's company. Empty company_id keeps the space-roster check.
	companyGate CompanyGate

	// specialistCompany fixes the company a one-off ephemeral specialist
	// (spawned via spawn_specialist) may be delegated within, keyed by
	// lowercased agent name. Ephemeral specialists are never seated in a
	// company roster, so CompanyGate.AgentInCompany would always reject
	// them; Create() consults this map first and bypasses the roster
	// lookup for names present here. Fixed once at spawn time via
	// SetSpecialistCompany and cleared via ClearSpecialistCompany when the
	// specialist's thread lands terminal.
	specialistCompany map[string]string

	// specialistThreads maps a specialist's own thread ID to its (lowercased)
	// agent name and spawn time, so the internal terminal-status hook
	// (registered once in New()) knows which ephemeral overlay entry to
	// evict when that specific thread finishes. See RegisterSpecialistThread,
	// SetSpecialistEvictor, and EvictStaleSpecialists (TTL sweep fallback).
	specialistThreads map[string]specialistThreadEntry

	// specialistEvictFn, if set, is invoked with the specialist's agent name
	// and thread ID when its thread lands terminal (or on TTL sweep). Wired
	// once by the spawn_specialist tool's server-side wiring to call
	// AgentRegistry.UnregisterEphemeral and post the S13 finish line
	// ("<Name> is done and gone.") into the owning session — the threadID
	// lets that wiring call tm.Get(threadID) for the SessionID. Kept as a
	// callback (not an *agents.AgentRegistry field) to avoid growing
	// threadmgr's import surface for a single-purpose hook.
	specialistEvictFn func(agentName, threadID string)

	// auditMu guards auditLog.
	auditMu sync.Mutex
	// auditLog is a bounded ring-buffer of lifecycle events (max maxAuditEntries).
	auditLog []AuditEntry

	// graphDir, when non-empty, is the directory where session dependency graphs
	// are serialised to JSON for crash recovery. Set via SetGraphDir.
	graphDir string
}

// New returns a ready-to-use ThreadManager with default limits.
func New() *ThreadManager {
	tm := &ThreadManager{
		threads:              make(map[string]*Thread),
		fileLocks:            make(map[string]string),
		MaxThreadsPerSession: DefaultMaxThreadsPerSession,
		auditLog:             make([]AuditEntry, 0, maxAuditEntries),
		threadBus:            NewThreadBus(DefaultThreadBusCapacity),
		proposalRegistry:     NewProposalRegistry(),
		specialistCompany:    make(map[string]string),
		specialistThreads:    make(map[string]specialistThreadEntry),
	}
	tm.OnStatusChange(tm.handleSpecialistThreadStatusChange)
	return tm
}

// SetHelpResolver configures automatic help resolution for blocked threads.
// Pass nil to disable (human input required, existing behaviour).
func (tm *ThreadManager) SetHelpResolver(r HelpResolver) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.helpResolver = r
}

// SetCompletionNotifier configures the notifier that posts a chat message
// when a sub-agent thread completes. Pass nil to disable.
func (tm *ThreadManager) SetCompletionNotifier(n *CompletionNotifier) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.completionNotifier = n
}

// SetEventEmitter wires the EventEmitter that receives thread lifecycle events.
// Pass nil to disable event emission.
func (tm *ThreadManager) SetEventEmitter(e *EventEmitter) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.emitter = e
}

// SetThreadBus wires the sibling context bus. Pass nil to disable.
func (tm *ThreadManager) SetThreadBus(bus *ThreadBus) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.threadBus = bus
}

// SetProposalRegistry wires the proposal/token registry. Pass nil to disable.
func (tm *ThreadManager) SetProposalRegistry(reg *ProposalRegistry) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.proposalRegistry = reg
}

// SetOnCancel registers a callback that is invoked after a thread is
// cancelled. The callback receives the sessionID and threadID so the server
// can push a thread_cancelled WebSocket event to connected clients.
// Pass nil to clear the callback. Thread-safe.
func (tm *ThreadManager) SetOnCancel(fn func(sessionID, threadID string)) {
	tm.onCancelMu.Lock()
	tm.onCancel = fn
	tm.onCancelMu.Unlock()
}

// ToolRegistryIface is the minimal interface the thread manager needs from the tool registry.
// Using an interface avoids import cycles and keeps the package testable.
type ToolRegistryIface interface {
	// SchemasByNames returns backend.Tool schemas for the given tool names.
	// Used to build the per-agent toolbelt from the agent's config.
	SchemasByNames(names []string) []backend.Tool
	// AllBuiltinSchemas returns schemas for all tools tagged "builtin".
	// Used when an agent's local_tools is set to the wildcard ["*"].
	AllBuiltinSchemas() []backend.Tool
	// Execute runs the named tool with the given args. Returns result text and error.
	Execute(ctx context.Context, name string, args map[string]any) (string, error)
}

// ToolExecutorFn is a gate-wrapped tool executor set via SetToolExecutor.
// It captures the permission gate at wiring time so threadmgr stays free of
// the permissions package. In server mode the executor uses the auto-approve
// gate; future interactive modes can swap in a gate that prompts the user.
type ToolExecutorFn func(ctx context.Context, name string, args map[string]any) (string, error)

// AgentRuntime is the per-thread execution context produced by an
// AgentRuntimePreparer. It mirrors what the orchestrator builds for a primary
// chat session — vault-aware tool schemas, a gate-wrapped tool executor that
// dispatches against the agent's session-local registry fork, an addendum
// appended to the persona system prompt (memory_mode + memory_block), and a
// cleanup callback invoked when the thread completes (closes the MuninnDB
// MCP client and tears down the toolbelt environment).
//
// All fields are optional. A non-nil but otherwise empty AgentRuntime is
// equivalent to the legacy fallback path (no per-agent schemas/executor).
type AgentRuntime struct {
	// Schemas are the LLM tool definitions for this agent run. They replace
	// the legacy toolRegistry-derived schemas when the runtime is non-nil,
	// and are appended after the threadmgr-owned finish/request_help schemas.
	Schemas []backend.Tool

	// ExecuteTool dispatches a tool call against the agent's session-local
	// tool registry (vault tools + toolbelt providers + local builtins) with
	// the agent-specific permission gate applied. When non-nil it takes
	// precedence over the global toolExecutor for this thread.
	ExecuteTool ToolExecutorFn

	// ExtraSystem is appended to the persona system prompt (the first system
	// message produced by buildContext). Used to inject memory_mode
	// instructions and the agent's memory_block when the vault is reachable.
	ExtraSystem string

	// Cleanup is invoked exactly once when the thread goroutine exits. Use
	// it to close the per-thread MuninnDB MCP client and release any session
	// env state. Nil-safe.
	Cleanup func()
}

// AgentRuntimePreparer is invoked when a thread spawns to build the agent's
// per-thread execution context. Returning (nil, nil) opts the thread out of
// the per-agent path and falls back to the legacy global toolRegistry/
// toolExecutor wiring. Returning a non-nil error is logged and also falls
// back to the legacy path so that vault outages do not kill delegation.
type AgentRuntimePreparer func(ctx context.Context, agentName string) (*AgentRuntime, error)

// SetToolRegistry wires the tool registry used by sub-agent threads to obtain
// agent-specific tool schemas (bash, read_file, etc.).
func (tm *ThreadManager) SetToolRegistry(r ToolRegistryIface) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.toolRegistry = r
}

// SetToolExecutor wires the gate-wrapped executor used by sub-agent threads to
// dispatch real tool calls. The executor captures the permission gate at wiring
// time. If not set, sub-threads return "unknown tool" for any non-built-in call.
func (tm *ThreadManager) SetToolExecutor(fn ToolExecutorFn) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.toolExecutor = fn
}

// SetAgentRuntimePreparer wires the function invoked once per spawned thread
// to build the agent's per-thread execution context (vault-aware tool
// schemas, executor, prompt addendum, cleanup). Pass nil to disable. When
// disabled, threads use the global toolRegistry/toolExecutor and never see
// MuninnDB tools — that legacy path is retained for tests and for agents
// without memory enabled. Thread-safe.
func (tm *ThreadManager) SetAgentRuntimePreparer(fn AgentRuntimePreparer) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.runtimePreparer = fn
}

// SetMembershipChecker wires the SpaceMembershipChecker used to validate that
// an agent is a member of a space before a thread is created. Pass nil to disable.
func (tm *ThreadManager) SetMembershipChecker(c SpaceMembershipChecker) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.memberChecker = c
}

// SetCompanyGate wires the CompanyGate used to isolate A2A delegation to
// agents seated in the space's company. Pass nil to disable (desk-only).
func (tm *ThreadManager) SetCompanyGate(g CompanyGate) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.companyGate = g
}

// SetSpecialistCompany fixes the company an ephemeral specialist (spawned
// via spawn_specialist) may be delegated within, for the lifetime of its
// thread. companyID may be empty (desk-level specialist, no company). Call
// once at spawn time, before the spawning thread's Create() runs.
func (tm *ThreadManager) SetSpecialistCompany(agentName, companyID string) {
	if strings.TrimSpace(agentName) == "" {
		return
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if tm.specialistCompany == nil {
		tm.specialistCompany = make(map[string]string)
	}
	tm.specialistCompany[strings.ToLower(agentName)] = companyID
}

// ClearSpecialistCompany removes the fixed-company record for a specialist.
// Call when the specialist's thread lands terminal (or on TTL sweep
// eviction) so the entry does not linger after the overlay agent is gone.
func (tm *ThreadManager) ClearSpecialistCompany(agentName string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	delete(tm.specialistCompany, strings.ToLower(agentName))
}

// specialistCompanyLocked returns the fixed company for agentName and
// whether it is a known specialist. Caller must hold tm.mu (read or write).
func (tm *ThreadManager) specialistCompanyLocked(agentName string) (string, bool) {
	companyID, ok := tm.specialistCompany[strings.ToLower(agentName)]
	return companyID, ok
}

// specialistThreadEntry tracks which ephemeral specialist owns a thread, and
// when it was registered — the registeredAt timestamp backs the TTL sweep
// fallback (EvictStaleSpecialists) for threads whose terminal status hook
// never fires (e.g. the process crashes mid-thread).
type specialistThreadEntry struct {
	agentName    string
	registeredAt time.Time
}

// SetSpecialistEvictor wires the callback invoked with a specialist's agent
// name and thread ID when its thread lands terminal or is TTL-swept.
// Typically wired once at server startup to call
// AgentRegistry.UnregisterEphemeral and post the finish line. Pass nil to
// disable (specialists then linger in the overlay until process restart —
// tests that don't care about eviction may leave this unset).
func (tm *ThreadManager) SetSpecialistEvictor(fn func(agentName, threadID string)) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.specialistEvictFn = fn
}

// RegisterSpecialistThread records that threadID belongs to the ephemeral
// specialist agentName, so the terminal-status hook (and the TTL sweep)
// know to evict it. Call once, right after Create() returns the thread for
// a spawn_specialist call.
func (tm *ThreadManager) RegisterSpecialistThread(threadID, agentName string) {
	if strings.TrimSpace(threadID) == "" || strings.TrimSpace(agentName) == "" {
		return
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if tm.specialistThreads == nil {
		tm.specialistThreads = make(map[string]specialistThreadEntry)
	}
	tm.specialistThreads[threadID] = specialistThreadEntry{
		agentName:    strings.ToLower(agentName),
		registeredAt: time.Now(),
	}
}

// handleSpecialistThreadStatusChange is registered once (in New()) as an
// OnStatusChange hook. When a tracked specialist thread lands terminal, it
// evicts the specialist from the ephemeral overlay (via specialistEvictFn)
// and clears its fixed-company record — S5's "roster pollution: zero" and
// S12's fixed-at-spawn company both end together with the thread.
func (tm *ThreadManager) handleSpecialistThreadStatusChange(id string, status ThreadStatus) {
	if !isTerminalStatus(status) {
		return
	}
	tm.mu.Lock()
	entry, ok := tm.specialistThreads[id]
	if ok {
		delete(tm.specialistThreads, id)
		delete(tm.specialistCompany, entry.agentName)
	}
	evict := tm.specialistEvictFn
	tm.mu.Unlock()
	if ok && evict != nil {
		evict(entry.agentName, id)
	}
}

// isTerminalStatus reports whether status is a terminal thread status
// (mirrors the switch in activeThreadCountLocked).
func isTerminalStatus(status ThreadStatus) bool {
	switch status {
	case StatusDone, StatusCancelled, StatusError:
		return true
	default:
		return false
	}
}

// EvictStaleSpecialists sweeps specialistThreads for entries older than
// maxAge whose thread never fired the terminal-status hook (crash, stuck
// watchdog, etc. — precedent: server.go's swarmSnapshotTTL sweep). Evicted
// agent names are returned for logging; specialistEvictFn is invoked for
// each. Safe to call on a timer.
func (tm *ThreadManager) EvictStaleSpecialists(maxAge time.Duration) []string {
	now := time.Now()
	tm.mu.Lock()
	var stale []string
	type staleEntry struct{ name, threadID string }
	var staleEntries []staleEntry
	for id, entry := range tm.specialistThreads {
		if now.Sub(entry.registeredAt) > maxAge {
			delete(tm.specialistThreads, id)
			delete(tm.specialistCompany, entry.agentName)
			stale = append(stale, entry.agentName)
			staleEntries = append(staleEntries, staleEntry{entry.agentName, id})
		}
	}
	evict := tm.specialistEvictFn
	tm.mu.Unlock()
	if evict != nil {
		for _, e := range staleEntries {
			evict(e.name, e.threadID)
		}
	}
	return stale
}

// widenDeskDMMembers unions DeskPeerNames onto SpaceMembers for a desk DM.
// Channels and company-scoped spaces are unchanged. Missing DeskFloorChecker
// keeps today's SpaceMembers-only deny.
func widenDeskDMMembers(checker SpaceMembershipChecker, spaceID string, members []string) ([]string, error) {
	floor, ok := checker.(DeskFloorChecker)
	if !ok {
		return members, nil
	}
	isDeskDM, err := floor.SpaceIsDeskDM(spaceID)
	if err != nil {
		return nil, err
	}
	if !isDeskDM {
		return members, nil
	}
	peers, err := floor.DeskPeerNames()
	if err != nil {
		return nil, err
	}
	if len(peers) == 0 {
		return members, nil
	}
	seen := make(map[string]struct{}, len(members)+len(peers))
	out := make([]string, 0, len(members)+len(peers))
	for _, m := range members {
		key := strings.ToLower(strings.TrimSpace(m))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, m)
	}
	for _, m := range peers {
		key := strings.ToLower(strings.TrimSpace(m))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, m)
	}
	return out, nil
}

// SetBackendResolver wires a function that resolves the correct backend for
// a given agent (provider, endpoint, apiKey, model). When set, delegated
// threads use this to obtain an agent-specific backend (e.g. Anthropic for
// claude agents) rather than the raw fallback backend passed to SpawnThread.
func (tm *ThreadManager) SetBackendResolver(fn func(provider, endpoint, apiKey, model string) (backend.Backend, error)) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.backendFor = fn
}

// OnStatusChange registers a callback invoked whenever a thread transitions to a
// new status. Passing nil clears all previously registered callbacks.
// Callbacks are called without holding any lock and may be called from multiple
// goroutines concurrently. Returns a deregister function.
func (tm *ThreadManager) OnStatusChange(fn func(id string, status ThreadStatus)) func() {
	if fn == nil {
		// Passing nil clears all registered callbacks.
		tm.statusChangeMu.Lock()
		tm.statusChangeHooks = nil
		tm.statusChangeMu.Unlock()
		return func() {}
	}
	tm.statusChangeMu.Lock()
	tm.statusChangeHooks = append(tm.statusChangeHooks, fn)
	idx := len(tm.statusChangeHooks) - 1
	tm.statusChangeMu.Unlock()
	return func() {
		tm.statusChangeMu.Lock()
		defer tm.statusChangeMu.Unlock()
		if idx < len(tm.statusChangeHooks) {
			tm.statusChangeHooks[idx] = nil // nil-out to deregister
		}
	}
}

// fireStatusChange invokes all registered OnStatusChange hooks for a thread.
// Must be called outside the main mu lock to prevent deadlocks.
// Each hook is called in its own deferred-recover scope so a panicking hook
// does not prevent subsequent hooks from executing or kill the calling goroutine.
func (tm *ThreadManager) fireStatusChange(id string, status ThreadStatus) {
	tm.statusChangeMu.RLock()
	hooks := tm.statusChangeHooks
	tm.statusChangeMu.RUnlock()
	for _, h := range hooks {
		if h == nil {
			continue
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("threadmgr: panic in OnStatusChange hook",
						"thread_id", id, "status", status,
						"panic", r, "stack", string(debug.Stack()))
				}
			}()
			h(id, status)
		}()
	}
}

// activeThreadCountLocked returns the number of non-terminal threads for the
// given session. Caller must hold tm.mu (read or write lock).
func (tm *ThreadManager) activeThreadCountLocked(sessionID string) int {
	count := 0
	for _, t := range tm.threads {
		if t.SessionID != sessionID {
			continue
		}
		switch t.Status {
		case StatusDone, StatusCancelled, StatusError:
			// terminal — not counted
		default:
			count++
		}
	}
	return count
}

// ActiveCount returns the number of non-terminal (active) threads for the given
// session. It is the public, lock-safe counterpart to activeThreadCountLocked.
// Returns 0 for an empty sessionID.
func (tm *ThreadManager) ActiveCount(sessionID string) int {
	if sessionID == "" {
		return 0
	}
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.activeThreadCountLocked(sessionID)
}

// Create registers a new thread in the queued state and returns it.
// The thread is not started — call Start() when ready to run the goroutine.
// Returns ErrThreadLimitExceeded if MaxThreadsPerSession active threads already
// exist for the session, preventing runaway agent spawning.
func (tm *ThreadManager) Create(p CreateParams) (*Thread, error) {
	// Validate ParentMessageID to prevent path traversal or injection attacks.
	if p.ParentMessageID != "" {
		if len(p.ParentMessageID) > 128 || strings.ContainsAny(p.ParentMessageID, "/\\..") {
			return nil, errors.New("threadmgr: invalid parent_message_id")
		}
	}

	// Space / company membership check: runs BEFORE acquiring the write lock
	// to avoid blocking concurrent Create() calls during I/O.
	if p.SpaceID != "" {
		tm.mu.RLock()
		checker := tm.memberChecker
		gate := tm.companyGate
		specialistCompanyID, isSpecialist := tm.specialistCompanyLocked(p.AgentID)
		tm.mu.RUnlock()

		// Company isolation is the stricter gate. When the space has a
		// company_id, the target must be in CompanyRoster — space roster
		// membership is not enough. Empty company_id is desk-level: keep
		// today's not_in_roster / space-roster behavior.
		companyScoped := false
		if gate != nil {
			companyID, err := gate.SpaceCompanyID(p.SpaceID)
			if err != nil {
				return nil, fmt.Errorf("threadmgr: space company lookup: %w", err)
			}
			if strings.TrimSpace(companyID) != "" {
				companyScoped = true
				var in bool
				if isSpecialist {
					// Ephemeral specialists are never seated in a company
					// roster — AgentInCompany would always reject them.
					// Their company was fixed at spawn time; honor that
					// fixed assignment instead of the roster lookup.
					in = specialistCompanyID == companyID
				} else {
					in, err = gate.AgentInCompany(p.AgentID, companyID)
					if err != nil {
						return nil, fmt.Errorf("threadmgr: company roster: %w", err)
					}
				}
				if !in {
					return nil, &notInCompanyError{Agent: p.AgentID}
				}
			}
		}

		if !companyScoped && isSpecialist {
			// Desk-level space, no company gate: an ephemeral specialist is
			// never a space member, but its spawn was already authorized by
			// the CoS grant. Accept it same as the company-fixed path above.
		} else if !companyScoped && checker != nil {
			members, err := checker.SpaceMembers(p.SpaceID)
			if err != nil {
				return nil, fmt.Errorf("threadmgr: space lookup: %w", err)
			}
			members, err = widenDeskDMMembers(checker, p.SpaceID, members)
			if err != nil {
				return nil, fmt.Errorf("threadmgr: desk floor: %w", err)
			}
			// nil members = space not found → deny-all (safe default).
			allowed := make(map[string]struct{}, len(members))
			for _, m := range members {
				allowed[strings.ToLower(m)] = struct{}{}
			}
			if _, ok := allowed[strings.ToLower(p.AgentID)]; !ok {
				roster := strings.Join(members, ", ")
				if roster == "" {
					roster = "(empty)"
				}
				return nil, fmt.Errorf("DELEGATE_FAIL: %w: agent %q is not a member of this space (roster: %s)",
					ErrAgentNotSpaceMember, p.AgentID, roster)
			}
		}
	}

	now := time.Now()
	t := &Thread{
		ID:              newID(),
		SessionID:       p.SessionID,
		AgentID:         p.AgentID,
		Task:            p.Task,
		Rationale:       p.Rationale,
		ParentMessageID: p.ParentMessageID,
		Status:          StatusQueued,
		DependsOn:       p.DependsOn,
		DependsOnHints:  p.DependsOnHints,
		StartedAt:       now,
		CreatedAt:       now,
		CreatedByUser:   p.CreatedByUser,
		CreatedReason:   p.CreatedReason,
		Specialist:      p.Specialist,
		SpecialistModel: p.SpecialistModel,
		TokenBudget:     p.TokenBudget,
		Timeout:         p.Timeout,
		InputCh:         make(chan string, 1),
	}
	limit := tm.MaxThreadsPerSession
	if limit <= 0 {
		limit = DefaultMaxThreadsPerSession
	}
	tm.mu.Lock()
	if p.SessionID != "" && tm.activeThreadCountLocked(p.SessionID) >= limit {
		tm.mu.Unlock()
		return nil, ErrThreadLimitReached
	}
	// Insert tentatively so DetectCycle can walk the full graph including this thread.
	tm.threads[t.ID] = t
	if len(t.DependsOn) > 0 {
		if tm.detectCycleLocked(t.ID, make(map[string]bool)) {
			delete(tm.threads, t.ID)
			tm.mu.Unlock()
			return nil, fmt.Errorf("%w: thread %q depends on itself transitively", ErrCyclicDependency, t.ID)
		}
	}
	tm.mu.Unlock()
	tm.appendAudit(t.ID, "created", p.CreatedByUser, p.CreatedReason)
	tm.trySnapshot(p.SessionID)

	// Persist synchronously so the thread record is durable before Create() returns.
	// Losing the initial thread record on a crash (async save) is far more damaging
	// than the slight latency cost of a synchronous write here.
	// Graceful degradation: if the save fails, the thread still works in memory.
	tm.mu.RLock()
	store := tm.store
	tm.mu.RUnlock()
	if store != nil {
		tCopy := *t // snapshot; t is already in the map but we don't hold the lock here
		if err := store.SaveThread(context.Background(), &tCopy); err != nil {
			slog.Warn("threadmgr: SaveThread failed on create (thread lives in memory)", "thread_id", tCopy.ID, "err", err)
		}
	}

	return t, nil
}

// Get returns the thread by ID and whether it was found.
func (tm *ThreadManager) Get(id string) (*Thread, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	t, ok := tm.threads[id]
	if !ok {
		return nil, false
	}
	cp := *t
	if t.Summary != nil {
		s := *t.Summary // deep-copy the FinishSummary value
		cp.Summary = &s
	}
	return &cp, true
}

// ListBySession returns all threads for a given session.
func (tm *ThreadManager) ListBySession(sessionID string) []*Thread {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	var result []*Thread
	for _, t := range tm.threads {
		if t.SessionID == sessionID {
			cp := *t // copy the struct
			if t.Summary != nil {
				s := *t.Summary // deep-copy the FinishSummary value, same as Get
				cp.Summary = &s
			}
			result = append(result, &cp)
		}
	}
	// Sort by StartedAt for deterministic ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].StartedAt.Before(result[j].StartedAt)
	})
	return result
}

// Start associates a cancellable context with a thread and marks it as thinking.
// It returns true if the transition from StatusQueued → StatusThinking succeeded,
// or false if the thread was not in StatusQueued (e.g. already started by another
// goroutine). The caller is responsible for launching the actual goroutine only
// when true is returned.
func (tm *ThreadManager) Start(id string, _ context.Context, cancel context.CancelFunc) bool {
	tm.mu.Lock()
	t, ok := tm.threads[id]
	if !ok {
		tm.mu.Unlock()
		return false
	}
	if t.Status != StatusQueued {
		tm.mu.Unlock()
		return false
	}
	t.cancel = cancel
	t.Status = StatusThinking
	tm.mu.Unlock()
	tm.fireStatusChange(id, StatusThinking)
	return true
}

// Cancel cancels a thread's context and marks it as cancelled.
// It is a no-op if the thread has already reached a terminal status
// (StatusDone, StatusError, or StatusCancelled).
// File leases held by the thread are released automatically.
func (tm *ThreadManager) Cancel(id string) {
	tm.mu.Lock()
	t, ok := tm.threads[id]
	var cancel func()
	var fired bool
	var sessionID string
	if ok && t.Status != StatusDone && t.Status != StatusError && t.Status != StatusCancelled {
		t.Status = StatusCancelled
		cancel = t.cancel
		fired = true
		sessionID = t.SessionID
	}
	tm.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if fired {
		tm.appendAudit(id, "cancelled", "", "")
		tm.fireStatusChange(id, StatusCancelled)

		// Persist status change asynchronously.
		tm.mu.RLock()
		store := tm.store
		tm.mu.RUnlock()
		if store != nil {
			threadID := id
			go func() {
				if err := store.UpdateThreadStatus(context.Background(), threadID, string(StatusCancelled)); err != nil {
					slog.Warn("threadmgr: UpdateThreadStatus (cancel) failed", "thread_id", threadID, "err", err)
				}
			}()
		}

		// Notify the caller (e.g. server WS hub) so the client gets a
		// thread_cancelled event without needing to poll.
		tm.onCancelMu.RLock()
		onCancel := tm.onCancel
		tm.onCancelMu.RUnlock()
		if onCancel != nil {
			onCancel(sessionID, id)
		}
	}
	// Release any file leases held by the cancelled thread so other threads
	// can acquire them without waiting for GC.
	if ok {
		tm.ReleaseLeases(id)
	}
}

// Complete marks a thread as done with the given summary and records CompletedAt.
// It is a no-op if the thread has already reached a terminal status
// (StatusDone, StatusError, or StatusCancelled).
// File leases held by the thread are released on successful completion.
func (tm *ThreadManager) Complete(id string, summary FinishSummary) {
	tm.mu.Lock()
	t, ok := tm.threads[id]
	if !ok {
		tm.mu.Unlock()
		return
	}
	// Do not overwrite a terminal status — cancelled threads stay cancelled.
	if t.Status == StatusCancelled || t.Status == StatusError || t.Status == StatusDone {
		tm.mu.Unlock()
		return
	}
	sessionID := t.SessionID
	status := StatusDone
	if summary.Status == "error" {
		status = StatusError
	}
	t.Status = status
	t.CompletedAt = time.Now()
	t.Summary = &summary
	tm.mu.Unlock()

	// Map FinishSummary.Status to audit action.
	auditAction := "completed"
	switch summary.Status {
	case "error":
		auditAction = "error"
	case "completed-with-timeout":
		auditAction = "timeout"
	}
	tm.appendAudit(id, auditAction, "", summary.Summary)
	tm.trySnapshot(sessionID)

	// Persist status change asynchronously.
	tm.mu.RLock()
	store := tm.store
	tm.mu.RUnlock()
	if store != nil {
		// Re-snapshot the thread with summary populated for a full upsert.
		tm.mu.RLock()
		liveCopy := *tm.threads[id] // threads[id] still in map at this point
		if tm.threads[id].Summary != nil {
			s := *tm.threads[id].Summary
			liveCopy.Summary = &s
		}
		tm.mu.RUnlock()
		go func(t Thread) {
			if err := store.SaveThread(context.Background(), &t); err != nil {
				slog.Warn("threadmgr: SaveThread (complete) failed", "thread_id", t.ID, "err", err)
			}
		}(liveCopy)
	}

	tm.fireStatusChange(id, status)
	// Release file leases now that the thread has finished writing.
	tm.ReleaseLeases(id)
}

// AttachVetResult records the productized vet loop's verdict onto a thread
// that has already reached StatusDone, appending a short "Vetted: ..."
// line to the thread's Summary text (the same field the thread panel and
// WaitForThreads results already surface — no new event type needed) and
// setting the structured VetLabel/VetFindings fields.
//
// Idempotent: a no-op (returns false) if the thread is missing, has no
// Summary yet, or already carries a VetLabel — this is the "cap: one vet
// per thread" enforcement point. Safe to call from any goroutine.
func (tm *ThreadManager) AttachVetResult(id, label, findings string) bool {
	tm.mu.Lock()
	t, ok := tm.threads[id]
	if !ok || t.Summary == nil || t.Summary.VetLabel != "" {
		tm.mu.Unlock()
		return false
	}
	t.Summary.VetLabel = label
	t.Summary.VetFindings = findings
	t.Summary.Summary = strings.TrimRight(t.Summary.Summary, "\n") +
		fmt.Sprintf("\n\n---\n**Vetted: %s**", label)
	if findings != "" {
		t.Summary.Summary += "\n" + findings
	}
	liveCopy := *t
	summaryCopy := *t.Summary
	liveCopy.Summary = &summaryCopy
	store := tm.store
	tm.mu.Unlock()

	if store != nil {
		go func(th Thread) {
			if err := store.SaveThread(context.Background(), &th); err != nil {
				slog.Warn("threadmgr: SaveThread (vet) failed", "thread_id", th.ID, "err", err)
			}
		}(liveCopy)
	}
	return true
}

// ResolveDependencies converts DependsOnHints (agent names) to thread IDs by
// looking up the most-recently-created thread per agent in the same session.
// The resolved IDs are appended to DependsOn and hints are cleared.
// Safe to call multiple times — idempotent after hints are consumed.
func (tm *ThreadManager) ResolveDependencies(id string) []string {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	t, ok := tm.threads[id]
	if !ok || len(t.DependsOnHints) == 0 {
		if ok {
			return t.DependsOn
		}
		return nil
	}

	// Build agent-name → most-recent thread-ID index for this session.
	agentToThread := make(map[string]string)
	for _, other := range tm.threads {
		if other.SessionID == t.SessionID && other.ID != t.ID {
			// Later-created threads overwrite earlier ones for the same agent.
			agentToThread[other.AgentID] = other.ID
		}
	}

	// Build a set of already-known dep IDs to prevent duplicates.
	existing := make(map[string]struct{}, len(t.DependsOn))
	for _, dep := range t.DependsOn {
		existing[dep] = struct{}{}
	}

	// Resolve hints to thread IDs, deduplicating against existing deps.
	for _, hint := range t.DependsOnHints {
		if tid, found := agentToThread[hint]; found {
			if _, dup := existing[tid]; !dup {
				t.DependsOn = append(t.DependsOn, tid)
				existing[tid] = struct{}{}
			}
		}
	}
	t.DependsOnHints = nil // consumed
	return t.DependsOn
}

// GetInputCh returns the InputCh channel for the live thread (not a copy).
// Returns nil, false if thread not found.
func (tm *ThreadManager) GetInputCh(id string) (chan string, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	t, ok := tm.threads[id]
	if !ok {
		return nil, false
	}
	return t.InputCh, true
}

// TrySendInput attempts to deliver input to a thread that is waiting for user
// input (StatusBlocked). Returns (sent, found, reason):
//   - sent=true, found=true, reason="" on successful delivery
//   - sent=false, found=false, reason="not_found" if missing or wrong-session
//   - sent=false, found=true, reason="not_waiting" if thread is not blocked
//   - sent=false, found=true, reason="buffer_full" if blocked but input queue is full
func (tm *ThreadManager) TrySendInput(threadID, sessionID, input string) (sent bool, found bool, reason string) {
	tm.mu.RLock()
	t, ok := tm.threads[threadID]
	if !ok || (sessionID != "" && t.SessionID != sessionID) {
		tm.mu.RUnlock()
		return false, false, "not_found"
	}
	if t.Status != StatusBlocked {
		tm.mu.RUnlock()
		return false, true, "not_waiting"
	}
	ch := t.InputCh
	tm.mu.RUnlock()

	// Non-blocking send — InputCh is buffered with capacity 1.
	select {
	case ch <- input:
		// Share operator guidance with sibling delegated workers so concurrent
		// threads can adapt without waiting for final synthesis.
		trimmed := strings.TrimSpace(input)
		if trimmed != "" {
			if len(trimmed) > 200 {
				trimmed = trimmed[:200] + "…"
			}
			tm.PublishSiblingContext(threadID, "user guidance: "+trimmed)
		}
		return true, true, ""
	default:
		return false, true, "buffer_full"
	}
}

// InjectReceipt returns UI-facing delivery metadata for a successful thread
// input injection:
//   - deliveredToAgent: target delegated agent for the injected thread
//   - sharedWithActive: number of sibling active delegates in the same session
//     that can consume the published "user guidance" context update
//
// ok is false when the thread is not found.
func (tm *ThreadManager) InjectReceipt(threadID string) (deliveredToAgent string, sharedWithActive int, ok bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	target, exists := tm.threads[threadID]
	if !exists {
		return "", 0, false
	}
	for id, candidate := range tm.threads {
		if id == threadID || candidate.SessionID != target.SessionID {
			continue
		}
		switch candidate.Status {
		case StatusDone, StatusError, StatusCancelled:
			// terminal; do not count as an active delegate
		default:
			sharedWithActive++
		}
	}
	return target.AgentID, sharedWithActive, true
}

// PublishSessionGuidance broadcasts operator guidance into the session thread bus
// so all active delegated workers can adapt while the lead run is still active.
// Returns the number of active delegated threads in the session at publish time.
func (tm *ThreadManager) PublishSessionGuidance(sessionID, actor, content string) int {
	return tm.PublishSessionGuidanceTarget(sessionID, actor, "", content)
}

// PublishSessionGuidanceTarget broadcasts operator guidance into the session
// thread bus. When targetAgentID is non-empty, only that delegate will consume
// the update from sibling context.
func (tm *ThreadManager) PublishSessionGuidanceTarget(sessionID, actor, targetAgentID, content string) int {
	content = strings.TrimSpace(content)
	if sessionID == "" || content == "" {
		return 0
	}
	targetAgentID = strings.TrimSpace(targetAgentID)
	tm.mu.RLock()
	bus := tm.threadBus
	active := 0
	for _, t := range tm.threads {
		if t.SessionID != sessionID {
			continue
		}
		if targetAgentID != "" && !strings.EqualFold(t.AgentID, targetAgentID) {
			continue
		}
		switch t.Status {
		case StatusDone, StatusError, StatusCancelled:
			// terminal; not active
		default:
			active++
		}
	}
	tm.mu.RUnlock()
	if bus == nil {
		return active
	}
	if strings.TrimSpace(actor) == "" {
		actor = "operator"
	}
	bus.Publish(sessionID, ThreadContextMessage{
		ThreadID:      "session-guidance",
		AgentID:       actor,
		TargetAgentID: targetAgentID,
		Content:       clipResult(content, 240),
	})
	return active
}

// CancelIfOwned cancels a thread only if it belongs to the given session.
// Returns (cancelled, found). found is false if the thread does not exist or
// belongs to a different session.
func (tm *ThreadManager) CancelIfOwned(threadID, sessionID string) (cancelled bool, found bool) {
	tm.mu.RLock()
	t, ok := tm.threads[threadID]
	if !ok || (sessionID != "" && t.SessionID != sessionID) {
		tm.mu.RUnlock()
		return false, false
	}
	tm.mu.RUnlock()

	tm.Cancel(threadID)
	return true, true
}

// CleanupSession cancels all queued or thinking threads for a session and
// removes their records from the manager. This prevents orphaned threads
// from accumulating in memory when a session ends before threads are started.
// Already-terminal threads (done, error, cancelled) are left untouched.
func (tm *ThreadManager) CleanupSession(sessionID string) {
	tm.mu.Lock()
	var cancels []func()
	var orphanIDs []string
	bus := tm.threadBus
	for id, t := range tm.threads {
		if t.SessionID != sessionID {
			continue
		}
		if t.Status == StatusQueued || t.Status == StatusThinking || t.Status == StatusBlocked {
			t.Status = StatusCancelled
			if t.cancel != nil {
				cancels = append(cancels, t.cancel)
			}
			orphanIDs = append(orphanIDs, id)
		}
	}
	// Remove orphan threads to free memory.
	for _, id := range orphanIDs {
		delete(tm.threads, id)
	}
	tm.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
	for _, id := range orphanIDs {
		tm.ReleaseLeases(id)
	}
	if bus != nil {
		bus.ClearSession(sessionID)
	}
}

// RecordActivity updates a thread's heartbeat: LastActivityAt is set to now and,
// when activity is non-empty, CurrentActivity is replaced. Safe to call from the
// thread's own goroutine and from token callbacks. No-op for unknown threads.
func (tm *ThreadManager) RecordActivity(threadID, activity string) {
	tm.mu.Lock()
	if t, ok := tm.threads[threadID]; ok {
		t.LastActivityAt = time.Now()
		if activity != "" {
			t.CurrentActivity = activity
		}
	}
	tm.mu.Unlock()
}

// recordTurn stamps the thread's current loop turn alongside the heartbeat.
func (tm *ThreadManager) recordTurn(threadID string, turn int, activity string) {
	tm.mu.Lock()
	if t, ok := tm.threads[threadID]; ok {
		t.Turn = turn
		t.LastActivityAt = time.Now()
		if activity != "" {
			t.CurrentActivity = activity
		}
	}
	tm.mu.Unlock()
}

// WaitReport is the result of WaitForThreads: which threads reached a terminal
// status before the deadline and which are still running (with live heartbeat
// state captured at return time).
type WaitReport struct {
	Completed []*Thread
	Pending   []*Thread
	TimedOut  bool // true when the deadline expired with threads still pending
}

// WaitForThreads blocks until every thread in threadIDs reaches a terminal
// status (done, cancelled, error), the timeout expires, or ctx is cancelled.
// Threads not found or belonging to a different session are ignored.
// When threadIDs is empty, includes uncollected terminal threads (that finished
// before wait was called) to prevent the race where fast specialists finish before
// wait_for_threads runs. Returns them as Completed immediately.
// It polls thread state — completion latency is bounded by the poll interval.
func (tm *ThreadManager) WaitForThreads(ctx context.Context, sessionID string, threadIDs []string, timeout time.Duration) WaitReport {
	const pollInterval = 500 * time.Millisecond
	deadline := time.Now().Add(timeout)

	// If no threadIDs provided (session-wide wait), include uncollected terminal threads
	// to handle fast specialists that finish before wait was called.
	// Placeholder / invented IDs ("<thread_id>", "thread_id_retrieved…") never
	// match a real thread. Treat that the same as an empty wait so the lead
	// still collects uncollected finished work instead of inventing an answer.
	if len(threadIDs) > 0 {
		real := make([]string, 0, len(threadIDs))
		for _, id := range threadIDs {
			id = strings.TrimSpace(id)
			if id == "" || strings.ContainsAny(id, "<>") || strings.Contains(id, "thread_id") {
				continue
			}
			if _, ok := tm.Get(id); !ok {
				continue
			}
			real = append(real, id)
		}
		threadIDs = real
	}

	var activeToWait []string
	var immediatelyCompleted []*Thread
	if len(threadIDs) == 0 {
		all := tm.ListBySession(sessionID)
		for _, t := range all {
			switch t.Status {
			case StatusDone, StatusCancelled, StatusError:
				if t.CollectedAt.IsZero() && !staleUncollected(t) {
					// Uncollected terminal thread — include in results immediately
					immediatelyCompleted = append(immediatelyCompleted, t)
				}
			default:
				// Active thread — wait for it
				activeToWait = append(activeToWait, t.ID)
			}
		}
	} else {
		activeToWait = threadIDs
	}

	collect := func() (completed, pending []*Thread) {
		for _, id := range activeToWait {
			t, ok := tm.Get(id)
			if !ok || (sessionID != "" && t.SessionID != sessionID) {
				continue
			}
			switch t.Status {
			case StatusDone, StatusCancelled, StatusError:
				completed = append(completed, t)
			default:
				pending = append(pending, t)
			}
		}
		return completed, pending
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		completed, pending := collect()
		// Add any immediately-completed threads (uncollected terminal threads from session-wide wait)
		completed = append(immediatelyCompleted, completed...)
		if len(pending) == 0 {
			tm.markCollected(completed)
			return WaitReport{Completed: completed}
		}
		if time.Now().After(deadline) {
			tm.markCollected(completed)
			return WaitReport{Completed: completed, Pending: pending, TimedOut: true}
		}
		select {
		case <-ctx.Done():
			tm.markCollected(completed)
			return WaitReport{Completed: completed, Pending: pending, TimedOut: true}
		case <-ticker.C:
		}
	}
}

// staleUncollectedWindow is how long a finished thread may still be
// collected by a session-wide wait. After a bounce CollectedAt is empty,
// so without this a hallway wait re-dumps hours of old Winston clocks.
const staleUncollectedWindow = 10 * time.Minute

func staleUncollected(t *Thread) bool {
	if t == nil {
		return true
	}
	if !t.CompletedAt.IsZero() {
		return time.Since(t.CompletedAt) > staleUncollectedWindow
	}
	if !t.StartedAt.IsZero() {
		return time.Since(t.StartedAt) > staleUncollectedWindow
	}
	return false
}

// markCollected stamps CollectedAt on the live thread records whose results
// are being returned to a waiting caller, so the completion notifier knows
// the lead agent already has them and skips its automatic follow-up.
func (tm *ThreadManager) markCollected(threads []*Thread) {
	if len(threads) == 0 {
		return
	}
	now := time.Now()
	tm.mu.Lock()
	for _, t := range threads {
		if live, ok := tm.threads[t.ID]; ok && live.CollectedAt.IsZero() {
			live.CollectedAt = now
		}
	}
	tm.mu.Unlock()
}

// WasCollected reports whether the thread's result has been delivered to a
// waiting lead agent via WaitForThreads.
func (tm *ThreadManager) WasCollected(threadID string) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	t, ok := tm.threads[threadID]
	return ok && !t.CollectedAt.IsZero()
}

// ActiveThreadIDs returns the IDs of all non-terminal threads in the session,
// ordered by creation time. Used by wait_for_threads when no IDs are given.
func (tm *ThreadManager) ActiveThreadIDs(sessionID string) []string {
	threads := tm.ListBySession(sessionID)
	var ids []string
	for _, t := range threads {
		switch t.Status {
		case StatusDone, StatusCancelled, StatusError:
		default:
			ids = append(ids, t.ID)
		}
	}
	return ids
}

// PublishSiblingContext writes a short context update for a live thread. The
// update becomes visible to sibling threads in the same session.
func (tm *ThreadManager) PublishSiblingContext(threadID, content string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	tm.mu.RLock()
	t, ok := tm.threads[threadID]
	bus := tm.threadBus
	tm.mu.RUnlock()
	if !ok || bus == nil {
		return
	}
	bus.Publish(t.SessionID, ThreadContextMessage{
		ThreadID: threadID,
		AgentID:  t.AgentID,
		Content:  clipResult(content, 240),
	})
}

// SiblingContext returns recent context updates from sibling threads in the
// same session as threadID. Updates from threadID itself are excluded.
func (tm *ThreadManager) SiblingContext(threadID string, limit int) []ThreadContextMessage {
	tm.mu.RLock()
	t, ok := tm.threads[threadID]
	bus := tm.threadBus
	tm.mu.RUnlock()
	if !ok || bus == nil {
		return nil
	}
	return bus.SiblingContext(t.SessionID, threadID, t.AgentID, limit)
}

// RequireApprovalToken validates a lead-issued token for a high-risk delegated
// action within the scope of the thread's session/thread/provider/action.
func (tm *ThreadManager) RequireApprovalToken(threadID, token, provider, action string) error {
	tm.mu.RLock()
	t, ok := tm.threads[threadID]
	reg := tm.proposalRegistry
	tm.mu.RUnlock()
	if !ok {
		return ErrApprovalTokenScopeMismatch
	}
	if reg == nil {
		return ErrApprovalTokenRequired
	}
	return reg.RequireToken(token, TokenRequirement{
		HighRisk:  true,
		SessionID: t.SessionID,
		ThreadID:  threadID,
		Provider:  provider,
		Action:    action,
	})
}

// ErrThreadNotFound is returned by ArchiveThread when the thread ID does not exist.
var ErrThreadNotFound = fmt.Errorf("thread not found")

// ErrThreadActive is returned by ArchiveThread when the thread is in an active
// state (thinking or tooling) and cannot be safely archived.
var ErrThreadActive = fmt.Errorf("cannot archive an active thread")

// ArchiveThread marks a thread as archived. Archived threads are hidden from
// the default list view but their messages are preserved. Returns ErrThreadNotFound
// if the thread does not exist, or ErrThreadActive if the thread is currently
// running (StatusThinking or StatusTooling).
func (tm *ThreadManager) ArchiveThread(id string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	t, ok := tm.threads[id]
	if !ok {
		return ErrThreadNotFound
	}
	if t.Status == StatusThinking || t.Status == StatusTooling {
		return ErrThreadActive
	}
	now := time.Now()
	t.ArchivedAt = &now
	return nil
}

// IsReady returns true if all upstream dependencies have StatusDone.
// A thread with no dependencies is immediately ready.
func (tm *ThreadManager) IsReady(id string) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	t, ok := tm.threads[id]
	if !ok {
		return false
	}
	for _, depID := range t.DependsOn {
		dep, ok := tm.threads[depID]
		if !ok || dep.Status != StatusDone {
			return false
		}
	}
	return true
}

// IsBlockedByFailure returns true if any upstream dependency has reached a
// terminal failure state (StatusCancelled or StatusError), making it
// impossible for this thread to ever become ready. Callers should cancel
// or error the thread immediately rather than waiting for the watchdog.
func (tm *ThreadManager) IsBlockedByFailure(id string) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	t, ok := tm.threads[id]
	if !ok {
		return false
	}
	for _, depID := range t.DependsOn {
		dep, ok := tm.threads[depID]
		if !ok {
			continue // unknown dep treated conservatively
		}
		if dep.Status == StatusCancelled || dep.Status == StatusError {
			return true
		}
	}
	return false
}

// DetectCycle returns true if there is a cycle in the dependency graph reachable
// from the given thread ID. It performs a depth-first search through DependsOn edges.
// This is the public variant; it acquires a read lock.
func (tm *ThreadManager) DetectCycle(id string) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.detectCycleLocked(id, make(map[string]bool))
}

// detectCycleLocked is the internal DFS cycle detector. Caller must hold at
// least a read lock on tm.mu. visited tracks nodes on the current DFS path.
func (tm *ThreadManager) detectCycleLocked(id string, visited map[string]bool) bool {
	if visited[id] {
		return true // back-edge → cycle
	}
	t, ok := tm.threads[id]
	if !ok {
		return false
	}
	visited[id] = true
	for _, depID := range t.DependsOn {
		if tm.detectCycleLocked(depID, visited) {
			return true
		}
	}
	visited[id] = false // pop from path (allow revisiting via different routes)
	return false
}

// Prune removes threads that are in a terminal state (StatusDone, StatusCancelled,
// StatusError) and whose CompletedAt timestamp is older than maxAge. It returns the
// number of threads removed. Safe to call from multiple goroutines.
func (tm *ThreadManager) Prune(maxAge time.Duration) int {
	cutoff := time.Now().Add(-maxAge)
	tm.mu.Lock()
	defer tm.mu.Unlock()
	pruned := 0
	for id, t := range tm.threads {
		switch t.Status {
		case StatusDone, StatusCancelled, StatusError:
			if !t.CompletedAt.IsZero() && t.CompletedAt.Before(cutoff) {
				delete(tm.threads, id)
				pruned++
			}
		}
	}
	return pruned
}

// StartPruner launches a background goroutine that calls Prune(maxAge) every
// interval until ctx is cancelled. This prevents completed/cancelled/error
// threads from accumulating in memory indefinitely. The goroutine exits cleanly
// when ctx is done.
func (tm *ThreadManager) StartPruner(ctx context.Context, interval, maxAge time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				tm.Prune(maxAge)
			}
		}
	}()
}

// StartWatchdog launches a background goroutine that scans all threads every
// 60 seconds and transitions stale non-terminal threads to StatusError,
// broadcasting a thread_done event so the frontend updates immediately rather
// than leaving the user waiting forever. Two thresholds apply:
//
//   - StatusQueued threads older than 10 minutes: never started — likely a bug
//     in the delegation path or a transient scheduler overload.
//   - StatusThinking / StatusTooling threads older than 30 minutes: sub-agent
//     did not complete within a reasonable wall-clock bound.
//
// The goroutine exits cleanly when ctx is cancelled.
// broadcast may be nil; when nil, no WS event is emitted but the thread is
// still transitioned to StatusError so it does not block DAG evaluation.
func (tm *ThreadManager) StartWatchdog(ctx context.Context, broadcast BroadcastFn) {
	const (
		scanInterval   = 60 * time.Second
		queuedTimeout  = 10 * time.Minute
		runningTimeout = 30 * time.Minute
	)
	go func() {
		ticker := time.NewTicker(scanInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				tm.watchdogScan(time.Now(), broadcast, queuedTimeout, runningTimeout)
			}
		}
	}()
}

// watchdogScan performs one watchdog pass: it finds threads that have been
// StatusQueued longer than queuedTimeout or StatusThinking/StatusTooling
// longer than runningTimeout, transitions them to StatusError, and
// broadcasts a thread_done event for each. Extracted from StartWatchdog so
// it can be driven directly (and deterministically) by tests.
//
// The StatusError transition is written through to the durable ThreadStore
// (when one is configured) before returning, exactly like Finalize/Cancel.
// Without this, a process restart reloads the thread via LoadFromStore still
// in its last-persisted (non-terminal) status, and the next watchdog scan
// times it out — and re-broadcasts — all over again.
func (tm *ThreadManager) watchdogScan(now time.Time, broadcast BroadcastFn, queuedTimeout, runningTimeout time.Duration) {
	tm.mu.Lock()
	type timedOut struct {
		id        string
		sessionID string
		agentID   string
		summary   string
	}
	var victims []timedOut
	for id, t := range tm.threads {
		switch t.Status {
		case StatusQueued:
			if now.Sub(t.CreatedAt) > queuedTimeout {
				victims = append(victims, timedOut{
					id:        id,
					sessionID: t.SessionID,
					agentID:   t.AgentID,
					summary:   "delegation timed out — thread never started",
				})
			}
		case StatusThinking, StatusTooling:
			if now.Sub(t.CreatedAt) > runningTimeout {
				victims = append(victims, timedOut{
					id:        id,
					sessionID: t.SessionID,
					agentID:   t.AgentID,
					summary:   "delegation timed out — sub-agent did not complete",
				})
			}
		}
	}
	// Transition victims to StatusError while still holding the lock.
	for _, v := range victims {
		t, ok := tm.threads[v.id]
		if !ok {
			continue
		}
		// Skip threads that transitioned since we built the list.
		switch t.Status {
		case StatusDone, StatusCancelled, StatusError:
			continue
		}
		t.Status = StatusError
		t.CompletedAt = now
	}
	store := tm.store
	tm.mu.Unlock()

	// Fire status-change hooks, broadcast events, and persist the terminal
	// status to the durable store (when configured) outside the lock.
	for _, v := range victims {
		tm.appendAudit(v.id, "error", "", v.summary)
		tm.fireStatusChange(v.id, StatusError)
		if store != nil {
			if err := store.UpdateThreadStatus(context.Background(), v.id, string(StatusError)); err != nil {
				slog.Warn("threadmgr: UpdateThreadStatus (watchdog) failed", "thread_id", v.id, "err", err)
			}
		}
		if broadcast != nil {
			broadcast(v.sessionID, "thread_done", map[string]any{
				"thread_id": v.id,
				"agent_id":  v.agentID,
				"status":    "error",
				"summary":   v.summary,
			})
		}
		slog.Warn("threadmgr: watchdog timed out thread",
			"thread_id", v.id, "session_id", v.sessionID, "reason", v.summary)
	}
}

// trySnapshot serialises the current thread dependency graph for sessionID to
// graphDir if one is configured. It is a no-op when graphDir is empty.
// Errors are logged but not returned — snapshot failures are non-fatal.
func (tm *ThreadManager) trySnapshot(sessionID string) {
	if sessionID == "" {
		return
	}
	tm.mu.RLock()
	dir := tm.graphDir
	tm.mu.RUnlock()
	if dir == "" {
		return
	}
	if err := tm.snapshotGraph(sessionID, dir); err != nil {
		slog.Warn("threadmgr: failed to snapshot dependency graph",
			"session", sessionID, "err", err)
	}
}

// SetGraphDir sets the directory where session dependency graphs are persisted
// for crash recovery. Pass "" to disable persistence (default). Thread-safe.
func (tm *ThreadManager) SetGraphDir(dir string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.graphDir = dir
}

// SetStore wires the durable ThreadStore used to persist thread state across
// server restarts. Pass nil to disable persistence (in-memory-only mode).
// Thread-safe.
func (tm *ThreadManager) SetStore(store ThreadStore) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.store = store
}

// LoadFromStore populates the in-memory thread map from the durable store for
// the given sessionID. Should be called once when a session is opened so that
// threads survive server restarts. Only threads not already present in memory
// are inserted; existing live threads are not overwritten.
// Returns nil when no store is configured.
func (tm *ThreadManager) LoadFromStore(ctx context.Context, sessionID string) error {
	tm.mu.RLock()
	store := tm.store
	tm.mu.RUnlock()
	if store == nil {
		return nil
	}

	threads, err := store.LoadThreads(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("threadmgr: LoadFromStore session %s: %w", sessionID, err)
	}

	tm.mu.Lock()
	for _, t := range threads {
		if _, exists := tm.threads[t.ID]; !exists {
			tm.threads[t.ID] = t
		}
	}
	tm.mu.Unlock()
	return nil
}

// appendAudit appends an AuditEntry to the ring-buffer audit log.
// When the log reaches maxAuditEntries the oldest entry is dropped.
func (tm *ThreadManager) appendAudit(threadID, action, actor, reason string) {
	e := AuditEntry{
		At:       time.Now(),
		ThreadID: threadID,
		Action:   action,
		Actor:    actor,
		Reason:   reason,
	}
	tm.auditMu.Lock()
	if len(tm.auditLog) >= maxAuditEntries {
		// Ring-buffer: drop the oldest entry by shifting the slice.
		copy(tm.auditLog, tm.auditLog[1:])
		tm.auditLog[len(tm.auditLog)-1] = e
	} else {
		tm.auditLog = append(tm.auditLog, e)
	}
	tm.auditMu.Unlock()
}

// AuditLog returns a snapshot copy of the current audit log entries.
// The returned slice is safe to read without holding any lock.
func (tm *ThreadManager) AuditLog() []AuditEntry {
	tm.auditMu.Lock()
	defer tm.auditMu.Unlock()
	cp := make([]AuditEntry, len(tm.auditLog))
	copy(cp, tm.auditLog)
	return cp
}

// FinalizeThread transitions a thread to a terminal state identified by statusStr.
// Valid statusStr values: "done", "cancelled", "error". Any other value maps to
// StatusError. If the thread is already in a terminal state this is a no-op
// (idempotent). An audit entry is appended on each successful transition.
func (tm *ThreadManager) FinalizeThread(id, statusStr, reason string) {
	var newStatus ThreadStatus
	var auditAction string
	switch statusStr {
	case "done":
		newStatus = StatusDone
		auditAction = "completed"
	case "cancelled":
		newStatus = StatusCancelled
		auditAction = "cancelled"
	default:
		newStatus = StatusError
		auditAction = "error"
	}

	tm.mu.Lock()
	t, ok := tm.threads[id]
	if !ok {
		tm.mu.Unlock()
		return
	}
	// Idempotent: do not overwrite a terminal status.
	switch t.Status {
	case StatusDone, StatusCancelled, StatusError:
		tm.mu.Unlock()
		return
	}
	t.Status = newStatus
	if t.CompletedAt.IsZero() {
		t.CompletedAt = time.Now()
	}
	tm.mu.Unlock()

	tm.appendAudit(id, auditAction, "", reason)

	// Persist status change asynchronously.
	tm.mu.RLock()
	store := tm.store
	tm.mu.RUnlock()
	if store != nil {
		threadID := id
		statusStr := string(newStatus)
		go func() {
			if err := store.UpdateThreadStatus(context.Background(), threadID, statusStr); err != nil {
				slog.Warn("threadmgr: UpdateThreadStatus (finalize) failed", "thread_id", threadID, "err", err)
			}
		}()
	}

	tm.fireStatusChange(id, newStatus)
}

// Archive marks a thread as archived and appends an audit entry with the given
// reason. Unlike ArchiveThread, this method does not return an error — it is a
// best-effort operation suitable for deferred cleanup paths.
func (tm *ThreadManager) Archive(id, reason string) {
	now := time.Now()
	tm.mu.Lock()
	t, ok := tm.threads[id]
	if ok && t.ArchivedAt == nil {
		t.ArchivedAt = &now
	}
	tm.mu.Unlock()
	if ok {
		tm.appendAudit(id, "archived", "", reason)
	}
}
