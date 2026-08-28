package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/scrypster/huginn/internal/agent/session"
	"github.com/scrypster/huginn/internal/backend"
)

// glabAvailable returns true if the `glab` CLI is in PATH.
func glabAvailable() bool {
	_, err := exec.LookPath("glab")
	return err == nil
}

// glBase is embedded in all glab CLI tool structs to share GlabPath and
// command helpers. Mirrors ghBase — same cmd.Dir discipline: without
// SandboxRoot, glab runs in the process's own cwd instead of the project the
// agent is operating on, silently targeting the wrong repo/MR.
type glBase struct {
	GlabPath    string // absolute path to glab binary, resolved before PATH shims
	SandboxRoot string
}

func (b *glBase) command(ctx context.Context, args ...string) *exec.Cmd {
	path := b.GlabPath
	if path == "" {
		path = "glab" // fallback if not set (no shims active)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	if b.SandboxRoot != "" {
		cmd.Dir = b.SandboxRoot
	}
	if sessionEnv := session.EnvFrom(ctx); len(sessionEnv) > 0 {
		cmd.Env = mergeEnv(os.Environ(), sessionEnv)
	}
	return cmd
}

// runGlabCmd runs an exec.Cmd built via glBase.command and returns (stdout, stderr, error).
func runGlabCmd(cmd *exec.Cmd) (string, string, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// NewGlabMRCreateTool constructs a GlabMRCreateTool with the given absolute
// glab binary path. Provided for tests that cannot use composite literals
// with unexported embedded types.
func NewGlabMRCreateTool(glabPath string) *GlabMRCreateTool {
	return &GlabMRCreateTool{glBase: glBase{GlabPath: glabPath}}
}

// --- glab_mr_create ---

type GlabMRCreateTool struct{ glBase }

func (t *GlabMRCreateTool) Name() string                { return "glab_mr_create" }
func (t *GlabMRCreateTool) Description() string         { return "Create a new merge request using the glab CLI." }
func (t *GlabMRCreateTool) Permission() PermissionLevel { return PermWrite }
func (t *GlabMRCreateTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "glab_mr_create",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"title"},
				Properties: map[string]backend.ToolProperty{
					"title":         {Type: "string", Description: "Merge request title"},
					"description":   {Type: "string", Description: "Merge request description (markdown)"},
					"source_branch": {Type: "string", Description: "Source branch (default: current branch)"},
					"target_branch": {Type: "string", Description: "Target branch to merge into (default: repo's default branch)"},
				},
			},
		},
	}
}
func (t *GlabMRCreateTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	title, _ := args["title"].(string)
	if strings.TrimSpace(title) == "" {
		return ToolResult{IsError: true, Error: "glab_mr_create: 'title' argument required"}
	}
	glArgs := []string{"mr", "create", "--title", title, "--yes"}
	if desc, _ := args["description"].(string); strings.TrimSpace(desc) != "" {
		glArgs = append(glArgs, "--description", desc)
	}
	if src, _ := args["source_branch"].(string); strings.TrimSpace(src) != "" {
		glArgs = append(glArgs, "--source-branch", src)
	}
	if tgt, _ := args["target_branch"].(string); strings.TrimSpace(tgt) != "" {
		glArgs = append(glArgs, "--target-branch", tgt)
	}
	cmd := t.command(ctx, glArgs...)
	stdout, stderr, err := runGlabCmd(cmd)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("glab mr create: %v\n%s", err, stderr)}
	}
	return ToolResult{Output: strings.TrimSpace(stdout)}
}

// --- glab_mr_checks ---
//
// IMPORTANT — status semantics are NOT the same as gh_pr_checks.
//
// gh_pr_checks relies on a documented gh CLI contract: `gh pr checks` exits
// with code 8 specifically when checks are still pending, letting a caller
// tell "still running" apart from "actually broke" from the exit code alone.
//
// glab has no equivalent CLI-level signal. `glab ci get` (the command this
// tool wraps — chosen over the more commonly cited `glab ci status` because
// `ci status`'s default behavior is a live/interactive pipeline monitor
// intended for a TTY, while `ci get` is a single non-interactive snapshot
// that also supports targeting a specific merge request via
// --merge-request, matching gh_pr_checks's per-PR targeting) exits 0
// whether the pipeline passed, failed, or is still running. There is no
// documented exit code that distinguishes those cases.
//
// So this tool's "status" classification is NOT a CLI contract — it is
// best-effort keyword matching over glab's own text output (looking for
// words like "failed", "success"/"passed", "running"/"pending"). Treat the
// classification as a hint, not a guarantee; the raw Output text is always
// returned alongside it so a caller can verify.
type GlabMRChecksTool struct{ glBase }

