package agent

import (
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/notepad"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/relay"
	"github.com/scrypster/huginn/internal/search"
	huginsession "github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/skills"
	"github.com/scrypster/huginn/internal/tools"
)

// recordLLMLatency records a latency sample to the stats collector.
// startNs must be the value of time.Now().UnixNano() captured before the LLM call.
// slot identifies the call site (e.g. "planner", "coder", "agent-chat").
func (o *Orchestrator) recordLLMLatency(startNs int64, slot string) {
	elapsed := float64(time.Now().UnixNano()-startNs) / 1e6
	o.sc.Histogram("agent.llm_latency_ms", elapsed, "slot:"+slot)
}

// MachineID returns the machine identifier (sourced from config; may be empty
// if the orchestrator was created without a config).
func (o *Orchestrator) MachineID() string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.machineID
}

// LastUsage returns prompt and completion token counts from the most recent call.
func (o *Orchestrator) LastUsage() (promptTokens, completionTokens int) {
	return int(o.lastUsagePrompt.Load()), int(o.lastUsageCompletion.Load())
}

// WithMachineID sets the machine ID on the orchestrator, typically called from
// main.go after loading config.
func (o *Orchestrator) WithMachineID(id string) *Orchestrator {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.machineID = id
	return o
}

// SetSessionStore wires the persistent session store so HydrateSession can
// re-load conversation history after a restart or session resume.
func (o *Orchestrator) SetSessionStore(store huginsession.StoreInterface) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sessionStore = store
}

// SetMuninnConfigPath sets the path to the global MuninnDB config file.
// Used by per-session MCP connection during AgentChat.
func (o *Orchestrator) SetMuninnConfigPath(path string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.muninnCfgPath = path
}

// SetTools configures the tool registry and permission gate for agentic mode.
func (o *Orchestrator) SetTools(reg *tools.Registry, gate *permissions.Gate) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.toolRegistry = reg
	o.permGate = gate
}

// SetAgentRegistry injects the named agent registry.
func (o *Orchestrator) SetAgentRegistry(reg *agents.AgentRegistry) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.agentReg = reg
}

// SetBackendCache injects the per-agent backend cache.
func (o *Orchestrator) SetBackendCache(bc *backend.BackendCache) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.backendCache = bc
}

// UpdateFallbackAPIKey updates the raw API key reference used when agents
// specify a provider but no per-agent key. Also invalidates all cached
// backend instances so stale backends built with the old (possibly empty)
// key are evicted immediately. Safe to call while the server is running —
// e.g., when the user sets a new API key via the web UI.
func (o *Orchestrator) UpdateFallbackAPIKey(rawRef string) {
	o.mu.RLock()
	bc := o.backendCache
	o.mu.RUnlock()
	if bc != nil {
		bc.WithFallbackAPIKey(rawRef)
		bc.InvalidateCache()
	}
}

// UpdateModels hot-reloads the reasoner model name so that
// new agent sessions pick up the change immediately without a restart.
// An empty string preserves the current value.
func (o *Orchestrator) UpdateModels(_, _, reasoner string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.models == nil {
		return
	}
	if reasoner != "" {
		o.models.Reasoner = reasoner
	}
}

// backendFor resolves the backend for the given agent.
// Falls back to o.backend when no BackendCache is configured or agent is nil.
// Thread-safe: snapshots o.backendCache and o.backend under o.mu.
func (o *Orchestrator) backendFor(ag *agents.Agent) (backend.Backend, error) {
	o.mu.RLock()
	bc := o.backendCache
	fb := o.backend
	o.mu.RUnlock()
	if bc == nil {
		return fb, nil
	}
	if ag == nil {
		return bc.For("", "", "", "")
	}
	return bc.For(ag.Provider, ag.Endpoint, ag.APIKey, ag.GetModelID())
}

