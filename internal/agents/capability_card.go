// internal/agents/capability_card.go
package agents

import (
	"fmt"
	"strings"

	"github.com/scrypster/huginn/internal/modelconfig"
)

// CapabilityCardInput holds the data needed to generate a capability card.
// Populate from an *Agent (roster.go) or AgentDef (ws.go) at the call site.
type CapabilityCardInput struct {
	Name         string
	SystemPrompt string
	Description  string // optional user override for the Role line
	ModelID      string // for tier/tools annotation; empty or nil infoFn = no annotation
	LocalTools   []string
	Toolbelt     []ToolbeltEntry
	Skills       []string
	MemoryMode   string
}

// providerDisplayNames maps provider slugs to human-readable display names.
var providerDisplayNames = map[string]string{
	"github":          "GitHub",
	"google":          "Google",
	"google-calendar": "Google Calendar",
	"google-drive":    "Google Drive",
	"notion":          "Notion",
	"slack":           "Slack",
	"jira":            "Jira",
	"linear":          "Linear",
	"twilio":          "Twilio",
	"stripe":          "Stripe",
	"openai":          "OpenAI",
	"anthropic":       "Anthropic",
}

// providerDisplayName returns a human-readable name for a provider slug.
// Falls back to title-casing the first letter if not in the lookup map.
func providerDisplayName(slug string) string {
	if name, ok := providerDisplayNames[slug]; ok {
		return name
	}
	if len(slug) == 0 {
		return slug
	}
	return strings.ToUpper(slug[:1]) + slug[1:]
}

// extractRoleBlurb returns the role text for the card's Role line.
// If descriptionOverride is non-empty, it is used directly.
// Otherwise, the first sentence of systemPrompt is extracted (max 200 chars).
// "You are <Name>, " prefix is stripped.
func extractRoleBlurb(systemPrompt, descriptionOverride string) string {
	if descriptionOverride != "" {
		return descriptionOverride
	}
	prompt := strings.TrimSpace(systemPrompt)
	if prompt == "" {
		return ""
	}
	// Strip "You are <Name>, " prefix (first comma within first 20 chars).
	if i := strings.Index(prompt, ", "); i > 0 && i < 20 {
		prompt = strings.TrimSpace(prompt[i+2:])
	}
	// Take first sentence.
	if i := strings.IndexAny(prompt, ".!?"); i > 0 {
		prompt = prompt[:i]
	}
	if len(prompt) > 200 {
		prompt = prompt[:197] + "..."
	}
	return prompt
}

// BuildCapabilityCard generates a deterministic, multi-line capability card for an agent.
// The card is outward-facing — it describes the agent to other agents for delegation purposes.
//
// Format:
//
//   - AgentName [tier, tools: yes/no]
//     Role: <role blurb>
//     Tools: <local tools>        (omitted if empty)
//     Connections: <providers>    (omitted if empty)
//     Skills: <skills>            (omitted if empty)
//     Memory: <mode>
func BuildCapabilityCard(in CapabilityCardInput, infoFn ModelInfoFn) string {
	var sb strings.Builder

	// Header: name + optional tier annotation
	sb.WriteString("- ")
	sb.WriteString(in.Name)
	if infoFn != nil && in.ModelID != "" {
		info := infoFn(in.ModelID)
		if info != nil {
			tier := "capable"
			switch info.Tier {
			case modelconfig.TierMedium:
				tier = "medium"
			case modelconfig.TierLow:
				tier = "low"
			}
			toolsLabel := "yes"
			if !info.SupportsTools {
				toolsLabel = "no"
			}
			fmt.Fprintf(&sb, " [%s, tools: %s]", tier, toolsLabel)
		}
	}
	sb.WriteString("\n")

	// Role
	if role := extractRoleBlurb(in.SystemPrompt, in.Description); role != "" {
		sb.WriteString("  Role: ")
		sb.WriteString(role)
		sb.WriteString("\n")
	}

	// Local tools
	if len(in.LocalTools) > 0 {
		sb.WriteString("  Tools: ")
		sb.WriteString(strings.Join(in.LocalTools, ", "))
		sb.WriteString("\n")
	}

	// Connections (toolbelt provider display names)
	if providers := ToolbeltProviders(in.Toolbelt); len(providers) > 0 {
		names := make([]string, len(providers))
		for i, p := range providers {
			names[i] = providerDisplayName(p)
		}
		sb.WriteString("  Connections: ")
		sb.WriteString(strings.Join(names, ", "))
		sb.WriteString("\n")
	}

	// Skills
	if len(in.Skills) > 0 {
		sb.WriteString("  Skills: ")
		sb.WriteString(strings.Join(in.Skills, ", "))
		sb.WriteString("\n")
	}

	// Memory mode
	mode := in.MemoryMode
	if mode == "" {
		mode = "conversational"
	}
	sb.WriteString("  Memory: ")
	sb.WriteString(mode)
	sb.WriteString("\n")

	return sb.String()
}
