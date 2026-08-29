package agents

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/models"
)

// ErrDuplicateAgentName is returned by TryRegister when an agent with the same
// name (case-insensitive) already exists in the registry.
var ErrDuplicateAgentName = errors.New("agent name already registered")

// vaultCollisionCount tracks vault name collisions observed since startup.
// Accessible via VaultCollisionCount() for tests and observability.
var vaultCollisionCount atomic.Int64

// VaultCollisionCount returns the cumulative count of vault name collisions.
func VaultCollisionCount() int64 { return vaultCollisionCount.Load() }

// resetVaultCollisionCount resets the counter to zero. Only for use in tests.
func resetVaultCollisionCount() { vaultCollisionCount.Store(0) }

const (
	MaxDelegationHistory = 6
	MaxAgentHistory      = 20
)

// Agent is a named, persona-bearing model with its own identity and local history.
type Agent struct {
	mu sync.Mutex

	Name                string
	SystemPrompt        string
	Color               string // lipgloss hex, e.g. "#58A6FF"
	Icon                string // single char, e.g. "C"
	IsDefault           bool
	ModelID             string
	Provider            string
	Endpoint            string
	APIKey              string
	History             []backend.Message
	VaultName           string
	Plasticity          string
	MemoryEnabled       bool
	ContextNotesEnabled bool
	MemoryMode          string
	VaultDescription    string
	Toolbelt            []ToolbeltEntry
	Skills              []string
	LocalTools          []string // tool names granted to this agent; ["*"] = all builtins
	ApprovedTools       []string // tool names pre-approved to skip permission prompts

	// Personality selects a behavioral preset (see personality.go). "" behaves
	// like "default" (no persona addendum, no harness bindings).
	Personality string

	// VetWork is the RESOLVED effective setting (personality default merged
	// with any explicit user override — see ResolveVetWork), not the raw
	// tri-state persisted on AgentDef. When true, a completed delegated
	// coding thread for this agent gets a one-shot adversarial reviewer pass
	// before its result is presented as final (see internal/agent's vet
	// wiring).
	VetWork bool
}

// Rename updates the agent's Name and re-indexes it in the registry under the
// new lowercase key. The old key is removed. reg must be the registry this
// agent was registered with; pass nil to update Name only (not recommended).
func (a *Agent) Rename(reg *AgentRegistry, newName string) {
	if reg != nil {
		reg.mu.Lock()
		oldKey := strings.ToLower(a.Name)
		a.Name = newName
		newKey := strings.ToLower(newName)
		delete(reg.agents, oldKey)
		reg.agents[newKey] = a
		reg.mu.Unlock()
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Name = newName
}

// WithExtraSystem returns a shallow copy of this agent with extraSystem appended
// to its SystemPrompt. The copy has a fresh zero-value mutex so it is safe to use
// as a single-request context without affecting the shared agent instance.
//
// Returns the receiver unchanged if extraSystem is empty.
func (a *Agent) WithExtraSystem(extraSystem string) *Agent {
	if extraSystem == "" {
		return a
	}
	a.mu.Lock()
	cp := a.cloneUnlocked()
	cp.SystemPrompt = cp.SystemPrompt + extraSystem
	a.mu.Unlock()
	return &cp
}

// SwapModel hot-swaps the model behind this agent. Thread-safe.
func (a *Agent) SwapModel(modelID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ModelID = modelID
}

// WithModelOverride returns a shallow copy of this agent whose ModelID has
// been replaced. The copy is request-scoped — the shared registry instance
// is never mutated, so two concurrent workflow steps can run the same agent
// against different models safely.
//
// Returns the receiver unchanged if modelID is empty (no override). Provider
// and Endpoint are preserved so backend resolution still works.
func (a *Agent) WithModelOverride(modelID string) *Agent {
	if strings.TrimSpace(modelID) == "" {
		return a
	}
	a.mu.Lock()
	cp := a.cloneUnlocked()
	cp.ModelID = modelID
	a.mu.Unlock()
	return &cp
}

// cloneUnlocked returns a request-scoped copy of the agent.
// Caller must hold a.mu while reading source fields.
func (a *Agent) cloneUnlocked() Agent {
	return Agent{
		// mu is intentionally zero-valued in the copy.
		Name:                a.Name,
		SystemPrompt:        a.SystemPrompt,
		Color:               a.Color,
		Icon:                a.Icon,
		IsDefault:           a.IsDefault,
		ModelID:             a.ModelID,
		Provider:            a.Provider,
		Endpoint:            a.Endpoint,
		APIKey:              a.APIKey,
		VaultName:           a.VaultName,
		Plasticity:          a.Plasticity,
		MemoryEnabled:       a.MemoryEnabled,
		ContextNotesEnabled: a.ContextNotesEnabled,
		MemoryMode:          a.MemoryMode,
		VaultDescription:    a.VaultDescription,
		Personality:         a.Personality,
		VetWork:             a.VetWork,
		// Clone slice-backed fields so request-scoped copies never alias shared
		// registry state under concurrent workflow execution.
		Toolbelt:      append([]ToolbeltEntry(nil), a.Toolbelt...),
		Skills:        append([]string(nil), a.Skills...),
		LocalTools:    append([]string(nil), a.LocalTools...),
		ApprovedTools: append([]string(nil), a.ApprovedTools...),
		// History is intentionally not copied — the copy is request-scoped.
	}
}

// DelegationContext returns the last MaxDelegationHistory messages for use as
// context when this agent is consulted by another agent.
func (a *Agent) DelegationContext() []backend.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.History) <= MaxDelegationHistory {
		cp := make([]backend.Message, len(a.History))
		copy(cp, a.History)
		return cp
	}
	src := a.History[len(a.History)-MaxDelegationHistory:]
	cp := make([]backend.Message, len(src))
	copy(cp, src)
	return cp
}

