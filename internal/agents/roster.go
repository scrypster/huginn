package agents

import (
	"fmt"
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
// Format example:
//
//	Available team members:
//	- Stacy [capable, tools: yes] — Pragmatic senior engineer
//	- Sam [medium, tools: yes] — QA engineer
func BuildRoster(reg *AgentRegistry, infoFn ModelInfoFn, primaryName string) string {
	all := reg.All()

	var lines []string
	for _, ag := range all {
		if strings.EqualFold(ag.Name, primaryName) {
			continue // exclude self
		}

		tier := "capable"
		toolsLabel := "unknown"
		if infoFn != nil && ag.ModelID != "" {
			info := infoFn(ag.ModelID)
			if info != nil {
				switch info.Tier {
				case modelconfig.TierHigh:
					tier = "capable"
				case modelconfig.TierMedium:
					tier = "medium"
				default:
					tier = "low"
				}
				if info.SupportsTools {
					toolsLabel = "yes"
				} else {
					toolsLabel = "no"
				}
			}
		}

		persona := extractPersonaBlurb(ag.SystemPrompt)
		if persona != "" {
			lines = append(lines, fmt.Sprintf("- %s [%s, tools: %s] — %s", ag.Name, tier, toolsLabel, persona))
		} else {
			lines = append(lines, fmt.Sprintf("- %s [%s, tools: %s]", ag.Name, tier, toolsLabel))
		}
	}

	if len(lines) == 0 {
		return ""
	}
	return "Available team members:\n" + strings.Join(lines, "\n")
}

// extractPersonaBlurb extracts a short (<=60 char) blurb from a system prompt.
// Strips "You are <Name>, " prefix. Takes the first sentence or first 60 chars.
func extractPersonaBlurb(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	// Strip "You are <Name>, " prefix if present (first comma within first 20 chars).
	if i := strings.Index(prompt, ", "); i > 0 && i < 20 {
		prompt = strings.TrimSpace(prompt[i+2:])
	}
	// First sentence.
	if i := strings.IndexAny(prompt, ".!?"); i > 0 {
		prompt = prompt[:i]
	}
	if len(prompt) > 60 {
		prompt = prompt[:57] + "..."
	}
	return prompt
}
