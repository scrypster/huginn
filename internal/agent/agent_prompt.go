package agent

import (
	"fmt"
	"strings"

	"github.com/scrypster/huginn/internal/tools"
)

// buildAgentSystemPrompt creates the system prompt for the agentic loop.
// Tool schemas are sent separately via ChatRequest.Tools, so we do not
// duplicate them as Markdown here — that wastes context and can confuse models
// when the prose description disagrees with the JSON schema.
//
// agentName is injected first ("Your name is {name}.") so the agent knows its identity.
// contextNotesBlock is the pre-built <context-notes> block (or "" if not enabled).
// agentMemoryMode controls how MuninnDB tools are used (passive/conversational/immersive).
// agentVaultName is the agent's personal memory vault (for instruction injection only).
// agentVaultDescription describes the vault's purpose.
func buildAgentSystemPrompt(contextText string, agentSkillsFragment string, reg *tools.Registry, globalInstructions, projectInstructions, agentName, contextNotesBlock string, agentMemoryMode, agentVaultName, agentVaultDescription string) string {
	var sb strings.Builder

	// 1. Agent identity — always first so the model knows its name.
	if agentName != "" {
		sb.WriteString("Your name is ")
		sb.WriteString(agentName)
		sb.WriteString(".\n\n")
	}

	// 2. Global instructions (highest-level user preferences).
	if globalInstructions != "" {
		sb.WriteString(globalInstructions)
		sb.WriteString("\n\n")
	}

	// 3. Project instructions (repo-specific overrides/context).
	if projectInstructions != "" {
		sb.WriteString(projectInstructions)
		sb.WriteString("\n\n")
	}

	// 4. Base Huginn identity.
	sb.WriteString("You are Huginn, an expert AI coding assistant. ")
	sb.WriteString("You can use tools to read files, edit code, run commands, and search the codebase.\n\n")
	sb.WriteString("When you need to make changes or gather information, use the available tools. ")
	sb.WriteString("Always verify your work by reading files before and after editing them.\n\n")
	if reg != nil {
		if _, ok := reg.Get("run_tests"); ok {
			sb.WriteString("After making code changes, ALWAYS run the relevant tests using the run_tests tool. ")
			sb.WriteString("Do not consider your task complete until all tests pass.\n\n")
		}
	}

	// 5. Per-agent skills fragment (injected after identity, before context notes).
	if agentSkillsFragment != "" {
		sb.WriteString("## Active Skills\n\n")
		sb.WriteString(agentSkillsFragment)
		sb.WriteString("\n\n")
	}

	// 6. Context notes (persistent memory file, injected before MuninnDB memories).
	if contextNotesBlock != "" {
		sb.WriteString(contextNotesBlock)
		sb.WriteString("\n\n")
	}

	// 7. MuninnDB memory instruction (only if muninn_recall OR muninn_where_left_off registered).
	if reg != nil {
		_, hasRecall := reg.Get("muninn_recall")
		_, hasWhere := reg.Get("muninn_where_left_off")
		if hasRecall || hasWhere {
			sb.WriteString(memoryModeInstruction(agentMemoryMode, agentVaultName, agentVaultDescription))
		}
	}

	if contextText != "" {
		sb.WriteString(contextText)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// memoryModeInstruction returns the system prompt section for MuninnDB memory tools.
// Only injected when MuninnDB MCP tools are registered (muninn_recall or muninn_where_left_off).
func memoryModeInstruction(mode, vault, vaultDescription string) string {
	if mode == "" {
		mode = "conversational"
	}
	var sb strings.Builder
	sb.WriteString("## Memory\n\n")
	if vaultDescription != "" {
		sb.WriteString(fmt.Sprintf("Your memory vault (%s) is described as: %s\n\n", vault, vaultDescription))
	}
	switch mode {
	case "passive":
		sb.WriteString("Use memory tools only when the user explicitly asks you to remember or recall something. " +
			"Do not proactively read or write memory during normal conversation.\n\n")
	case "immersive":
		sb.WriteString("At the start of every session, call `muninn_where_left_off` to re-orient yourself. " +
			"Before any significant decision or action, call `muninn_recall` passing the relevant topic as the `context` parameter. " +
			"After every response that used recalled memories, call `muninn_feedback` to signal whether each memory was helpful — " +
			"this is how your recall improves over time (SGD learning loop). " +
			"Write atomic memories for every new fact, decision, or preference you learn. " +
			"Use `muninn_evolve` to update stale facts rather than writing duplicates. " +
			"Periodically call `muninn_consolidate` when memories feel redundant.\n\n" +
			"You are a long-term collaborator, not a session assistant. Act like someone who genuinely remembers.\n\n")
	default: // "conversational"
		sb.WriteString("At the start of each conversation, call `muninn_recall` passing the user's message text as the `context` parameter. " +
			"Write atomic memories for new facts, decisions, and preferences you learn. " +
			"After using a recalled memory to answer a question, call `muninn_feedback` to signal whether it was helpful — " +
			"this improves recall quality over time. " +
			"Use `muninn_evolve` to update outdated facts. " +
			"Do not flood memory with obvious or session-specific details.\n\n")
	}
	sb.WriteString("Speak naturally — never mention 'engram IDs', 'vault names', or internal MuninnDB terminology to the user. " +
		"If you're unsure what you remember, say so honestly. Memory enriches your responses; it doesn't replace judgment.\n\n")
	return sb.String()
}
