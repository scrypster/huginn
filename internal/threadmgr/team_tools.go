package threadmgr

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// stalledAfter is how long a non-terminal thread may go without heartbeat
// activity before team-status output flags it as possibly stalled.
const stalledAfter = 2 * time.Minute

// humanAgo renders a short relative duration like "3s ago" or "2m10s ago".
func humanAgo(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t).Round(time.Second)
	if d < 0 {
		d = 0
	}
	return d.String() + " ago"
}

// describeThreadLiveness renders one line of heartbeat detail for a thread:
// current activity, turn, last-activity recency, and a stall warning when the
// heartbeat has gone quiet on a non-terminal thread.
func describeThreadLiveness(th *Thread) string {
	var parts []string
	if th.CurrentActivity != "" {
		parts = append(parts, th.CurrentActivity)
	}
	if !th.LastActivityAt.IsZero() {
		parts = append(parts, "last activity "+humanAgo(th.LastActivityAt))
	}
	switch th.Status {
	case StatusDone, StatusCancelled, StatusError:
		// terminal — no stall warning
	default:
		ref := th.LastActivityAt
		if ref.IsZero() {
			ref = th.CreatedAt
		}
		if time.Since(ref) > stalledAfter {
			parts = append(parts, fmt.Sprintf("⚠ possibly stalled (no activity for %s)", time.Since(ref).Round(time.Second)))
		}
	}
	return strings.Join(parts, ", ")
}

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
		if liveness := describeThreadLiveness(th); liveness != "" {
			sb.WriteString(" — " + liveness)
		}
		if th.TokensUsed > 0 {
			sb.WriteString(fmt.Sprintf(", tokens=%d", th.TokensUsed))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\nTip: use wait_for_threads to block until running threads finish and collect their full results.")
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

// ─── wait_for_threads ────────────────────────────────────────────────────────

const (
	// defaultWaitTimeout is used when the model omits timeout_seconds.
	defaultWaitTimeout = 120 * time.Second
	// maxWaitTimeout caps the wait so it stays under the per-tool execution
	// timeout in the agent loop (5 minutes) — the agent can always call again.
	maxWaitTimeout = 240 * time.Second
)

// WaitForThreadsFn resolves the session from ctx, expands an empty ID list to
// all active threads, and blocks until they finish or the timeout expires.
type WaitForThreadsFn func(ctx context.Context, threadIDs []string, timeout time.Duration) (WaitReport, error)

// WaitForThreadsTool is the barrier primitive for delegation: the lead agent
// blocks until its delegated threads finish, then receives every full
// FinishSummary in one tool result — no recall_thread_result polling loop.
// On timeout it returns live heartbeat state for the still-running threads so
// the lead can tell "still working" from "stalled" and decide to wait again.
type WaitForThreadsTool struct {
	Fn WaitForThreadsFn
}

func (w *WaitForThreadsTool) Name() string { return "wait_for_threads" }
func (w *WaitForThreadsTool) Description() string {
	return "Wait for delegated threads to finish and collect their full results. Blocks up to timeout_seconds (default 120, max 240). With no thread_ids, waits on all active threads in the session. On timeout, reports each pending thread's live status so you can call this again or act on partial results."
}
func (w *WaitForThreadsTool) Permission() tools.PermissionLevel { return tools.PermRead }

func (w *WaitForThreadsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "wait_for_threads",
			Description: w.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"thread_ids": {
						Type:        "array",
						Description: "Thread IDs to wait for (from delegate_to_agent). Omit to wait on all active threads in the session.",
					},
					"timeout_seconds": {
						Type:        "number",
						Description: "Maximum seconds to wait (default 120, max 240).",
					},
				},
			},
		},
	}
}

func (w *WaitForThreadsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	if w.Fn == nil {
		return tools.ToolResult{IsError: true, Error: "wait_for_threads: not configured"}
	}

	var ids []string
	if raw, ok := args["thread_ids"]; ok {
		if arr, ok := raw.([]any); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok && s != "" {
					ids = append(ids, s)
				}
			}
		}
	}

	timeout := defaultWaitTimeout
	if raw, ok := args["timeout_seconds"]; ok {
		if secs, ok := raw.(float64); ok && secs > 0 {
			timeout = time.Duration(secs) * time.Second
		}
	}
	if timeout > maxWaitTimeout {
		timeout = maxWaitTimeout
	}

	report, err := w.Fn(ctx, ids, timeout)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("wait_for_threads: %v", err)}
	}
	if len(report.Completed) == 0 && len(report.Pending) == 0 {
		return tools.ToolResult{Output: "No matching threads to wait for — nothing is currently delegated in this session."}
	}

	var sb strings.Builder
	if len(report.Completed) > 0 {
		sb.WriteString(fmt.Sprintf("## Finished threads (%d)\n\n", len(report.Completed)))
		for _, th := range report.Completed {
			if th.Summary != nil {
				sb.WriteString(formatFinishSummary(th.AgentID, th.Summary))
			} else {
				// Cancelled/errored before producing a summary.
				sb.WriteString(fmt.Sprintf("## Result from agent %q\n\n**Status:** %s (no result produced)\n", th.AgentID, th.Status))
			}
			sb.WriteString(fmt.Sprintf("Thread ID: `%s`\n\n", th.ID))
		}
	}
	if len(report.Pending) > 0 {
		sb.WriteString(fmt.Sprintf("## Still running (%d) — timed out after %s\n\n", len(report.Pending), timeout))
		for _, th := range report.Pending {
			sb.WriteString(fmt.Sprintf("- **%s** (thread `%s`): status=%s", th.AgentID, th.ID, th.Status))
			if liveness := describeThreadLiveness(th); liveness != "" {
				sb.WriteString(" — " + liveness)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\nThreads with recent activity are still working — call wait_for_threads again to keep waiting. Threads flagged as stalled may need cancelling.")
	}

	pendingIDs := make([]string, 0, len(report.Pending))
	for _, th := range report.Pending {
		pendingIDs = append(pendingIDs, th.ID)
	}
	return tools.ToolResult{
		Output: strings.TrimSpace(sb.String()),
		Metadata: map[string]any{
			"completed":   len(report.Completed),
			"pending":     len(report.Pending),
			"pending_ids": pendingIDs,
			"timed_out":   report.TimedOut,
		},
	}
}