// resolveDefaultAgent returns the default agent, its model name, and the resolved backend.
// Thread-safe: snapshots o.agentReg under o.mu.
func (o *Orchestrator) resolveDefaultAgent() (*agents.Agent, string, backend.Backend, error) {
	o.mu.RLock()
	reg := o.agentReg
	o.mu.RUnlock()
	var ag *agents.Agent
	if reg != nil {
		ag = reg.DefaultAgent()
	}
	modelName := o.defaultModelName()
	b, err := o.backendFor(ag)
	return ag, modelName, b, err
}

// defaultModelName returns the model ID from the default agent, or the configured default.
// Thread-safe: snapshots o.agentReg and o.defaultModel under o.mu.
func (o *Orchestrator) defaultModelName() string {
	o.mu.RLock()
	reg := o.agentReg
	dm := o.defaultModel
	o.mu.RUnlock()
	if reg != nil {
		if ag := reg.DefaultAgent(); ag != nil {
			if id := ag.GetModelID(); id != "" {
				return id
			}
		}
	}
	return dm
}

// GetAgentRegistry returns the current agent registry. Returns nil if none has
// been injected yet. Thread-safe.
func (o *Orchestrator) GetAgentRegistry() *agents.AgentRegistry {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.agentReg
}

// SetMemoryStore injects the MemoryStore for agent cross-session persistence.
func (o *Orchestrator) SetMemoryStore(ms agents.MemoryStoreIface) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.memoryStore = ms
}

// SetMemoryReplicator injects the MemoryReplicator for channel vault replication.
// Pass nil to disable memory replication (default).
func (o *Orchestrator) SetMemoryReplicator(mr *MemoryReplicator) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.memoryReplicator = mr
}

// SetRelayHub sets the relay hub for remote agent routing.
// Pass nil to use InProcessHub (default, current behavior).
func (o *Orchestrator) SetRelayHub(h relay.Hub) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.relayHub = h
}

// SetWSBroadcast wires a function that emits typed WS events to browser clients
// subscribed to a given session. The signature matches threadmgr.BroadcastFn
// to allow wiring from the server package without a circular import.
// Pass nil to disable WS briefing events (default).
func (o *Orchestrator) SetWSBroadcast(fn func(sessionID, msgType string, payload map[string]any)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.wsBroadcast = fn
}

// ModelNames returns the deduplicated list of configured model IDs across all agents.
// Falls back to the configured default model when no agent registry is set.
func (o *Orchestrator) ModelNames() []string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	if o.agentReg != nil {
		for _, ag := range o.agentReg.All() {
			add(ag.GetModelID())
		}
		return names
	}
	// Fall back to the Models config struct if available.
	if o.models != nil {
		add(o.models.Reasoner)
		return names
	}
	add(o.defaultModel)
	return names
}

// SetSkillsFragment injects workspace rule content into the ContextBuilder.
// Called after any skill mutation (e.g., install, delete, enable/disable).
func (o *Orchestrator) SetSkillsFragment(fragment string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.contextBuilder.SetSkillsFragment(fragment)
}

// SkillsFragment returns the current skills fragment string injected into context.
func (o *Orchestrator) SkillsFragment() string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.contextBuilder.SkillsFragment()
}

// SetSkillsRegistry stores the full skills registry for per-agent skill resolution.
// Called by the server after any skill mutation.
func (o *Orchestrator) SetSkillsRegistry(reg *skills.SkillRegistry) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.skillsReg = reg
}

// SkillsRegistry returns the current skills registry. Primarily used in tests.
func (o *Orchestrator) SkillsRegistry() *skills.SkillRegistry {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.skillsReg
}

// skillsFragmentFor resolves the skills prompt fragment for the default agent
// in the given registry. Delegates nil/empty/named-list semantics entirely to
// FilteredSkillsFragment — see SkillRegistry.FilteredSkillsFragment for details.
// Returns "" when no skills registry is set.
func (o *Orchestrator) skillsFragmentFor(agReg *agents.AgentRegistry) string {
	o.mu.RLock()
	skillsReg := o.skillsReg
	o.mu.RUnlock()

	if skillsReg == nil {
		return ""
	}

	var agentSkills []string // nil = not set → global fallback
	if agReg != nil {
		if da := agReg.DefaultAgent(); da != nil {
			agentSkills = da.Skills
		}
	}

	return skillsReg.FilteredSkillsFragment(agentSkills)
}

