// Package oneshot runs a single agentic turn from the CLI without the TUI
// or web UI. --print and --agent NAME MSG both go through Run.
package oneshot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/memory"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/tools"
)

// ToolCall is one executed tool invocation captured during the loop.
type ToolCall struct {
	Name   string         `json:"name"`
	Args   map[string]any `json:"args,omitempty"`
	Result string         `json:"result,omitempty"`
}

// Result is the structured output of a one-shot run.
type Result struct {
	AgentOutput string     `json:"agentOutput"`
	ToolsCalled []ToolCall `json:"toolsCalled"`
}

// Config configures a one-shot agentic run. Backend and a way to resolve
// agents (Registry or LoadRegistry) are required.
type Config struct {
	Prompt          string
	AgentName       string
	Model           string
	NoTools         bool
	SkipPermissions bool
	MaxTurns        int
	CWD             string
	BashTimeout     time.Duration

	Backend  backend.Backend
	Models   *modelconfig.Models
	Registry *agents.AgentRegistry
	Tools    *tools.Registry

	// LoadRegistry is used when Registry is nil. Tests inject a stub; the
	// CLI uses LoadDefaultRegistry when both are nil.
	LoadRegistry func() (*agents.AgentRegistry, error)

	// OnToken receives streamed assistant tokens. Ignored when nil.
	OnToken func(string)
}

// LoadDefaultRegistry reads ~/.huginn/agents and builds a live registry.
func LoadDefaultRegistry(models *modelconfig.Models) (*agents.AgentRegistry, error) {
	if models == nil {
		models = modelconfig.DefaultModels()
	}
	cfg, err := agents.LoadAgents()
	if err != nil {
		cfg = agents.DefaultAgentsConfig()
	}
	return agents.BuildRegistryWithUsername(cfg, models, memory.ResolveUsername("")), nil
}

// DefaultToolRegistry registers the same local builtins the web named-agent
// path sees (minus LSP/MCP, which are process-lifetime services).
func DefaultToolRegistry(cwd string, bashTimeout time.Duration) *tools.Registry {
	if bashTimeout <= 0 {
		bashTimeout = 120 * time.Second
	}
	reg := tools.NewRegistry()
	tools.RegisterBuiltins(reg, cwd, bashTimeout)
	tools.RegisterGitTools(reg, cwd)
	tools.RegisterTestsTool(reg, cwd, bashTimeout)
	tools.RegisterGitHubTools(reg)
	reg.TagTools(tools.GitHubCLIToolNames(), "github_cli")
	reg.TagTools(tools.BuiltinToolNames(), "builtin")
	return reg
}

