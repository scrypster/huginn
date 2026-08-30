package modelconfig

import (
	"strings"
	"sync"
	"unicode"
)

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
// Populated by probing the backend at startup and when listing models.
type ModelRegistry struct {
	mu        sync.RWMutex
	Available []ModelInfo `json:"available"`
}

// ReplaceAvailable swaps the probed model list. Safe for concurrent readers.
func (r *ModelRegistry) ReplaceAvailable(models []ModelInfo) {
	if r == nil {
		return
	}
	copied := make([]ModelInfo, len(models))
	copy(copied, models)
	r.mu.Lock()
	r.Available = copied
	r.mu.Unlock()
}

// NewRegistry builds an empty registry.
func NewRegistry(models *Models) *ModelRegistry {
	return &ModelRegistry{}
}

// ModelContextWindow returns the context window for the named model (0 = unknown).
func (r *ModelRegistry) ModelContextWindow(modelName string) int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, m := range r.Available {
		if m.Name == modelName {
			return m.ContextWindow
		}
	}
	return 0
}

// Lookup returns a copy of the named model's info, or nil if unknown.
func (r *ModelRegistry) Lookup(modelName string) *ModelInfo {
	if r == nil || modelName == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, m := range r.Available {
		if m.Name == modelName {
			cp := m
			return &cp
		}
	}
	return nil
}

// ModelSupportsTools returns true if the named model supports tool calling.
// Defaults to true when unknown.
func (r *ModelRegistry) ModelSupportsTools(modelName string) bool {
	if r == nil {
		return true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
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
	if r == nil {
		return true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
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

// IsLowTierToolClass reports 7b-class names that do not reliably emit tool_calls,
// even when Ollama advertises a `tools` capability. Matches 7b / 3b / tiny as
// size tokens so 14b / 13b / 70b stay quiet.
func IsLowTierToolClass(name string) bool {
	n := strings.ToLower(name)
	if n == "" {
		return false
	}
	if hasSizeToken(n, "7b") || hasSizeToken(n, "3b") {
		return true
	}
	return strings.HasPrefix(n, "tiny") || hasSizeToken(n, "tiny")
}

// UnreliableForTools is the picker warning rule: 7b-class names or an explicit
// SupportsTools=false. 14b+ and probed-tools models without a low-tier name do
// not warn.
func UnreliableForTools(name string, supportsTools bool) bool {
	if IsLowTierToolClass(name) {
		return true
	}
	return !supportsTools
}

func hasSizeToken(name, token string) bool {
	for i := 0; i+len(token) <= len(name); i++ {
		if name[i:i+len(token)] != token {
			continue
		}
		leftOK := i == 0 || !isTokenChar(rune(name[i-1]))
		rightOK := i+len(token) == len(name) || !isTokenChar(rune(name[i+len(token)]))
		if leftOK && rightOK {
			return true
		}
	}
	return false
}

func isTokenChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