// AppendHistory adds messages to this agent's local history, trimming to MaxAgentHistory.
func (a *Agent) AppendHistory(msgs ...backend.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.History = append(a.History, msgs...)
	if len(a.History) > MaxAgentHistory {
		a.History = a.History[len(a.History)-MaxAgentHistory:]
	}
}

// GetModelID returns the canonical API model ID for this agent. Thread-safe.
// Friendly aliases (e.g. "haiku", "sonnet") are resolved to their real model IDs
// via the global ProviderCatalog so agents stay functional across Anthropic renames.
func (a *Agent) GetModelID() string {
	a.mu.Lock()
	id := a.ModelID
	provider := a.Provider
	a.mu.Unlock()
	return models.GlobalProviderCatalog().Resolve(provider, id)
}

// HistoryLen returns the current history length. Thread-safe.
func (a *Agent) HistoryLen() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.History)
}

// SnapshotHistory returns the last n messages from history as a copy.
// If n <= 0 or n >= len(history), the full history is returned.
func (a *Agent) SnapshotHistory(n int) []backend.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	h := a.History
	if n > 0 && len(h) > n {
		h = h[len(h)-n:]
	}
	cp := make([]backend.Message, len(h))
	copy(cp, h)
	return cp
}

// BuildPersonaPrompt constructs the system prompt for an agent call,
// prepending the agent's persona before the codebase context block.
// An addendum lists this agent's local tools, image-generation grant,
// and whether it can delegate — so the model can say so like a teammate
// instead of crashing on a missing capability.
func BuildPersonaPrompt(ag *Agent, ctxText string) string {
	var body string
	if ag.SystemPrompt != "" {
		prefix := ""
		if ag.Name != "" {
			prefix = "Your name is " + ag.Name + ".\n\n"
		}
		body = prefix + ag.SystemPrompt
	} else {
		body = "You are " + ag.Name + ", an expert assistant. " +
			"Use markdown formatting — tables, bold, code blocks, lists — when it improves readability."
	}
	if add := capabilityAddendum(ag); add != "" {
		body += "\n\n" + add
	}
	if add := PersonalityAddendum(ag.Personality); add != "" {
		body += "\n\n" + add
	}
	return body + "\n\n" + ctxText
}

// BuildSkeletonPersonaPrompt returns a minimal system prompt for a turn the
// caller has already classified as trivial (see agent.IsTrivialAsk): an
// identity line plus the short personality addendum, nothing else. It
// deliberately omits capabilityAddendum, codebase context, cross-session
// memory, the team roster, and the available-models block — none of that
// changes the answer to "ping" or "thanks", and skipping it cuts prefill
// cost on local models where prompt size dominates latency (perf wave step
// 2a). Callers MUST gate this on the existing trivial-ask classifier rather
// than inventing a second one, and MUST use the full BuildPersonaPrompt*
// family for every non-trivial turn, delegated worker thread, and coding run.
func BuildSkeletonPersonaPrompt(ag *Agent) string {
	name := ag.Name
	if name == "" {
		name = "assistant"
	}
	body := "You are " + name + "."
	if add := PersonalityAddendum(ag.Personality); add != "" {
		body += "\n\n" + add
	}
	return body
}