func (t *GlabMRChecksTool) Name() string { return "glab_mr_checks" }
func (t *GlabMRChecksTool) Description() string {
	return "Show CI pipeline status for a merge request using the glab CLI. Unlike gh_pr_checks, glab has no distinct exit code for a 'pending' pipeline — status is inferred from glab's text output on a best-effort basis, not guaranteed."
}
func (t *GlabMRChecksTool) Permission() PermissionLevel { return PermRead }
func (t *GlabMRChecksTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "glab_mr_checks",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"mr": {Type: "integer", Description: "Merge request IID (default: MR for the current branch)"},
				},
			},
		},
	}
}
func (t *GlabMRChecksTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	glArgs := []string{"ci", "get"}
	if num, ok := intArg(args, "mr"); ok {
		glArgs = append(glArgs, "--merge-request", fmt.Sprintf("%d", num))
	}
	cmd := t.command(ctx, glArgs...)
	stdout, stderr, err := runGlabCmd(cmd)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("glab ci get: %v\n%s", err, stderr)}
	}
	status := classifyGlabPipelineStatus(stdout)
	return ToolResult{
		Output:   strings.TrimSpace(stdout),
		Metadata: map[string]any{"status": status, "status_is_heuristic": true},
	}
}

// classifyGlabPipelineStatus performs a best-effort, case-insensitive
// keyword scan of glab's textual output to guess a coarse pipeline status.
// This is NOT a CLI-guaranteed contract (see GlabMRChecksTool doc comment) —
// it exists so callers get a quick hint without having to read prose, but
// the classification can be wrong if glab changes its wording.
func classifyGlabPipelineStatus(output string) string {
	low := strings.ToLower(output)
	switch {
	case strings.Contains(low, "failed") || strings.Contains(low, "failure"):
		return "failed"
	case strings.Contains(low, "success") || strings.Contains(low, "passed"):
		return "passed"
	case strings.Contains(low, "running") || strings.Contains(low, "pending") ||
		strings.Contains(low, "created") || strings.Contains(low, "waiting"):
		return "pending"
	default:
		return "unknown"
	}
}

// --- glab_ci_view_failed ---
//
// GitLab's CLI has no single-flag equivalent of `gh run view --log-failed`
// (which returns the failed step's log text directly). glab separates
// "which jobs failed" (glab ci get --status=failed, metadata only — no log
// text) from "what did this one job print" (glab ci trace <job-id>, which
// streams a single job's log by ID). This tool does both steps: it asks
// `glab ci get` for failed job IDs (JSON), then fetches each one's log via
// `glab ci trace` and concatenates them (capped), so the caller still gets
// "why did it fail" in one call — approximating gh_run_view_failed's intent
// using the closest real glab primitives, not a literal command mapping.
type GlabCIViewFailedTool struct{ glBase }

func (t *GlabCIViewFailedTool) Name() string { return "glab_ci_view_failed" }
func (t *GlabCIViewFailedTool) Description() string {
	return "Show logs for failed CI jobs in a GitLab pipeline using the glab CLI (glab ci get to find failed jobs, glab ci trace per job for logs)."
}
func (t *GlabCIViewFailedTool) Permission() PermissionLevel { return PermRead }
func (t *GlabCIViewFailedTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "glab_ci_view_failed",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"mr":     {Type: "integer", Description: "Merge request IID whose pipeline to inspect (default: MR for the current branch)"},
					"branch": {Type: "string", Description: "Branch whose pipeline to inspect (default: current branch; ignored if 'mr' is set)"},
				},
			},
		},
	}
}

// maxGlabFailedJobs caps how many failed jobs' logs are fetched per call —
// mirrors gh_run_view_failed never dumping unbounded log volume.
const maxGlabFailedJobs = 5

