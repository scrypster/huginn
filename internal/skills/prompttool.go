package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// AgentExecutor is implemented by the Orchestrator and injected into agent-mode PromptTools.
// Using an interface here avoids a circular import between skills and agent packages.
type AgentExecutor interface {
	ExecuteAgentTool(ctx context.Context, model string, budgetTokens int, prompt string) (string, error)
}

const maxShellOutputBytes = 65536 // 64 KB

// defaultAgentTimeout is the maximum time an agent-mode tool execution may run.
// Without this guard a skill that loops forever would hang the caller indefinitely.
const defaultAgentTimeout = 5 * time.Minute

// PromptTool is a tool loaded from a skill's tools/*.md file.
// It supports three execution modes:
//   - "template" (default): text/template substitution in the body with a restricted FuncMap
//   - "shell": executes shellBin with shellArgs (or parses body as template when shellBin is empty),
//     caps output at 64 KB; never uses sh -c with string concatenation
//   - "agent": stub with depth-limit guard — real LLM call requires orchestrator reference
type PromptTool struct {
	name        string
	description string
	schemaJSON  string
	body        string

	// Mode: "template" | "shell" | "agent"
	mode string

	// Shell mode fields
	shellBin         string
	shellArgs        []string
	shellTimeoutSecs int
	maxOutputBytes   int

	// Agent mode fields
	agentModel       string
	budgetTokens     int
	depth            int // current recursion depth (injected by caller for nested agent tools)
	maxDepth         int // maximum allowed recursion depth (0 = use default of 5)
	agentExecutor    atomic.Pointer[AgentExecutor]
}

// NewPromptTool creates a PromptTool in template mode (the default).
func NewPromptTool(name, description, schemaJSON, body string) *PromptTool {
	return &PromptTool{
		name:           name,
		description:    description,
		schemaJSON:     schemaJSON,
		body:           body,
		mode:           "template",
		maxOutputBytes: maxShellOutputBytes,
	}
}

// SetAgentExecutor injects an orchestrator reference for agent-mode execution.
// This avoids a circular import between skills and agent packages.
func (p *PromptTool) SetAgentExecutor(e AgentExecutor) {
	p.agentExecutor.Store(&e)
}

func (p *PromptTool) Name() string { return p.name }
func (p *PromptTool) Description() string { return p.description }
func (p *PromptTool) Permission() tools.PermissionLevel {
	switch p.mode {
	case "shell":
		return tools.PermExec  // executes binaries
	case "agent":
		return tools.PermWrite // makes outbound LLM calls consuming user's budget
	default:
		return tools.PermRead  // template mode: pure string rendering, no side effects
	}
}

func (p *PromptTool) Schema() backend.Tool {
	var params backend.ToolParameters
	if p.schemaJSON != "" && p.schemaJSON != "{}" {
		if err := json.Unmarshal([]byte(p.schemaJSON), &params); err != nil {
			slog.Warn("skills: tool schema JSON is malformed, schema validation disabled",
				"tool", p.name, "err", err)
		}
	}
	if params.Type == "" {
		params.Type = "object"
	}
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        p.name,
			Description: p.description,
			Parameters:  params,
		},
	}
}

func (p *PromptTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	switch p.mode {
	case "shell":
		return p.executeShell(ctx, args)
	case "agent":
		return p.executeAgent(ctx, args)
	default:
		// "template" mode
		return p.executeTemplate(args)
	}
}

// templateKeywords is the set of built-in text/template action keywords that must
// NOT be converted to dot-prefixed field access during normalization.
var templateKeywords = map[string]bool{
	"if": true, "else": true, "end": true, "range": true,
	"with": true, "define": true, "block": true, "template": true,
	"nil": true,
}

// bareIdentRe matches {{identifier}} where identifier is a simple name without dots or spaces.
// These are normalized to {{.identifier}} for text/template compatibility.
var bareIdentRe = regexp.MustCompile(`\{\{([a-zA-Z_][a-zA-Z0-9_]*)\}\}`)

// normalizeTemplateSyntax converts legacy {{key}} placeholders (no dot prefix) to
// the text/template canonical form {{.key}}, leaving directives like {{.key}},
// {{upper .name}}, {{if ...}}, {{end}}, etc. unchanged.
func normalizeTemplateSyntax(body string) string {
	return bareIdentRe.ReplaceAllStringFunc(body, func(match string) string {
		// Extract the identifier (group 1).
		inner := match[2 : len(match)-2] // strip {{ and }}
		if templateKeywords[inner] {
			return match // leave keywords untouched
		}
		return "{{." + inner + "}}"
	})
}

