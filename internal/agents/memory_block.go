package agents

import (
	"fmt"
	"strings"
)

// BuildMemoryBlock returns memory system instructions for injection into the agent's
// system prompt. Returns "" when memory is disabled or the vault is unconfigured,
// so callers never inject instructions that reference tools that aren't available.
//
// This function should only be called when the vault connection was successfully
// established (i.e., from vaultResult.memoryBlock set in connectAgentVault).
func BuildMemoryBlock(ag *Agent) string {
	if !ag.MemoryEnabled || ag.VaultName == "" {
		return ""
	}
	mode := ag.MemoryMode
	if mode == "" {
		mode = "conversational"
	}

	var sb strings.Builder
	sb.WriteString("\n\n## Memory System\n")
	if ag.VaultDescription != "" {
		fmt.Fprintf(&sb, "Your memory vault (%s) purpose: %s\n\n", ag.VaultName, ag.VaultDescription)
	}
	switch mode {
	case "passive":
		sb.WriteString("Memory mode: **passive** — only use memory tools when explicitly asked.")
	case "immersive":
		sb.WriteString("Memory mode: **immersive** — at conversation start recall relevant context; " +
			"throughout capture decisions and patterns; before ending run hygiene (dedup, link related memories).")
	default: // conversational
		sb.WriteString("Memory mode: **conversational** — proactively recall relevant context when starting tasks; " +
			"store important decisions and insights as they emerge.")
	}
	return sb.String()
}