// SkillsFragmentForAgent resolves the per-agent skills prompt fragment for a
// specific agent (rather than the registry's default agent). Used by the
// non-default agent chat path (ChatWithAgent) and by scheduled workflow steps
// so each agent gets its own assigned skills, not the orchestrator's defaults.
//
// Semantics match FilteredSkillsFragment: nil Skills field → global fallback;
// empty/non-nil → no skills fragment; named list → just those skills.
// Returns "" when no skills registry is configured.
func (o *Orchestrator) SkillsFragmentForAgent(ag *agents.Agent) string {
	o.mu.RLock()
	skillsReg := o.skillsReg
	o.mu.RUnlock()

	if skillsReg == nil {
		return ""
	}
	var agentSkills []string
	if ag != nil {
		agentSkills = ag.Skills
	}
	return skillsReg.FilteredSkillsFragment(agentSkills)
}

// InvalidateSkillsCache marks the orchestrator's per-agent skills fragment as
// stale so the next AgentChat call will recompute it from the registry.
// Intended to be passed as the reload callback to SkillRegistry.SetReloadCallback.
func (o *Orchestrator) InvalidateSkillsCache() {
	o.mu.Lock()
	defer o.mu.Unlock()
	// Clearing the context-builder's fragment is the simplest safe-invalidation:
	// the next Build() call will regenerate it via skillsFragmentFor.
	o.contextBuilder.SetSkillsFragment("")
}

// SetNotepads injects persistent notepads into the context builder.
func (o *Orchestrator) SetNotepads(notepads []*notepad.Notepad) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.contextBuilder.SetNotepads(notepads)
}

// SetGitRoot sets the git repository root for injecting git context into every Build().
// It also caches the root for loading .huginn.md project instructions in AgentChat.
func (o *Orchestrator) SetGitRoot(root string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.workspaceRoot = root
	o.contextBuilder.SetGitRoot(root)
}

// WorkspaceRoot returns the git repository root set by SetGitRoot.
func (o *Orchestrator) WorkspaceRoot() string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.workspaceRoot
}

// SetHuginnHome sets the ~/.huginn directory path for agent memory file resolution.
func (o *Orchestrator) SetHuginnHome(home string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.huginnHome = home
}

// SetSearcher sets the semantic searcher for context retrieval.
func (o *Orchestrator) SetSearcher(s search.Searcher) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.contextBuilder.SetSearcher(s)
}

// Backend returns the backend instance. Used for sub-tool wiring.
func (o *Orchestrator) Backend() backend.Backend {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.backend
}

// ToolRegistry returns the tool registry. Used for post-setup tool registration (e.g. consult_agent).
func (o *Orchestrator) ToolRegistry() *tools.Registry {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.toolRegistry
}

// SetMaxDelegationDepth sets the cap on agent-to-agent delegation chains.
// A value ≤ 0 keeps the default (5). Wire from config at startup.
func (o *Orchestrator) SetMaxDelegationDepth(depth int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.maxDelegationDepthCfg = depth
}

// SetSpaceContext records the channel/space this orchestrator is serving.
// Must be called before the first session starts so SessionClose can tag summaries.
func (o *Orchestrator) SetSpaceContext(spaceID, spaceName string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.spaceID = spaceID
	o.spaceName = spaceName
}

// maxDelegationDepth returns the effective max delegation depth (≥ 1).
func (o *Orchestrator) maxDelegationDepth() int {
	o.mu.RLock()
	defer o.mu.RUnlock()
	if o.maxDelegationDepthCfg > 0 {
		return o.maxDelegationDepthCfg
	}
	return 0 // workforce.NewDelegationContext defaults to 5 when ≤ 0
}