// BuildPersonaPromptWithRoster builds the system prompt for a primary agent,
// appending the agent roster when one is provided. Returns the base prompt
// unchanged if roster is empty. Delegation instructions are omitted when
// the agent's model does not support delegation (TierLow).
func BuildPersonaPromptWithRoster(ag *Agent, ctxText, roster string) string {
	return AppendTeamRoster(BuildPersonaPrompt(ag, ctxText), roster, AgentSupportsDelegation(ag))
}

// BuildPersonaPromptWithMemory constructs the system prompt with cross-session context.
func BuildPersonaPromptWithMemory(ag *Agent, ctxText string, recentSummaries []SessionSummary) string {
	base := BuildPersonaPrompt(ag, ctxText)
	if len(recentSummaries) == 0 {
		return base
	}
	var sb strings.Builder
	sb.WriteString(base)
	sb.WriteString("\n\n## Recent Work Context\n")
	for _, s := range recentSummaries {
		sb.WriteString("Session ")
		sb.WriteString(s.Timestamp.Format("2006-01-02"))
		sb.WriteString(": ")
		sb.WriteString(s.Summary)
		if len(s.FilesTouched) > 0 {
			sb.WriteString(". Files: ")
			sb.WriteString(strings.Join(s.FilesTouched, ", "))
		}
		if len(s.Decisions) > 0 {
			sb.WriteString(". Decisions: ")
			sb.WriteString(strings.Join(s.Decisions, "; "))
		}
		if len(s.OpenQuestions) > 0 {
			sb.WriteString(". Open questions: ")
			sb.WriteString(strings.Join(s.OpenQuestions, "; "))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// AgentRegistry holds all named agents by name.
type AgentRegistry struct {
	mu         sync.RWMutex
	agents     map[string]*Agent
	vaultNames map[string]string // vaultName → first agent name that claimed it

	// ephemeral holds one-off specialist agents spawned via spawn_specialist.
	// It is a separate overlay map, keyed lowercase like agents, so it
	// SURVIVES ReloadFromConfig (which only replaces r.agents/r.vaultNames).
	// ByName consults this overlay after the main map; All() and Names()
	// NEVER do — invisibility from the roster, handleListAgents, and mention
	// autocomplete is structural, not a filter applied later.
	ephemeral map[string]*Agent
}

// NewRegistry creates an empty AgentRegistry.
func NewRegistry() *AgentRegistry {
	return &AgentRegistry{
		agents:     make(map[string]*Agent),
		vaultNames: make(map[string]string),
		ephemeral:  make(map[string]*Agent),
	}
}

// NewAgentRegistry is an alias for NewRegistry.
func NewAgentRegistry() *AgentRegistry {
	return NewRegistry()
}

// Register adds an agent to the registry.
// If another agent already claimed the same VaultName, a warning is logged.
func (r *AgentRegistry) Register(a *Agent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[strings.ToLower(a.Name)] = a
	if a.VaultName != "" {
		if owner, collision := r.vaultNames[a.VaultName]; collision && owner != a.Name {
			vaultCollisionCount.Add(1)
			slog.Warn("agents: vault name collision",
				"vault", a.VaultName,
				"existing_agent", owner,
				"new_agent", a.Name,
			)
		} else {
			r.vaultNames[a.VaultName] = a.Name
		}
	}
}

// TryRegister adds an agent to the registry only if no agent with the same name
// (case-insensitive) already exists. Returns ErrDuplicateAgentName when the name
// is taken. Vault collisions are still logged and counted but do not block registration.
func (r *AgentRegistry) TryRegister(a *Agent) error {
	if a == nil {
		return fmt.Errorf("agents: TryRegister called with nil agent")
	}
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("agents: TryRegister called with empty agent name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := strings.ToLower(a.Name)
	if _, exists := r.agents[key]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateAgentName, a.Name)
	}
	if _, exists := r.ephemeral[key]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateAgentName, a.Name)
	}
	r.agents[key] = a
	if a.VaultName != "" {
		if owner, collision := r.vaultNames[a.VaultName]; collision && owner != a.Name {
			vaultCollisionCount.Add(1)
			slog.Warn("agents: vault name collision",
				"vault", a.VaultName,
				"existing_agent", owner,
				"new_agent", a.Name,
			)
		} else {
			r.vaultNames[a.VaultName] = a.Name
		}
	}
	return nil
}

