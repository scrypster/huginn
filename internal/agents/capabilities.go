package agents

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/scrypster/huginn/internal/modelconfig"
)

// modelSizeRe pulls a parameter-count hint like "7b" or "35b" out of a model
// ID (e.g. "qwen2.5-coder:7b", "qwen3.6:35b").
var modelSizeRe = regexp.MustCompile(`(\d+)\s*b\b`)

const teamRosterDelegateHint = "Use `delegate_to_agent` to assign sub-tasks to team members, then `wait_for_threads` to collect their full results when you need them before replying. " +
	"Only delegate when the request clearly requires another agent's specialized expertise. " +
	"For simple conversational messages (greetings, questions, general chat), respond directly — do not delegate."

// InferModelInfo classifies a model ID via InferCapabilities.
// Empty IDs return nil (no annotation). Unknown names default to TierLow.
func InferModelInfo(modelID string) *modelconfig.ModelInfo {
	if strings.TrimSpace(modelID) == "" {
		return nil
	}
	info := &modelconfig.ModelInfo{Name: modelID, SupportsTools: true}
	info.InferCapabilities()
	return info
}

// RegistryModelInfoFn prefers a live registry probe, then InferModelInfo.
func RegistryModelInfoFn(reg *modelconfig.ModelRegistry) ModelInfoFn {
	return func(modelID string) *modelconfig.ModelInfo {
		if looked := reg.Lookup(modelID); looked != nil {
			looked.InferCapabilities()
			return looked
		}
		return InferModelInfo(modelID)
	}
}

// AgentSupportsDelegation reports whether this agent's model may receive
// delegate_to_agent. Empty/unknown model IDs stay optimistic (true) so
// existing unnamed-model tests and unprobed agents keep current behavior.
// TierLow (7b) is false.
func AgentSupportsDelegation(ag *Agent) bool {
	if ag == nil {
		return false
	}
	id := ag.GetModelID()
	if strings.TrimSpace(id) == "" {
		return true
	}
	info := InferModelInfo(id)
	if info == nil {
		return true
	}
	if info.SupportsDelegation {
		return true
	}
	// info.SupportsDelegation is false, meaning InferCapabilities fell
	// through to its TierLow default — it found no high/medium tier name
	// pattern. That default exists to block genuinely small local models
	// ("qwen2.5-coder:7b"), not to silently deny delegation to every model
	// whose name it doesn't recognize (test doubles, new/unlisted model
	// families, larger local models like "qwen3.6:35b"). Only trust the
	// TierLow verdict when the name itself signals a small parameter
	// count; otherwise stay optimistic, per this function's own contract.
	if m := modelSizeRe.FindStringSubmatch(strings.ToLower(id)); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n >= 13 // mirrors InferCapabilities' medium-tier "13b"/"14b" patterns
		}
		return false
	}
	return true
}

func agentHasImageGeneration(ag *Agent) bool {
	if ag == nil {
		return false
	}
	for _, n := range ag.LocalTools {
		switch strings.ToLower(strings.TrimSpace(n)) {
		case "generate_image", "image", "dalle", "imagen":
			return true
		}
	}
	return false
}

// capabilityAddendum tells the agent its grants and limits like a teammate.
func capabilityAddendum(ag *Agent) string {
	if ag == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Your capabilities\n")
	if len(ag.LocalTools) == 0 {
		b.WriteString("Local tools: no local tools\n")
	} else {
		b.WriteString("Local tools: ")
		b.WriteString(strings.Join(ag.LocalTools, ", "))
		b.WriteString("\n")
	}
	if !agentHasImageGeneration(ag) {
		b.WriteString("You do not have image generation.\n")
	}
	if !AgentSupportsDelegation(ag) {
		b.WriteString("You cannot delegate.\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// AppendTeamRoster adds capability cards and, when the agent can delegate,
// the delegate_to_agent instruction. Empty roster is a no-op.
func AppendTeamRoster(base, roster string, canDelegate bool) string {
	if roster == "" {
		return base
	}
	out := base + "\n\n## Your Team\n" + roster
	if canDelegate {
		out += "\n\n" + teamRosterDelegateHint
	}
	return out
}