// executeTemplate renders the body using text/template with a restricted FuncMap.
// Only safe, pure string functions are exposed — no exec, no file I/O, no network.
// Missing keys produce an empty string (missingkey=zero).
func (p *PromptTool) executeTemplate(args map[string]any) tools.ToolResult {
	funcMap := template.FuncMap{
		"upper":   strings.ToUpper,
		"lower":   strings.ToLower,
		"trim":    strings.TrimSpace,
		"join":    strings.Join,
		"replace": strings.ReplaceAll,
	}

	normalizedBody := normalizeTemplateSyntax(p.body)

	tmpl, err := template.New(p.name).Funcs(funcMap).Option("missingkey=zero").Parse(normalizedBody)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("parse template: %v", err)}
	}

	// Convert args to string map for safe, predictable rendering.
	strArgs := make(map[string]string, len(args))
	for k, v := range args {
		strArgs[k] = fmt.Sprintf("%v", v)
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, strArgs); err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("execute template: %v", err)}
	}
	return tools.ToolResult{Output: buf.String()}
}

// executeShell runs a shell command and captures its output.
//
// If shellBin is set (populated from frontmatter shell: field), it is used directly
// as the command binary with shellArgs as arguments.
//
// If shellBin is empty, the body is rendered as a text/template to produce the full
// command string, which is then split on whitespace into argv — NEVER passed to sh -c.
//
// Output is captured separately for stdout and stderr, each with independent byte caps.
// The context deadline (or shellTimeoutSecs if set) constrains execution time.
func (p *PromptTool) executeShell(ctx context.Context, args map[string]any) tools.ToolResult {
	// Apply a per-tool timeout if configured, unless the context already has a tighter deadline.
	if p.shellTimeoutSecs > 0 {
		d := time.Duration(p.shellTimeoutSecs) * time.Second
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}

	capBytes := p.maxOutputBytes
	if capBytes <= 0 {
		capBytes = maxShellOutputBytes
	}

	var cmd *exec.Cmd

	if p.shellBin != "" {
		// Static binary + args from frontmatter (original Phase 1 behavior).
		cmd = exec.CommandContext(ctx, p.shellBin, p.shellArgs...) //nolint:gosec // binary is from trusted frontmatter, not user input
	} else {
		// Dynamic: render body as template to produce argv, then exec directly (no sh -c).
		parts, err := p.renderShellArgs(args)
		if err != nil {
			return tools.ToolResult{IsError: true, Error: err.Error()}
		}
		if len(parts) == 0 {
			return tools.ToolResult{IsError: true, Error: "shell mode: empty command after template rendering"}
		}
		cmd = exec.CommandContext(ctx, parts[0], parts[1:]...) //nolint:gosec // command parts come from trusted skill frontmatter body
	}

	// Use separate limited writers for stdout and stderr.
	var stdout limitedWriter
	stdout.max = capBytes
	var stderr limitedWriter
	stderr.max = capBytes / 8 // 8 KB stderr cap
	if stderr.max < 1024 {
		stderr.max = 1024
	}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return tools.ToolResult{IsError: true, Error: fmt.Sprintf("shell command timed out: %v", ctx.Err())}
		}
		errMsg := fmt.Sprintf("shell command failed: %v", err)
		if stderr.buf.Len() > 0 {
			errMsg += "\nstderr: " + stderr.buf.String()
		}
		return tools.ToolResult{IsError: true, Error: errMsg}
	}

	out := stdout.buf.String()
	if stdout.truncated {
		out += "\n[output truncated at 64KB]"
	}
	return tools.ToolResult{Output: out}
}

// truncate returns s truncated to at most n bytes (rune-boundary unaware for simplicity).
// Retained for any callers that may rely on it, though shell mode now uses limitedWriter.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// limitedWriter caps writes at max bytes and sets truncated=true when the cap is hit.
type limitedWriter struct {
	buf       strings.Builder
	max       int
	truncated bool
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	remaining := w.max - w.buf.Len()
	if remaining <= 0 {
		w.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = w.buf.Write(p[:remaining])
		w.truncated = true
		return len(p), nil
	}
	return w.buf.Write(p)
}