// Unregister removes the agent with the given name (case-insensitive) from the registry.
// No-op if name not found.
func (r *AgentRegistry) Unregister(name string) {
	key := strings.ToLower(name)
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.agents[key]
	if !ok {
		return
	}
	delete(r.agents, key)
}

// ByName looks up an agent by name (case-insensitive). Consults the main
// roster first, then falls back to the ephemeral specialist overlay so
// spawned one-off specialists remain resolvable (e.g. for delegation,
// dispatch, and cost tracking) without appearing in the roster.
func (r *AgentRegistry) ByName(name string) (*Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	key := strings.ToLower(name)
	if a, ok := r.agents[key]; ok {
		return a, ok
	}
	a, ok := r.ephemeral[key]
	return a, ok
}

// RegisterEphemeral adds a one-off specialist agent to the ephemeral overlay.
// The overlay survives ReloadFromConfig. Returns ErrDuplicateAgentName if the
// name (case-insensitive) collides with either the main roster or an
// existing ephemeral entry.
func (r *AgentRegistry) RegisterEphemeral(a *Agent) error {
	if a == nil {
		return fmt.Errorf("agents: RegisterEphemeral called with nil agent")
	}
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("agents: RegisterEphemeral called with empty agent name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := strings.ToLower(a.Name)
	if _, exists := r.agents[key]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateAgentName, a.Name)
	}
	if _, exists := r.ephemeral[key]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateAgentName, a.Name)
	}
	r.ephemeral[key] = a
	return nil
}

// UnregisterEphemeral removes the named agent (case-insensitive) from the
// ephemeral overlay only. No-op if not found there. Never touches the main
// roster, so calling it with a seated agent's name is a safe no-op.
func (r *AgentRegistry) UnregisterEphemeral(name string) {
	key := strings.ToLower(name)
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.ephemeral, key)
}

// IsEphemeral reports whether name (case-insensitive) resolves to an
// ephemeral specialist rather than a seated roster agent.
func (r *AgentRegistry) IsEphemeral(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, inMain := r.agents[strings.ToLower(name)]
	if inMain {
		return false
	}
	_, ok := r.ephemeral[strings.ToLower(name)]
	return ok
}

// EphemeralAgents returns all currently registered ephemeral specialists
// (order not guaranteed). Intended for TTL sweeps and diagnostics — not for
// roster/listing surfaces, which must use All() instead.
func (r *AgentRegistry) EphemeralAgents() []*Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*Agent, 0, len(r.ephemeral))
	for _, a := range r.ephemeral {
		result = append(result, a)
	}
	return result
}

// All returns all registered agents (order not guaranteed).
func (r *AgentRegistry) All() []*Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*Agent, 0, len(r.agents))
	for _, a := range r.agents {
		result = append(result, a)
	}
	return result
}

// Names returns all registered agent names in lowercase.
func (r *AgentRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.agents))
	for name := range r.agents {
		names = append(names, name)
	}
	return names
}

// DefaultAgent returns the agent marked IsDefault=true, or the alphabetically
// first registered agent as a deterministic fallback, or nil if empty.
func (r *AgentRegistry) DefaultAgent() *Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]string, 0, len(r.agents))
	for k := range r.agents {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var first *Agent
	for _, k := range keys {
		a := r.agents[k]
		if first == nil {
			first = a
		}
		if a.IsDefault {
			return a
		}
	}
	return first
}

// DefaultAgentName returns the name of the default agent, or "" if none.
// Implements tools.AgentNameResolver.
func (r *AgentRegistry) DefaultAgentName() string {
	if ag := r.DefaultAgent(); ag != nil {
		return ag.Name
	}
	return ""
}

// SetDefault marks the named agent (case-insensitive) as the default,
// clearing the flag from all other agents. No-op if name not found.
func (r *AgentRegistry) SetDefault(name string) {
	key := strings.ToLower(name)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.agents[key]; !exists {
		return // true no-op: unknown name, leave state unchanged
	}
	for k, a := range r.agents {
		a.IsDefault = k == key
	}
}
