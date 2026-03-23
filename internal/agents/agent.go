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

	Name          string
	SystemPrompt  string
	Color         string // lipgloss hex, e.g. "#58A6FF"
	Icon          string // single char, e.g. "C"
	IsDefault     bool
	ModelID       string
	Provider      string
	Endpoint      string
	APIKey        string
	History       []backend.Message
	VaultName     string
	Plasticity    string
	MemoryEnabled       bool
	ContextNotesEnabled bool
	MemoryMode          string
	VaultDescription    string
	Toolbelt            []ToolbeltEntry
	Skills              []string
	LocalTools          []string // tool names granted to this agent; ["*"] = all builtins
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
	cp := Agent{
		// mu is intentionally zero-valued in the copy.
		Name:                a.Name,
		SystemPrompt:        a.SystemPrompt + extraSystem,
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
		Toolbelt:            a.Toolbelt,
		Skills:              a.Skills,
		LocalTools:          a.LocalTools,
		// History is intentionally not copied — the copy is request-scoped.
	}
	a.mu.Unlock()
	return &cp
}

// SwapModel hot-swaps the model behind this agent. Thread-safe.
func (a *Agent) SwapModel(modelID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ModelID = modelID
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
func BuildPersonaPrompt(ag *Agent, ctxText string) string {
	if ag.SystemPrompt != "" {
		return ag.SystemPrompt + "\n\n" + ctxText
	}
	return "You are " + ag.Name + ", an expert assistant. " +
		"Use markdown formatting — tables, bold, code blocks, lists — when it improves readability.\n\n" + ctxText
}

// BuildPersonaPromptWithRoster builds the system prompt for a primary agent,
// appending the agent roster when one is provided. Returns the base prompt
// unchanged if roster is empty.
func BuildPersonaPromptWithRoster(ag *Agent, ctxText, roster string) string {
	base := BuildPersonaPrompt(ag, ctxText)
	if roster == "" {
		return base
	}
	return base + "\n\n## Your Team\n" + roster +
		"\n\nUse `delegate_to_agent` to assign sub-tasks to team members."
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
}

// NewRegistry creates an empty AgentRegistry.
func NewRegistry() *AgentRegistry {
	return &AgentRegistry{
		agents:     make(map[string]*Agent),
		vaultNames: make(map[string]string),
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

// ByName looks up an agent by name (case-insensitive).
func (r *AgentRegistry) ByName(name string) (*Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.agents[strings.ToLower(name)]
	return a, ok
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
