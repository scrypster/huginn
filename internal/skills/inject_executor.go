package skills

import (
	"github.com/scrypster/huginn/internal/tools"
)

// InjectAgentExecutor injects an AgentExecutor into all PromptTools registered in the given registry.
// This enables agent-mode skill tools to make LLM calls. Call this after the orchestrator is created
// and tools are registered.
func InjectAgentExecutor(toolReg *tools.Registry, executor AgentExecutor) {
	if toolReg == nil || executor == nil {
		return
	}
	// Get all tools from the registry
	allTools := toolReg.All()
	for _, t := range allTools {
		// Only PromptTools have SetAgentExecutor; other tools ignore the call.
		// Since PromptTool is in the skills package, we can safely type-assert here.
		if pt, ok := t.(*PromptTool); ok {
			pt.SetAgentExecutor(executor)
		}
	}
}
