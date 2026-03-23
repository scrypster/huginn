package modelconfig

import "strings"

// CapabilityTier classifies a model's orchestration capability.
type CapabilityTier string

const (
	TierHigh   CapabilityTier = "high"   // Anthropic Opus/Sonnet, GPT-4 class
	TierMedium CapabilityTier = "medium" // Haiku, GPT-4o-mini, 14B+ local with tools
	TierLow    CapabilityTier = "low"    // 7B-13B local, no tool support
)

// highTierPatterns are name substrings that identify high-tier models.
var highTierPatterns = []string{
	"opus", "sonnet", "gpt-4", "gpt4",
}

// mediumTierPatterns are name substrings that identify medium-tier models.
var mediumTierPatterns = []string{
	"haiku", "gpt-4o-mini", "14b", "13b",
}

// ModelInfo holds capability info for a single model.
type ModelInfo struct {
	Name          string `json:"name"`
	ContextWindow int    `json:"contextWindow"`
	SupportsTools bool   `json:"supportsTools"`

	// Populated by InferCapabilities after probing the backend.
	Tier               CapabilityTier `json:"tier,omitempty"`
	SupportsDelegation bool           `json:"supportsDelegation,omitempty"`
	ReliableFinish     bool           `json:"reliableFinish,omitempty"`
	PromptBudget       int            `json:"promptBudget,omitempty"`
}

// InferCapabilities populates Tier, SupportsDelegation, ReliableFinish, and
// PromptBudget from the model's Name, ContextWindow, and SupportsTools.
// Safe to call multiple times (idempotent).
func (m *ModelInfo) InferCapabilities() {
	name := strings.ToLower(m.Name)
	for _, p := range mediumTierPatterns {
		if strings.Contains(name, p) {
			m.Tier = TierMedium
			m.SupportsDelegation = m.SupportsTools
			m.ReliableFinish = m.SupportsTools
			m.PromptBudget = 4096
			return
		}
	}
	for _, p := range highTierPatterns {
		if strings.Contains(name, p) {
			m.Tier = TierHigh
			m.SupportsDelegation = true
			m.ReliableFinish = true
			m.PromptBudget = 8192
			return
		}
	}
	// Default: low tier
	m.Tier = TierLow
	m.SupportsDelegation = false
	m.ReliableFinish = false
	m.PromptBudget = 1024
}

// ModelRegistry holds capability info for available models.
// Populated by probing the backend at startup.
type ModelRegistry struct {
	Available []ModelInfo `json:"available"`
}

// NewRegistry builds an empty registry.
func NewRegistry(models *Models) *ModelRegistry {
	return &ModelRegistry{}
}

// ModelContextWindow returns the context window for the named model (0 = unknown).
func (r *ModelRegistry) ModelContextWindow(modelName string) int {
	for _, m := range r.Available {
		if m.Name == modelName {
			return m.ContextWindow
		}
	}
	return 0
}

// ModelSupportsTools returns true if the named model supports tool calling.
// Defaults to true when unknown.
func (r *ModelRegistry) ModelSupportsTools(modelName string) bool {
	for _, m := range r.Available {
		if m.Name == modelName {
			return m.SupportsTools
		}
	}
	return true // optimistic default
}

// HasModel returns true if modelName appears in the Available list.
// When Available is empty (e.g. backend not yet probed) it returns true so that
// callers don't incorrectly treat unprobed models as stale.
func (r *ModelRegistry) HasModel(modelName string) bool {
	if len(r.Available) == 0 {
		return true // cannot validate yet — optimistic default
	}
	for _, m := range r.Available {
		if m.Name == modelName {
			return true
		}
	}
	return false
}