// Run resolves the named agent, applies --model via SwapModel, builds the
// same toolbelt as ChatWithAgent (applyToolbelt + local_tools, including
// ["*"]), and executes RunLoop through ChatWithAgent.
func Run(ctx context.Context, cfg Config) (*Result, error) {
	if strings.TrimSpace(cfg.Prompt) == "" {
		return nil, fmt.Errorf("oneshot: prompt is required")
	}
	if cfg.Backend == nil {
		return nil, fmt.Errorf("oneshot: backend is required")
	}

	reg, err := resolveRegistry(cfg)
	if err != nil {
		return nil, err
	}

	ag, err := resolveAgent(reg, cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Model != "" {
		ag.SwapModel(cfg.Model)
	}

	models := cfg.Models
	if models == nil {
		models = modelconfig.DefaultModels()
	}
	orch, err := agent.NewOrchestrator(cfg.Backend, models, nil, nil, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("oneshot: orchestrator: %w", err)
	}
	orch.SetAgentRegistry(reg)
	if cfg.MaxTurns > 0 {
		orch.SetMaxTurns(cfg.MaxTurns)
	}

	var gate *permissions.Gate
	if !cfg.NoTools {
		toolReg := cfg.Tools
		if toolReg == nil {
			toolReg = DefaultToolRegistry(cfg.CWD, cfg.BashTimeout)
		}
		// One-shot has no TUI prompt. skipAll auto-allows; otherwise deny so
		// PermExec/PermWrite cannot hang waiting for a human.
		gate = permissions.NewGate(cfg.SkipPermissions, func(permissions.PermissionRequest) permissions.Decision {
			return permissions.Deny
		})
		defer gate.Close()
		orch.SetTools(toolReg, gate)
	}

	result := &Result{ToolsCalled: []ToolCall{}}
	var pending []ToolCall
	var buf strings.Builder

	onToken := func(tok string) {
		buf.WriteString(tok)
		if cfg.OnToken != nil {
			cfg.OnToken(tok)
		}
	}
	onToolEvent := func(eventType string, payload map[string]any) {
		name, _ := payload["tool"].(string)
		switch eventType {
		case "tool_call":
			args, _ := payload["args"].(map[string]any)
			pending = append(pending, ToolCall{Name: name, Args: args})
		case "tool_result":
			out, _ := payload["result"].(string)
			for i := range pending {
				if pending[i].Name == name && pending[i].Result == "" {
					pending[i].Result = out
					break
				}
			}
		}
	}

	if err := orch.ChatWithAgent(ctx, ag, cfg.Prompt, "", onToken, onToolEvent, nil); err != nil {
		return nil, err
	}

	result.AgentOutput = buf.String()
	result.ToolsCalled = pending
	if result.ToolsCalled == nil {
		result.ToolsCalled = []ToolCall{}
	}
	return result, nil
}

// WriteResult prints a one-shot result. JSON mode emits a single object.
// Text mode writes the answer (unless already streamed) and a short tool
// summary to errW.
func WriteResult(w, errW io.Writer, result *Result, jsonOut, streamed bool) error {
	if result == nil {
		return fmt.Errorf("oneshot: nil result")
	}
	if jsonOut {
		if result.ToolsCalled == nil {
			result.ToolsCalled = []ToolCall{}
		}
		enc := json.NewEncoder(w)
		return enc.Encode(result)
	}
	if !streamed {
		if _, err := fmt.Fprintln(w, result.AgentOutput); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if summary := FormatToolSummary(result.ToolsCalled); summary != "" && errW != nil {
		_, err := fmt.Fprint(errW, summary)
		return err
	}
	return nil
}

// FormatToolSummary is a short human-readable list of tool calls.
func FormatToolSummary(calls []ToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Tools used:\n")
	for _, c := range calls {
		args := ""
		if len(c.Args) > 0 {
			raw, err := json.Marshal(c.Args)
			if err == nil {
				args = string(raw)
			}
		}
		result := strings.ReplaceAll(c.Result, "\n", "\\n")
		if len(result) > 200 {
			result = result[:200] + "…"
		}
		fmt.Fprintf(&b, "  %s %s → %s\n", c.Name, args, result)
	}
	return b.String()
}

func resolveRegistry(cfg Config) (*agents.AgentRegistry, error) {
	if cfg.Registry != nil {
		return cfg.Registry, nil
	}
	if cfg.LoadRegistry != nil {
		return cfg.LoadRegistry()
	}
	return LoadDefaultRegistry(cfg.Models)
}

func resolveAgent(reg *agents.AgentRegistry, cfg Config) (*agents.Agent, error) {
	if name := strings.TrimSpace(cfg.AgentName); name != "" {
		ag, ok := reg.ByName(name)
		if !ok {
			return nil, fmt.Errorf("unknown agent %q; available: %s", name, availableAgentNames(reg))
		}
		return ag, nil
	}
	if ag := reg.DefaultAgent(); ag != nil {
		return ag, nil
	}
	// --print with no named agents: same local_tools=["*"] as a god-mode assistant
	// so the one-shot path can still run the tool loop.
	modelID := cfg.Model
	if modelID == "" && cfg.Models != nil {
		modelID = cfg.Models.Reasoner
	}
	if modelID == "" {
		modelID = modelconfig.DefaultModels().Reasoner
	}
	return &agents.Agent{
		Name:         "Huginn",
		SystemPrompt: "You are Huginn, a helpful AI coding assistant. Use markdown formatting when it improves readability.",
		ModelID:      modelID,
		LocalTools:   []string{"*"},
		IsDefault:    true,
	}, nil
}

func availableAgentNames(reg *agents.AgentRegistry) string {
	if reg == nil {
		return "(none)"
	}
	all := reg.All()
	if len(all) == 0 {
		return "(none)"
	}
	names := make([]string, 0, len(all))
	for _, a := range all {
		names = append(names, a.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
