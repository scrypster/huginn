package threadmgr

import (
	"context"
	"fmt"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// DelegateResult is returned by DelegateFn after creating a thread.
type DelegateResult struct {
	ThreadID  string
	Spawned   bool     // true if the thread was spawned immediately
	Conflicts []string // non-empty if file lease conflicts prevented creation
	Err       error
}

// DelegateFn is the function DelegateToAgentTool calls to create and optionally
// spawn a thread. Callers wire this up to threadmgr and agents logic.
//
//	func myDelegate(ctx context.Context, p threadmgr.DelegateParams) threadmgr.DelegateResult { ... }
type DelegateFn func(ctx context.Context, p DelegateParams) DelegateResult

// DelegateParams holds all the inputs for a delegation request.
type DelegateParams struct {
	AgentName   string
	Task        string
	Rationale   string   // why this agent was chosen (optional, surfaced to subagent)
	DependsOn   []string // agent name hints
	FileIntents []string // file paths the agent intends to modify
}

// DelegateToAgentTool creates and optionally spawns a sub-thread for a named agent.
// Only register this tool for primary agents where ModelInfo.SupportsDelegation == true.
//
// The actual thread management is handled by DelegateFn, which the caller provides.
// This design avoids the tools→threadmgr→agents→tools import cycle.
type DelegateToAgentTool struct {
	// Fn is the delegate implementation provided by the caller.
	// It must be non-nil for Execute to work.
	Fn DelegateFn
}

func (d *DelegateToAgentTool) Name() string { return "delegate_to_agent" }
func (d *DelegateToAgentTool) Description() string {
	return "Delegate a sub-task to a named agent. The agent runs concurrently and reports back via finish()."
}
func (d *DelegateToAgentTool) Permission() tools.PermissionLevel { return tools.PermExec }

func (d *DelegateToAgentTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "delegate_to_agent",
			Description: d.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"agent", "task"},
				Properties: map[string]backend.ToolProperty{
					"agent": {
						Type:        "string",
						Description: "Name of the agent to delegate to (must be a registered agent)",
					},
					"task": {
						Type:        "string",
						Description: "Clear description of the task for the agent",
					},
					"depends_on": {
						Type:        "array",
						Description: "Agent names or task hints that this thread depends on",
					},
					"file_intents": {
						Type:        "array",
						Description: "File paths or globs the agent intends to modify (for lease management)",
					},
					"rationale": {
						Type:        "string",
						Description: "Why you chose this specific agent for the task (surfaced to subagent as delegation context)",
					},
				},
			},
		},
	}
}

func (d *DelegateToAgentTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	if d.Fn == nil {
		return tools.ToolResult{IsError: true, Error: "delegate_to_agent: delegate function not configured"}
	}

	agentName, ok := args["agent"].(string)
	if !ok || agentName == "" {
		return tools.ToolResult{IsError: true, Error: "delegate_to_agent: 'agent' argument required"}
	}
	task, ok := args["task"].(string)
	if !ok || task == "" {
		return tools.ToolResult{IsError: true, Error: "delegate_to_agent: 'task' argument required"}
	}

	// Parse depends_on hints
	var hints []string
	if raw, ok := args["depends_on"]; ok {
		if arr, ok := raw.([]any); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok && s != "" {
					hints = append(hints, s)
				}
			}
		}
	}

	// Parse file_intents
	var fileIntents []string
	if raw, ok := args["file_intents"]; ok {
		if arr, ok := raw.([]any); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok && s != "" {
					fileIntents = append(fileIntents, s)
				}
			}
		}
	}

	rationale, _ := args["rationale"].(string)

	res := d.Fn(ctx, DelegateParams{
		AgentName:   agentName,
		Task:        task,
		Rationale:   rationale,
		DependsOn:   hints,
		FileIntents: fileIntents,
	})

	if res.Err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("delegate_to_agent: %v", res.Err)}
	}
	if len(res.Conflicts) > 0 {
		return tools.ToolResult{
			IsError: true,
			Error:   fmt.Sprintf("delegate_to_agent: file conflicts for agent %q: %v", agentName, res.Conflicts),
		}
	}

	if res.Spawned {
		return tools.ToolResult{
			Output:   fmt.Sprintf("delegated task to agent %q (thread %s) — spawned immediately", agentName, res.ThreadID),
			Metadata: map[string]any{"thread_id": res.ThreadID, "status": "spawned"},
		}
	}
	return tools.ToolResult{
		Output:   fmt.Sprintf("delegated task to agent %q (thread %s) — queued, waiting for dependencies", agentName, res.ThreadID),
		Metadata: map[string]any{"thread_id": res.ThreadID, "status": "queued"},
	}
}