func (t *GlabCIViewFailedTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	getArgs := []string{"ci", "get", "--status", "failed", "--with-job-details", "--output", "json"}
	if num, ok := intArg(args, "mr"); ok {
		getArgs = append(getArgs, "--merge-request", fmt.Sprintf("%d", num))
	} else if branch, _ := args["branch"].(string); strings.TrimSpace(branch) != "" {
		getArgs = append(getArgs, "--branch", branch)
	}
	getCmd := t.command(ctx, getArgs...)
	stdout, stderr, err := runGlabCmd(getCmd)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("glab ci get: %v\n%s", err, stderr)}
	}

	jobIDs := findFailedJobIDs(stdout)
	if len(jobIDs) == 0 {
		// Couldn't identify individual failed job IDs from the JSON shape —
		// fall back to the raw ci get output rather than returning nothing.
		return ToolResult{Output: strings.TrimSpace(stdout)}
	}
	if len(jobIDs) > maxGlabFailedJobs {
		jobIDs = jobIDs[:maxGlabFailedJobs]
	}

	var sb strings.Builder
	for _, id := range jobIDs {
		traceCmd := t.command(ctx, "ci", "trace", fmt.Sprintf("%d", id))
		traceOut, traceErr, tErr := runGlabCmd(traceCmd)
		fmt.Fprintf(&sb, "=== job %d ===\n", id)
		if tErr != nil {
			fmt.Fprintf(&sb, "(failed to fetch trace: %v\n%s)\n\n", tErr, traceErr)
			continue
		}
		sb.WriteString(truncate(traceOut, maxOutputBytes/maxGlabFailedJobs))
		sb.WriteString("\n\n")
	}
	return ToolResult{Output: truncate(sb.String(), maxOutputBytes)}
}

// findFailedJobIDs recursively walks a glab `ci get --output json` payload
// looking for objects that carry both a numeric "id" and a "status" field
// equal (case-insensitively) to "failed". This is deliberately schema-
// tolerant rather than pinned to one exact JSON shape: glab's JSON output
// shape for this command is not pinned down in available documentation, and
// a lenient walk degrades to "found nothing" (safe fallback to raw text)
// rather than panicking or silently misparsing on a shape mismatch.
func findFailedJobIDs(rawJSON string) []int {
	var v any
	if err := json.Unmarshal([]byte(rawJSON), &v); err != nil {
		return nil
	}
	var ids []int
	var walk func(node any)
	walk = func(node any) {
		switch n := node.(type) {
		case map[string]any:
			status, hasStatus := n["status"].(string)
			idFloat, hasID := n["id"].(float64)
			if hasStatus && hasID && strings.EqualFold(status, "failed") {
				ids = append(ids, int(idFloat))
			}
			for _, val := range n {
				walk(val)
			}
		case []any:
			for _, item := range n {
				walk(item)
			}
		}
	}
	walk(v)
	return ids
}

// --- glab_mr_comment ---

type GlabMRCommentTool struct{ glBase }

func (t *GlabMRCommentTool) Name() string { return "glab_mr_comment" }
func (t *GlabMRCommentTool) Description() string {
	return "Post a comment on a merge request using the glab CLI."
}
func (t *GlabMRCommentTool) Permission() PermissionLevel { return PermWrite }
func (t *GlabMRCommentTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "glab_mr_comment",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"mr", "body"},
				Properties: map[string]backend.ToolProperty{
					"mr":   {Type: "integer", Description: "Merge request IID"},
					"body": {Type: "string", Description: "Comment body (markdown)"},
				},
			},
		},
	}
}
func (t *GlabMRCommentTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	num, ok := intArg(args, "mr")
	if !ok {
		return ToolResult{IsError: true, Error: "glab_mr_comment: 'mr' argument required"}
	}
	body, _ := args["body"].(string)
	if strings.TrimSpace(body) == "" {
		return ToolResult{IsError: true, Error: "glab_mr_comment: 'body' argument required"}
	}
	cmd := t.command(ctx, "mr", "note", fmt.Sprintf("%d", num), "--message", body)
	stdout, stderr, err := runGlabCmd(cmd)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("glab mr note: %v\n%s", err, stderr)}
	}
	return ToolResult{Output: strings.TrimSpace(stdout)}
}
