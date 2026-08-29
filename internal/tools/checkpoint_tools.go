package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/checkpoint"
)

// RegisterCheckpointTools registers the checkpoint_* belt: run-level
// undo/diff/list/gc tools backed by a *checkpoint.Manager. Deliberately not
// called from RegisterBuiltins/init_tools.go — see init_checkpoint.go at
// the repo root for the one-line wiring an orchestrator setup path needs to
// call this.
func RegisterCheckpointTools(reg *Registry, mgr *checkpoint.Manager) {
	reg.Register(&CheckpointListTool{Manager: mgr})
	reg.Register(&CheckpointDiffRunTool{Manager: mgr})
	reg.Register(&CheckpointRevertRunTool{Manager: mgr})
	reg.Register(&CheckpointGCTool{Manager: mgr})
}

// --- checkpoint_list ---

// CheckpointListTool lists recent agent runs that can be undone.
type CheckpointListTool struct {
	Manager *checkpoint.Manager
}

func (t *CheckpointListTool) Name() string { return "checkpoint_list" }
func (t *CheckpointListTool) Description() string {
	return "List recent agent runs (checkpoints) that can be undone with checkpoint_revert_run. " +
		"Each run shows the agent, task, status, and whether it has been pushed."
}
func (t *CheckpointListTool) Permission() PermissionLevel { return PermRead }
func (t *CheckpointListTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "checkpoint_list",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"limit": {Type: "integer", Description: "Maximum number of runs to return (default 20)"},
				},
			},
		},
	}
}

func (t *CheckpointListTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	limit := 20
	if v, ok := args["limit"]; ok {
		if n, ok := checkpointArgInt(v); ok && n > 0 {
			limit = n
		}
	}
	runs, err := t.Manager.List(ctx, limit)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("checkpoint_list: %v", err)}
	}
	if len(runs) == 0 {
		return ToolResult{Output: "No runs recorded yet."}
	}
	var sb strings.Builder
	for _, r := range runs {
		fmt.Fprintf(&sb, "%s  agent=%s  status=%s  touched=%d file(s)", r.ThreadID, r.AgentID, r.Status, len(r.TouchedPaths))
		if r.Pushed {
			sb.WriteString("  pushed=true")
		}
		if r.TaskSummary != "" {
			fmt.Fprintf(&sb, "\n  task: %s", r.TaskSummary)
		}
		sb.WriteString("\n")
	}
	return ToolResult{Output: strings.TrimRight(sb.String(), "\n")}
}

// --- checkpoint_diff_run ---

// CheckpointDiffRunTool shows everything a run changed, as one unified diff.
type CheckpointDiffRunTool struct {
	Manager *checkpoint.Manager
}

func (t *CheckpointDiffRunTool) Name() string { return "checkpoint_diff_run" }
func (t *CheckpointDiffRunTool) Description() string {
	return "Show the full unified diff of everything a run (thread) changed, from its pre-run checkpoint to now. " +
		"Captures bash side effects too, not just write_file/edit_file calls."
}
func (t *CheckpointDiffRunTool) Permission() PermissionLevel { return PermRead }
func (t *CheckpointDiffRunTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "checkpoint_diff_run",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"thread_id"},
				Properties: map[string]backend.ToolProperty{
					"thread_id": {Type: "string", Description: "The run's thread ID (as shown by checkpoint_list)"},
				},
			},
		},
	}
}

func (t *CheckpointDiffRunTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	threadID, ok := args["thread_id"].(string)
	if !ok || strings.TrimSpace(threadID) == "" {
		return ToolResult{IsError: true, Error: "checkpoint_diff_run: 'thread_id' argument required"}
	}
	diff, err := t.Manager.DiffRun(ctx, threadID)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("checkpoint_diff_run: %v", err)}
	}
	if strings.TrimSpace(diff) == "" {
		return ToolResult{Output: fmt.Sprintf("Run %s made no file changes.", threadID)}
	}
	return ToolResult{Output: diff, Metadata: map[string]any{"thread_id": threadID}}
}

// --- checkpoint_revert_run ---

// CheckpointRevertRunTool undoes a run's file changes, honoring hand-edit
// preservation and the pushed-run guard.
type CheckpointRevertRunTool struct {
	Manager *checkpoint.Manager
}