// renderShellArgs renders the tool body as a text/template and splits the result on
// whitespace to produce an argv slice. The result is NEVER passed to sh -c.
func (p *PromptTool) renderShellArgs(args map[string]any) ([]string, error) {
	funcMap := template.FuncMap{
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
		"trim":  strings.TrimSpace,
	}
	strArgs := make(map[string]string, len(args))
	for k, v := range args {
		strArgs[k] = fmt.Sprintf("%v", v)
	}

	normalizedBody := normalizeTemplateSyntax(p.body)
	tmpl, err := template.New("shell").Funcs(funcMap).Option("missingkey=zero").Parse(normalizedBody)
	if err != nil {
		return nil, fmt.Errorf("parse shell template: %w", err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, strArgs); err != nil {
		return nil, fmt.Errorf("render shell template: %w", err)
	}
	return strings.Fields(buf.String()), nil
}

// executeAgent is the agent mode executor. It enforces a maximum recursion depth.
// If an AgentExecutor is wired, it delegates the call; otherwise, returns a stub message.
func (p *PromptTool) executeAgent(ctx context.Context, args map[string]any) tools.ToolResult {
	maxDepth := p.maxDepth
	if maxDepth <= 0 {
		maxDepth = 5 // sensible default
	}
	if p.depth >= maxDepth {
		return tools.ToolResult{
			IsError: true,
			Error:   fmt.Sprintf("agent mode: maximum recursion depth %d reached", maxDepth),
		}
	}

	model := p.agentModel
	if model == "" {
		model = "default"
	}
	budget := p.budgetTokens
	if budget <= 0 {
		budget = 5000 // sensible default
	}

	// If no executor is wired, return a stub message.
	exec := p.agentExecutor.Load()
	if exec == nil {
		return tools.ToolResult{
			Output: fmt.Sprintf(
				"[agent mode stub] tool=%q model=%q budget_tokens=%d args=%v\n"+
					"Agent mode requires an orchestrator backend to make LLM calls. "+
					"Wire an orchestrator reference to enable real agent execution.",
				p.name, model, budget, args,
			),
		}
	}

	// Build the prompt from args and body.
	// The body is a template that uses the args to construct the LLM prompt.
	prompt, err := p.renderAgentPrompt(args)
	if err != nil {
		return tools.ToolResult{
			IsError: true,
			Error:   fmt.Sprintf("agent mode: failed to render prompt: %v", err),
		}
	}

	// Apply a per-execution timeout so a skill that loops forever cannot hang the caller.
	execCtx, execCancel := context.WithTimeout(ctx, defaultAgentTimeout)
	defer execCancel()

	// Delegate to the orchestrator.
	executor := *exec // dereference the pointer from Load()
	result, err := executor.ExecuteAgentTool(execCtx, model, budget, prompt)
	if err != nil {
		return tools.ToolResult{
			IsError: true,
			Error:   fmt.Sprintf("agent mode: execution failed: %v", err),
		}
	}
	return tools.ToolResult{Output: result}
}

// renderAgentPrompt renders the tool body as a text/template to produce the LLM prompt.
func (p *PromptTool) renderAgentPrompt(args map[string]any) (string, error) {
	funcMap := template.FuncMap{
		"upper":   strings.ToUpper,
		"lower":   strings.ToLower,
		"trim":    strings.TrimSpace,
		"join":    strings.Join,
		"replace": strings.ReplaceAll,
	}
	strArgs := make(map[string]string, len(args))
	for k, v := range args {
		strArgs[k] = fmt.Sprintf("%v", v)
	}

	normalizedBody := normalizeTemplateSyntax(p.body)
	tmpl, err := template.New("agent").Funcs(funcMap).Option("missingkey=zero").Parse(normalizedBody)
	if err != nil {
		return "", fmt.Errorf("parse agent prompt template: %w", err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, strArgs); err != nil {
		return "", fmt.Errorf("render agent prompt template: %w", err)
	}
	return buf.String(), nil
}

// validateArgs checks required fields and per-field pattern constraints defined in the
// schema (parsed from schemaJSON). It accepts any args when schemaJSON is empty or "{}".
func (p *PromptTool) validateArgs(args map[string]any) error {
	if p.schemaJSON == "" || p.schemaJSON == "{}" {
		return nil
	}

	var schema map[string]any
	if err := json.Unmarshal([]byte(p.schemaJSON), &schema); err != nil {
		return nil // malformed schema — don't block execution
	}

	// Check required fields.
	if required, ok := schema["required"].([]any); ok {
		for _, r := range required {
			key, ok := r.(string)
			if !ok {
				continue
			}
			if _, exists := args[key]; !exists {
				return fmt.Errorf("missing required arg: %q", key)
			}
		}
	}

	// Check per-field pattern constraints.
	props, _ := schema["properties"].(map[string]any)
	for key, val := range args {
		prop, ok := props[key].(map[string]any)
		if !ok {
			continue // unknown property — allow it
		}
		pattern, ok := prop["pattern"].(string)
		if !ok {
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			continue // malformed pattern — skip validation
		}
		strVal := fmt.Sprintf("%v", val)
		if !re.MatchString(strVal) {
			return fmt.Errorf("arg %q does not match pattern %q", key, pattern)
		}
	}
	return nil
}
