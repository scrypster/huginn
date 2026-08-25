package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/claudecode"
)

// allowedPermissionModes is the set of permission_mode values a model-supplied
// tool argument may select.
//
// bypassPermissions is deliberately absent: it grants the same unrestricted
// access as --dangerously-skip-permissions, which is gated behind explicit
// operator config. A model-supplied tool argument must not be able to reach
// it. An operator can still set it via claude_code.delegate.permission_mode.
var allowedPermissionModes = map[string]bool{
	"acceptEdits": true, "auto": true, "manual": true, "dontAsk": true, "plan": true,
}

// ClaudeCodeTool delegates a one-shot task to the Claude Code CLI.
//
// It is PermExec: the delegated session can run arbitrary tools in the target
// directory, so it goes through the same approval path as bash.
type ClaudeCodeTool struct {
	cfg        claudecode.Config
	defaultCWD string
	onEvent    func(claudecode.StreamEvent)
}

// NewClaudeCodeTool returns the tool. onEvent may be nil; when set it receives
// live stream events so the caller can mirror them into a thread panel.
func NewClaudeCodeTool(cfg claudecode.Config, defaultCWD string, onEvent func(claudecode.StreamEvent)) *ClaudeCodeTool {
	return &ClaudeCodeTool{cfg: cfg, defaultCWD: defaultCWD, onEvent: onEvent}
}

func (t *ClaudeCodeTool) Name() string { return "claude_code" }

func (t *ClaudeCodeTool) Description() string {
	return "Delegate a self-contained coding task to a Claude Code session and return its result."
}

func (t *ClaudeCodeTool) Permission() PermissionLevel { return PermExec }

func (t *ClaudeCodeTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "claude_code",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"prompt"},
				Properties: map[string]backend.ToolProperty{
					"prompt":          {Type: "string", Description: "The task for Claude Code. Must be self-contained: the session has no access to this conversation."},
					"cwd":             {Type: "string", Description: "Working directory for the session. Defaults to the current workspace root."},
					"model":           {Type: "string", Description: "Model alias, e.g. opus or sonnet. Defaults to the configured model."},
					"max_turns":       {Type: "integer", Description: "Maximum agentic turns before the session stops."},
					"permission_mode": {Type: "string", Description: "One of acceptEdits, auto, manual, dontAsk, plan. Defaults to acceptEdits."},
				},
			},
		},
	}
}

func (t *ClaudeCodeTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		return ToolResult{
			IsError: true,
			Error:   "claude_code: prompt is required and must be a non-empty string",
		}
	}

	permMode := stringArg(args, "permission_mode", "")
	if permMode != "" && !allowedPermissionModes[permMode] {
		allowed := make([]string, 0, len(allowedPermissionModes))
		for m := range allowedPermissionModes {
			allowed = append(allowed, m)
		}
		sort.Strings(allowed)
		return ToolResult{
			IsError: true,
			Error: fmt.Sprintf("claude_code: permission_mode %q is not allowed; allowed values are: %s",
				permMode, strings.Join(allowed, ", ")),
		}
	}

	req := claudecode.DelegateRequest{
		Prompt:         prompt,
		CWD:            stringArg(args, "cwd", t.defaultCWD),
		Model:          stringArg(args, "model", ""),
		PermissionMode: permMode,
		MaxTurns:       intArgOr(args, "max_turns", 0),
	}

	sessionID := uuid.NewString()
	res, err := claudecode.Delegate(ctx, t.cfg.Delegate, t.cfg.Binary, req, sessionID, t.onEvent)

	out := ToolResult{
		Output: res.Text,
		Metadata: map[string]any{
			"claude_session_id": res.SessionID,
			"cost_usd":          res.CostUSD,
			"duration_ms":       res.DurationMS,
			"num_turns":         res.NumTurns,
		},
	}
	if res.IsError {
		out.IsError = true
		out.Error = res.ErrorText
	}
	if err != nil {
		out.IsError = true
		out.Error = fmt.Sprintf("claude_code: %v", err)
		if res.ErrorText != "" {
			out.Error = fmt.Sprintf("claude_code: %s (%v)", res.ErrorText, err)
		}
	}
	return out
}

// RegisterClaudeCodeTool registers the tool only when delegation is enabled.
func RegisterClaudeCodeTool(reg *Registry, cfg claudecode.Config, cwd string, onEvent func(claudecode.StreamEvent)) {
	if !cfg.DelegateEnabled() {
		return
	}
	reg.Register(NewClaudeCodeTool(cfg, cwd, onEvent))
}

func stringArg(args map[string]any, key, fallback string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

// intArgOr is named to avoid colliding with the package's existing
// intArg(args, key) (int, bool) in github.go.
func intArgOr(args map[string]any, key string, fallback int) int {
	switch v := args[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64: // JSON numbers arrive as float64
		return int(v)
	}
	return fallback
}