func (t *CheckpointRevertRunTool) Name() string { return "checkpoint_revert_run" }
func (t *CheckpointRevertRunTool) Description() string {
	return "Undo a run's file changes back to its pre-run checkpoint. Files only — does not undo database rows, " +
		"network calls, or other side effects, and never rewrites a run that has already been pushed unless " +
		"allow_after_push is set. By default, files hand-edited since the run finished are preserved, not overwritten " +
		"(pass all=true to force a full restore)."
}
func (t *CheckpointRevertRunTool) Permission() PermissionLevel { return PermWrite }
func (t *CheckpointRevertRunTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "checkpoint_revert_run",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"thread_id"},
				Properties: map[string]backend.ToolProperty{
					"thread_id":        {Type: "string", Description: "The run's thread ID (as shown by checkpoint_list)"},
					"all":              {Type: "boolean", Description: "Force a full restore, overwriting hand-edits made since the run finished (default false)"},
					"only_paths":       {Type: "array", Description: "Restrict the restore to this subset of the run's touched paths (array of strings)"},
					"allow_after_push": {Type: "boolean", Description: "Permit reverting local files even though this run was already pushed (default false; the pushed commit itself is never rewritten)"},
				},
			},
		},
	}
}

func (t *CheckpointRevertRunTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	threadID, ok := args["thread_id"].(string)
	if !ok || strings.TrimSpace(threadID) == "" {
		return ToolResult{IsError: true, Error: "checkpoint_revert_run: 'thread_id' argument required"}
	}
	opts := checkpoint.RevertOptions{}
	if v, ok := args["all"].(bool); ok {
		opts.All = v
	}
	if v, ok := args["allow_after_push"].(bool); ok {
		opts.AllowAfterPush = v
	}
	if rawPaths, ok := args["only_paths"].([]any); ok {
		for _, p := range rawPaths {
			if s, ok := p.(string); ok && s != "" {
				opts.OnlyPaths = append(opts.OnlyPaths, s)
			}
		}
	}

	result, err := t.Manager.RevertRun(ctx, threadID, opts)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("checkpoint_revert_run: %v", err)}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Reverted run %s.\n", threadID)
	fmt.Fprintf(&sb, "Restored: %d file(s)", len(result.Restored))
	if len(result.Restored) > 0 {
		fmt.Fprintf(&sb, " (%s)", strings.Join(result.Restored, ", "))
	}
	sb.WriteString("\n")
	if len(result.Deleted) > 0 {
		fmt.Fprintf(&sb, "Removed (did not exist at checkpoint): %s\n", strings.Join(result.Deleted, ", "))
	}
	if len(result.SkippedEdited) > 0 {
		fmt.Fprintf(&sb, "Skipped (hand-edited since this run finished, preserved): %s\n", strings.Join(result.SkippedEdited, ", "))
	}
	if result.Warning != "" {
		fmt.Fprintf(&sb, "\n%s\n", result.Warning)
	}
	return ToolResult{
		Output: strings.TrimRight(sb.String(), "\n"),
		Metadata: map[string]any{
			"thread_id":      threadID,
			"restored":       result.Restored,
			"deleted":        result.Deleted,
			"skipped_edited": result.SkippedEdited,
			"not_restorable": result.NotRestorable,
		},
	}
}

// --- checkpoint_gc ---

// CheckpointGCTool prunes old checkpoint data to bound disk usage.
type CheckpointGCTool struct {
	Manager *checkpoint.Manager
}

func (t *CheckpointGCTool) Name() string { return "checkpoint_gc" }
func (t *CheckpointGCTool) Description() string {
	return "Garbage-collect old run checkpoints beyond the retention window, reclaiming shadow-store disk space."
}
func (t *CheckpointGCTool) Permission() PermissionLevel { return PermWrite }
func (t *CheckpointGCTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "checkpoint_gc",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"keep_runs":       {Type: "integer", Description: fmt.Sprintf("Number of most recent runs to retain (default %d)", checkpoint.DefaultKeepRuns)},
					"max_age_seconds": {Type: "integer", Description: "Also prune runs older than this many seconds (0 = no age cutoff)"},
				},
			},
		},
	}
}

func (t *CheckpointGCTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	opts := checkpoint.GCOptions{}
	if v, ok := args["keep_runs"]; ok {
		if n, ok := checkpointArgInt(v); ok {
			opts.KeepRuns = n
		}
	}
	if v, ok := args["max_age_seconds"]; ok {
		if n, ok := checkpointArgInt(v); ok && n > 0 {
			opts.MaxAge = time.Duration(n) * time.Second
		}
	}
	result, err := t.Manager.GC(ctx, opts)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("checkpoint_gc: %v", err)}
	}
	return ToolResult{
		Output: fmt.Sprintf("Pruned %d run(s), kept %d. Shadow store objects: %d -> %d.",
			result.PrunedRuns, result.KeptRuns, result.ObjectsBefore, result.ObjectsAfter),
		Metadata: map[string]any{
			"pruned_runs":    result.PrunedRuns,
			"kept_runs":      result.KeptRuns,
			"objects_before": result.ObjectsBefore,
			"objects_after":  result.ObjectsAfter,
		},
	}
}

// checkpointArgInt converts a JSON-decoded numeric arg (float64) or plain int to int.
func checkpointArgInt(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), true
	case int:
		return x, true
	case string:
		if n, err := strconv.Atoi(x); err == nil {
			return n, true
		}
	}
	return 0, false
}
