package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/scrypster/huginn/internal/modelconfig"
)

// AgentDef is the on-disk representation of a named agent.
type AgentDef struct {
	Name         string `json:"name"`
	Model        string `json:"model"`
	SystemPrompt string `json:"system_prompt"`
	Color        string `json:"color"`
	Icon         string `json:"icon"`
	ID           string `json:"id,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
	IsDefault    bool   `json:"is_default,omitempty"`
	Provider     string `json:"provider,omitempty"`
	Endpoint     string `json:"endpoint,omitempty"`
	APIKey       string `json:"api_key,omitempty"`

	// VaultName is the fully-qualified MuninnDB vault name for this agent.
	// If empty, defaults to "huginn:agent:<username>:<agentname>".
	VaultName string `json:"vault_name,omitempty"`

	// Plasticity controls the MuninnDB learning rate preset.
	// Values: "default", "knowledge-graph", "reference". Empty = "default".
	Plasticity string `json:"plasticity,omitempty"`

	// MemoryEnabled controls whether this agent uses memory injection.
	// nil means inherit from global config.
	MemoryEnabled *bool `json:"memory_enabled,omitempty"`

	// ContextNotesEnabled enables the persistent memory file for this agent.
	// When true, ~/.huginn/agents/{name}.memory.md is read and injected at conversation start,
	// and the update_memory tool is available for the agent to update the file.
	ContextNotesEnabled bool `json:"context_notes_enabled,omitempty"`

	// MemoryMode controls how aggressively memory tools are used.
	// Values: "passive" (only when asked), "conversational" (default: proactive recall+write),
	// "immersive" (maximum: orientation, feedback, hygiene). Empty → "conversational" at runtime.
	MemoryMode string `json:"memory_mode,omitempty"`

	// VaultDescription is a human-written description of this agent's memory vault.
	// Injected into the system prompt to ground the agent in what its memory is for.
	VaultDescription string `json:"vault_description,omitempty"`

	// MemoryType is a transient API-bridge field used by the frontend.
	// Values: "none", "context", "muninndb". NOT persisted to disk.
	// On PUT: call ApplyMemoryType() to translate to canonical fields before saving.
	// On GET: call DeriveMemoryType() to populate from canonical fields for the response.
	MemoryType string `json:"memory_type,omitempty"`

	// Toolbelt is the list of connections assigned to this agent.
	// Only tools for these providers will be injected into conversation context.
	// Empty toolbelt = no external tools granted (default-deny).
	// Use a wildcard entry (Provider: "*") for allow-all behavior.
	Toolbelt []ToolbeltEntry `json:"toolbelt,omitempty"`

	// Skills is the list of skill names assigned to this agent.
	// Only skills in this list will be injected into the agent's system prompt.
	// If empty, falls back to globally-enabled skills.
	Skills []string `json:"skills"`

	// LocalTools is the allowlist of builtin tool names granted to this agent.
	// Empty/nil = no local tools (default-deny).
	// ["*"] = all builtin tools (God Mode).
	// Named list = only those specific tools.
	LocalTools []string `json:"local_tools,omitempty"`

	// Description is a short (max 500 bytes) human-readable summary of what this agent does.
	// Visible to other agents in channel contexts for intelligent task delegation.
	Description string `json:"description,omitempty"`

	// Version is an optimistic-lock counter incremented on every save.
	// On PUT /api/v1/agents/{name}: if the client sends Version > 0 and it does
	// not match the stored value, the request is rejected with 409 Conflict.
	// Clients that do not track versions should omit the field (or send 0) to
	// skip the conflict check (last-writer-wins semantics).
	Version int `json:"version,omitempty"`
}

// ResolvedVaultName returns the effective vault name for the agent.
// Falls back to "huginn:agent:<username>:<agentname>" if VaultName is empty.
// The agent-name segment is sanitized to contain only [a-z0-9-] characters:
// spaces become hyphens, all other non-alphanumeric characters are dropped.
func (a AgentDef) ResolvedVaultName(username string) string {
	if a.VaultName != "" {
		return a.VaultName
	}
	var sb strings.Builder
	for _, r := range strings.ToLower(a.Name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else if r == ' ' {
			sb.WriteRune('-')
		}
		// all other characters (colons, slashes, @, etc.) are dropped
	}
	return fmt.Sprintf("huginn:agent:%s:%s", username, sb.String())
}

// ResolveAgentVaultName returns the vault name for an agent given its name and the
// resolved username. This is a package-level helper for callers that have a runtime
// Agent (not an AgentDef) and need to derive the vault name pattern.
func ResolveAgentVaultName(agentName, username string) string {
	return AgentDef{Name: agentName}.ResolvedVaultName(username)
}

// CheckVaultNameCollision detects when the incoming agent definition would resolve
// to the same vault name as an existing agent other than the one being updated.
// excludeName is the current on-disk name of the agent being updated (URL path param);
// pass "" when creating a brand-new agent (nothing to exclude).
// username is passed through to ResolvedVaultName (use "" when not known at call time).
func CheckVaultNameCollision(incoming AgentDef, excludeName, username string, allAgents []AgentDef) error {
	vaultName := incoming.ResolvedVaultName(username)
	for _, a := range allAgents {
		// Skip the agent currently being updated (identified by its current name).
		if excludeName != "" && strings.EqualFold(a.Name, excludeName) {
			continue
		}
		if a.ResolvedVaultName(username) == vaultName {
			return fmt.Errorf("vault name %q is already used by agent %q", vaultName, a.Name)
		}
	}
	return nil
}

// ApplyMemoryType translates the transient MemoryType enum to the canonical storage fields
// (MemoryEnabled, ContextNotesEnabled) and then clears MemoryType so it is not persisted.
// An empty MemoryType is a no-op (supports partial PATCH semantics).
// Returns an error for unrecognized values so the API handler can return a 400.
func (d *AgentDef) ApplyMemoryType() error {
	switch d.MemoryType {
	case "":
		return nil // no-op: preserve existing canonical fields
	case "none":
		f := false
		d.MemoryEnabled = &f
		d.ContextNotesEnabled = false
	case "context":
		f := false
		d.MemoryEnabled = &f
		d.ContextNotesEnabled = true
	case "muninndb":
		t := true
		d.MemoryEnabled = &t
		d.ContextNotesEnabled = false
	default:
		return fmt.Errorf("unknown memory_type %q: must be none, context, or muninndb", d.MemoryType)
	}
	d.MemoryType = "" // do not persist — canonical fields are the source of truth
	return nil
}

// DeriveMemoryType populates the transient MemoryType field from the canonical storage
// fields for inclusion in API GET responses. nil MemoryEnabled maps to "none" (not "muninndb")
// so that legacy agents that were never explicitly configured do not falsely appear active.
func (d *AgentDef) DeriveMemoryType() {
	if d.ContextNotesEnabled {
		d.MemoryType = "context"
		return
	}
	if d.MemoryEnabled != nil && *d.MemoryEnabled {
		d.MemoryType = "muninndb"
		return
	}
	d.MemoryType = "none" // nil (never set) or explicit false
}

// validPlasticity contains the recognized plasticity values.
// Empty string is allowed and defaults to "default" at runtime.
var validPlasticity = map[string]bool{
	"":               true,
	"default":        true,
	"knowledge-graph": true,
	"reference":      true,
}

// validMemoryMode contains the recognized memory_mode values.
// Empty string is allowed and defaults to "conversational" at runtime.
var validMemoryMode = map[string]bool{
	"":               true,
	"passive":        true,
	"conversational": true,
	"immersive":      true,
}

// agentColorRE matches a CSS hex color: # followed by exactly 6 hex digits.
var agentColorRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Validate checks AgentDef fields for basic correctness.
// Returns a non-nil error describing the first validation failure.
func (d AgentDef) Validate() error {
	if strings.TrimSpace(d.Name) == "" {
		return fmt.Errorf("agent name is required")
	}
	if len(d.Name) > 128 {
		return fmt.Errorf("agent name must be 128 characters or fewer")
	}
	if d.Color != "" && !agentColorRE.MatchString(d.Color) {
		return fmt.Errorf("color must be a hex color like #rrggbb, got %q", d.Color)
	}
	if len(d.Description) > 500 {
		return fmt.Errorf("agent description must be 500 bytes or fewer")
	}
	if !validPlasticity[d.Plasticity] {
		return fmt.Errorf("invalid plasticity %q: must be one of default, knowledge-graph, reference", d.Plasticity)
	}
	if !validMemoryMode[d.MemoryMode] {
		return fmt.Errorf("invalid memory_mode %q: must be one of passive, conversational, immersive", d.MemoryMode)
	}
	return nil
}

// AgentsConfig is the top-level structure for agents.json.
type AgentsConfig struct {
	Agents []AgentDef `json:"agents"`
}

// FromDef constructs a runtime Agent from a persisted AgentDef.
// Note: MemoryEnabled is a *bool in AgentDef to distinguish "unset" from
// "explicitly false". Here we collapse nil → true (memory enabled by default)
// so the runtime Agent field (a plain bool) is always safe to read without a
// nil check. nil means "inherit from global config; default is enabled (true)".
func FromDef(def AgentDef) *Agent {
	// MemoryEnabled: nil means inherit global config, default is true (memory enabled).
	memEnabled := true
	if def.MemoryEnabled != nil {
		memEnabled = *def.MemoryEnabled
	}
	return &Agent{
		Name:          def.Name,
		ModelID:       def.Model,
		Provider:      def.Provider,
		Endpoint:      def.Endpoint,
		APIKey:        def.APIKey,
		SystemPrompt:  def.SystemPrompt,
		Color:         def.Color,
		Icon:          def.Icon,
		IsDefault:     def.IsDefault,
		VaultName:     def.VaultName,
		Plasticity:    def.Plasticity,
		MemoryEnabled:       memEnabled,
		ContextNotesEnabled: def.ContextNotesEnabled,
		MemoryMode:          def.MemoryMode,
		VaultDescription:    def.VaultDescription,
		Toolbelt:            def.Toolbelt,
		Skills:              def.Skills,
		LocalTools:          def.LocalTools,
	}
}

// DefaultAgentsConfig returns an empty agent configuration.
// Users create their own agents via the web UI.
func DefaultAgentsConfig() *AgentsConfig {
	return &AgentsConfig{Agents: []AgentDef{}}
}

// huginnBaseDir returns ~/.huginn, creating it if needed.
// Respects the HUGINN_HOME environment variable for testing isolation.
func huginnBaseDir() (string, error) {
	base := os.Getenv("HUGINN_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("home dir: %w", err)
		}
		base = home
	}
	dir := filepath.Join(base, ".huginn")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("mkdir huginn: %w", err)
	}
	return dir, nil
}

// LoadAgents reads agents from ~/.huginn/agents/*.json (per-file format).
// Falls back to ~/.huginn/agents.json for backward compatibility.
// Returns defaults if neither exists.
func LoadAgents() (*AgentsConfig, error) {
	baseDir, err := huginnBaseDir()
	if err != nil {
		return nil, err
	}
	return loadAgentsFromBase(baseDir)
}

// loadAgentsFromBase loads agents from baseDir using per-file format with
// fallback to legacy agents.json. Returns defaults if nothing found.
func loadAgentsFromBase(baseDir string) (*AgentsConfig, error) {
	// First: try per-file format ~/.huginn/agents/*.json
	agentsDir := filepath.Join(baseDir, "agents")
	entries, err := filepath.Glob(filepath.Join(agentsDir, "*.json"))
	if err == nil && len(entries) > 0 {
		var agentsList []AgentDef
		for _, path := range entries {
			// Skip draft files
			if filepath.Base(path) == ".draft.json" {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				continue // skip unreadable
			}
			var agent AgentDef
			if err := json.Unmarshal(data, &agent); err != nil {
				continue // skip corrupt
			}
			agentsList = append(agentsList, agent)
		}
		if len(agentsList) > 0 {
			return &AgentsConfig{Agents: agentsList}, nil
		}
	}

	// Fallback: legacy single agents.json
	legacyPath := filepath.Join(baseDir, "agents.json")
	return LoadAgentsFrom(legacyPath)
}

// LoadAgentsFrom reads agents from the given path, returning defaults if file missing or empty.
func LoadAgentsFrom(path string) (*AgentsConfig, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultAgentsConfig(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read agents: %w", err)
	}
	// Treat a zero-length file the same as a missing file so that an
	// accidentally truncated agents.json doesn't cause a parse error.
	if len(data) == 0 {
		return DefaultAgentsConfig(), nil
	}
	var cfg AgentsConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse agents: %w", err)
	}
	return &cfg, nil
}

// SaveAgents writes agents to ~/.huginn/agents/<name>.json (one file per agent).
// This is the preferred save path; it replaces the old single-file approach.
func SaveAgents(cfg *AgentsConfig) error {
	baseDir, err := huginnBaseDir()
	if err != nil {
		return err
	}
	for _, def := range cfg.Agents {
		if err := SaveAgent(baseDir, def); err != nil {
			return err
		}
	}
	return nil
}

// SaveAgentsTo writes agents to the given path, creating parent directories as needed.
// Kept for backward compatibility and tests.
func SaveAgentsTo(cfg *AgentsConfig, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

// SaveAgent saves a single agent definition to <baseDir>/agents/<name>.json.
// Creates the directory if it does not exist. Uses an atomic write.
// Increments agent.Version inside the function so the on-disk counter is
// always larger than the last-seen client value — safe for optimistic locking.
func SaveAgent(baseDir string, agent AgentDef) error {
	agentsDir := filepath.Join(baseDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o700); err != nil {
		return fmt.Errorf("create agents dir: %w", err)
	}

	// Increment version inside SaveAgent so the bump is atomic with the write;
	// callers never need to manage the counter themselves.
	agent.Version++

	data, err := json.MarshalIndent(agent, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal agent: %w", err)
	}

	name := agent.Name
	if name == "" {
		name = "unnamed"
	}
	safe := sanitizeAgentName(name)
	path := filepath.Join(agentsDir, safe+".json")

	// Atomic write via temp file + rename.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write agent tmp: %w", err)
	}
	return os.Rename(tmp, path)
}

// DeleteAgent removes an agent's file from <baseDir>/agents/.
func DeleteAgent(baseDir, name string) error {
	safe := sanitizeAgentName(name)
	path := filepath.Join(baseDir, "agents", safe+".json")
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("agent %q not found", name)
	}
	return err
}

// sanitizeAgentName returns a safe filename component for an agent name.
// Keeps only lowercase letters, digits, hyphens, and underscores.
func sanitizeAgentName(name string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('_')
		}
	}
	return sb.String()
}

// SaveAgentDefault saves a single agent to ~/.huginn/agents/<name>.json.
func SaveAgentDefault(agent AgentDef) error {
	baseDir, err := huginnBaseDir()
	if err != nil {
		return err
	}
	return SaveAgent(baseDir, agent)
}

// DeleteAgentDefault removes an agent file from ~/.huginn/agents/.
func DeleteAgentDefault(name string) error {
	baseDir, err := huginnBaseDir()
	if err != nil {
		return err
	}
	return DeleteAgent(baseDir, name)
}

// InferProvider returns the likely provider for a model name.
// Returns "" when the model doesn't match a known prefix.
func InferProvider(model string) string {
	lower := strings.ToLower(model)
	switch {
	case strings.HasPrefix(lower, "claude"):
		return "anthropic"
	case strings.HasPrefix(lower, "gpt-") || strings.HasPrefix(lower, "o1") || strings.HasPrefix(lower, "o3"):
		return "openai"
	case strings.HasPrefix(lower, "gemini"):
		return "google"
	case strings.HasPrefix(lower, "llama") || strings.HasPrefix(lower, "mistral"):
		return "ollama"
	default:
		return ""
	}
}

// MigrateAgents migrates from the legacy ~/.huginn/agents.json to per-agent files
// in ~/.huginn/agents/<name>.json. If migration succeeds, renames agents.json to
// agents.json.bak. Safe to call multiple times (idempotent).
func MigrateAgents(baseDir string) error {
	legacyPath := filepath.Join(baseDir, "agents.json")
	data, err := os.ReadFile(legacyPath)
	if os.IsNotExist(err) {
		return nil // nothing to migrate
	}
	if err != nil {
		return err
	}

	var cfg AgentsConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse legacy agents.json: %w", err)
	}

	if err := os.MkdirAll(filepath.Join(baseDir, "agents"), 0o700); err != nil {
		return err
	}

	for _, agent := range cfg.Agents {
		if err := SaveAgent(baseDir, agent); err != nil {
			return fmt.Errorf("migrate agent %s: %w", agent.Name, err)
		}
	}

	// Rename original to .bak
	return os.Rename(legacyPath, legacyPath+".bak")
}

// BuildRegistry constructs an AgentRegistry from a loaded AgentsConfig.
func BuildRegistry(cfg *AgentsConfig, models *modelconfig.Models) *AgentRegistry {
	return BuildRegistryWithUsername(cfg, models, "")
}

// BuildRegistryWithUsername constructs an AgentRegistry from a loaded AgentsConfig
// and populates each agent's VaultName using ResolvedVaultName(username).
// If username is empty, the vault name falls back to the def.VaultName field (if set)
// or a best-effort pattern without a username segment.
func BuildRegistryWithUsername(cfg *AgentsConfig, models *modelconfig.Models, username string) *AgentRegistry {
	reg := NewRegistry()
	for _, def := range cfg.Agents {
		a := FromDef(def)
		// Always resolve the effective vault name so per-agent memory activation
		// works even when VaultName was not explicitly set in agents.json.
		a.VaultName = def.ResolvedVaultName(username)
		// Populate plasticity from the def (FromDef already set it; ensure non-empty).
		if def.Plasticity != "" {
			a.Plasticity = def.Plasticity
		} else if a.Plasticity == "" {
			a.Plasticity = "default"
		}
		// MemoryEnabled is already set by FromDef (nil → true default).
		reg.Register(a)
	}
	return reg
}
