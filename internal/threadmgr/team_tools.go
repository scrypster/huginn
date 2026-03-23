package threadmgr

import (
	"context"
	"fmt"
	"strings"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// ListTeamStatusFn is the function signature wired by main.go.
// It receives the current session ID (from context) and returns all threads.
type ListTeamStatusFn func(ctx context.Context) ([]*Thread, error)

// ListTeamStatusTool lets a lead agent query the real-time status of all
// sub-threads running in the current session.
type ListTeamStatusTool struct {
	Fn ListTeamStatusFn
}

func (t *ListTeamStatusTool) Name() string        { return "list_team_status" }
func (t *ListTeamStatusTool) Description() string {
	return "List the real-time status of all delegated threads in the current session, including whether they are queued, running, blocked, or completed."
}
func (t *ListTeamStatusTool) Permission() tools.PermissionLevel { return tools.PermRead }

func (t *ListTeamStatusTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "list_team_status",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:       "object",
				Properties: map[string]backend.ToolProperty{},
			},
		},
	}
}

func (t *ListTeamStatusTool) Execute(ctx context.Context, _ map[string]any) tools.ToolResult {
	if t.Fn == nil {
		return tools.ToolResult{IsError: true, Error: "list_team_status: not configured"}
	}
	threads, err := t.Fn(ctx)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("list_team_status: %v", err)}
	}
	if len(threads) == 0 {
		return tools.ToolResult{Output: "No delegated threads found in this session."}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Team Status (%d threads)\n\n", len(threads)))
	for _, th := range threads {
		sb.WriteString(fmt.Sprintf("- **%s** (thread `%s`): status=%s", th.AgentID, th.ID, th.Status))
		if th.Task != "" {
			task := th.Task
			if len(task) > 120 {
				task = task[:120] + "…"
			}
			sb.WriteString(fmt.Sprintf(", task=%q", task))
		}
		if th.TokensUsed > 0 {
			sb.WriteString(fmt.Sprintf(", tokens=%d", th.TokensUsed))
		}
		sb.WriteString("\n")
	}
	return tools.ToolResult{Output: sb.String()}
}

// ─── recall_thread_result ────────────────────────────────────────────────────

// RecallThreadResultFn retrieves the finish summary for a completed thread.
// Returns the thread and nil error if found; returns an error if not found or
// the thread belongs to a different session.
type RecallThreadResultFn func(ctx context.Context, threadID string) (*Thread, error)

// RecallThreadResultTool lets a lead agent read the FinishSummary produced by
// a completed sub-thread (the structured output the sub-agent wrote via finish()).
type RecallThreadResultTool struct {
	Fn RecallThreadResultFn
}

func (r *RecallThreadResultTool) Name() string { return "recall_thread_result" }
func (r *RecallThreadResultTool) Description() string {
	return "Retrieve the structured result (summary, files modified, key decisions, artifacts) from a completed delegated thread."
}
func (r *RecallThreadResultTool) Permission() tools.PermissionLevel { return tools.PermRead }

func (r *RecallThreadResultTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "recall_thread_result",
			Description: r.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"thread_id"},
				Properties: map[string]backend.ToolProperty{
					"thread_id": {
						Type:        "string",
						Description: "The thread ID returned by delegate_to_agent",
					},
				},
			},
		},
	}
}

func (r *RecallThreadResultTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	if r.Fn == nil {
		return tools.ToolResult{IsError: true, Error: "recall_thread_result: not configured"}
	}
	threadID, ok := args["thread_id"].(string)
	if !ok || threadID == "" {
		return tools.ToolResult{IsError: true, Error: "recall_thread_result: thread_id is required"}
	}

	thread, err := r.Fn(ctx, threadID)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("recall_thread_result: %v", err)}
	}
	if thread == nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("recall_thread_result: thread %q not found", threadID)}
	}

	if thread.Summary == nil {
		return tools.ToolResult{
			Output: fmt.Sprintf("Thread %q (agent=%s, status=%s) has no result yet.", threadID, thread.AgentID, thread.Status),
		}
	}

	return tools.ToolResult{
		Output:   formatFinishSummary(thread.AgentID, thread.Summary),
		Metadata: map[string]any{"thread_id": threadID, "agent": thread.AgentID, "status": string(thread.Status)},
	}
}
