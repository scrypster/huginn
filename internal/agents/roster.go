package agents

import (
	"strings"

	"github.com/scrypster/huginn/internal/modelconfig"
)

// ModelInfoFn resolves a model ID to its ModelInfo (with capabilities).
// Pass nil to skip capability annotations.
type ModelInfoFn func(modelID string) *modelconfig.ModelInfo

// BuildRoster constructs the agent roster string injected into the primary
// agent's system prompt. It excludes the primary agent itself (primaryName,
// case-insensitive) and returns an empty string if no other agents exist.
//
// Each agent is represented as a capability card (multi-line).
func BuildRoster(reg *AgentRegistry, infoFn ModelInfoFn, primaryName string) string {
	all := reg.All()

	var parts []string
	for _, ag := range all {
		if strings.EqualFold(ag.Name, primaryName) {
			continue // exclude self
		}
		card := BuildCapabilityCard(CapabilityCardInput{
			Name:         ag.Name,
			SystemPrompt: ag.SystemPrompt,
			ModelID:      ag.ModelID,
			LocalTools:   ag.LocalTools,
			Toolbelt:     ag.Toolbelt,
			Skills:       ag.Skills,
			MemoryMode:   ag.MemoryMode,
		}, infoFn)
		parts = append(parts, card)
	}

	if len(parts) == 0 {
		return ""
	}
	return "Available team members:\n" + strings.Join(parts, "")
}
