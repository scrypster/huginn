package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/scrypster/huginn/internal/backend"
)

// DefaultGatedTools are gated when an agent has no explicit ClaudeGatedTools.
// Every Claude Code tool that can mutate state or reach the network is here.
// An unconfigured agent runs unattended, so the default must be restrictive:
// an empty gated list emits no hooks and gates nothing at all.
//
// These are Claude Code's tool names, not Huginn's LocalTools names — never
// mix the two namespaces.
var DefaultGatedTools = []string{
	"Bash", "Write", "Edit", "NotebookEdit", "WebFetch", "Task",
}

// AgentBackendConfig is everything a Claude Code agent needs for one turn.
type AgentBackendConfig struct {
	Binary       string
	SessionID    string
	CWD          string
	Model        string
	SystemPrompt string   // assembled by AssembleSystemPrompt, rebuilt every turn
	AllowedTools []string // pre-authorised; never invoke the approval hook
	GatedTools   []string // require approval; one PreToolUse hook entry each
	HookCommand  string
	MCPConfig    string // empty in v1 — the seam for the toolbelt MCP server
	FirstTurn    bool   // true until the session exists
}

// AgentBackend drives one Claude Code session as a Huginn agent's backend.
//
// It implements backend.Backend, with two documented departures from the
// interface's usual semantics:
//
//   - req.Messages is ignored past the newest turn. Claude Code owns the
//     conversation; replaying history would duplicate it.
//   - ToolCalls is always empty. Claude Code executes its own tools, so they
//     are reported via ExecutedTools for persistence and must never be
//     dispatched by the agent loop.
type AgentBackend struct {
	cfg AgentBackendConfig

	mu   sync.Mutex // serialises turns: one writer per transcript
	args []string   // last argv, for tests
}

var _ backend.Backend = (*AgentBackend)(nil)

// NewAgentBackend returns a backend bound to one Claude Code session.
func NewAgentBackend(cfg AgentBackendConfig) *AgentBackend { return &AgentBackend{cfg: cfg} }

func (b *AgentBackend) lastArgs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.args...)
}

func (b *AgentBackend) Health(context.Context) error   { return nil }
func (b *AgentBackend) Shutdown(context.Context) error { return nil }
func (b *AgentBackend) ContextWindow() int             { return 200000 }

func (b *AgentBackend) ChatCompletion(ctx context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	// One turn at a time per session: two concurrent writers would corrupt the
	// transcript, which is the session's only source of truth.
	b.mu.Lock()
	defer b.mu.Unlock()

	prompt, err := newestUserMessage(req.Messages)
	if err != nil {
		return nil, err
	}

	args, err := b.buildArgs(prompt)
	if err != nil {
		return nil, err
	}
	b.args = args

	cmd := exec.CommandContext(ctx, b.cfg.Binary, args...)
	if b.cfg.CWD != "" {
		cmd.Dir = b.cfg.CWD
	}
	// Own process group, so cancelling the turn kills the tools the CLI
	// spawned rather than orphaning them. See delegate_unix.go.
	setProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("claudecode agent: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("claudecode agent: start %s: %w", b.cfg.Binary, err)
	}

	var (
		res   DelegateResult
		tools []backend.ExecutedTool
	)
	onEvent := func(e StreamEvent) {
		switch e.Type {
		case "text":
			if req.OnEvent != nil {
				req.OnEvent(backend.StreamEvent{Type: backend.StreamText, Content: e.Text})
			} else if req.OnToken != nil {
				req.OnToken(e.Text)
			}
		case "tool_use":
			// Surfaced for live display only; persistence happens via
			// ExecutedTools, which the loop must not dispatch.
			if req.OnEvent != nil {
				req.OnEvent(backend.NewToolCallEvent(e.ToolName))
			}
		}
	}

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		applyStreamLine(line, &res, onEvent)
		tools = appendExecutedTools(tools, line)
	}

	if waitErr := cmd.Wait(); waitErr != nil {
		return nil, fmt.Errorf("claudecode agent: %s failed: %w", b.cfg.Binary, waitErr)
	}
	if res.IsError {
		return nil, fmt.Errorf("claudecode agent: %s", res.ErrorText)
	}

	// ToolCalls is deliberately left nil: Claude Code already ran these.
	return &backend.ChatResponse{
		Content:       res.Text,
		DoneReason:    "stop",
		ExecutedTools: tools,
	}, nil
}

// buildArgs delegates to the package's single argument builder. The agent-only
// flags live on DelegateRequest so there is exactly one place that knows the
// CLI's argv shape.
func (b *AgentBackend) buildArgs(prompt string) ([]string, error) {
	settings, err := BuildHookSettings(b.cfg.GatedTools, b.cfg.HookCommand)
	if err != nil {
		return nil, fmt.Errorf("claudecode agent: build hook settings: %w", err)
	}
	// A zero DelegateConfig is load-bearing: it guarantees no inherited
	// permission mode, no --max-turns, and above all SkipPermissions=false, so
	// an agent backend can never emit --dangerously-skip-permissions.
	return BuildArgs(DelegateConfig{}, DelegateRequest{
		Prompt:             prompt,
		Model:              b.cfg.Model,
		AllowedTools:       b.cfg.AllowedTools,
		Resume:             !b.cfg.FirstTurn,
		AppendSystemPrompt: b.cfg.SystemPrompt,
		Settings:           settings,
		MCPConfig:          b.cfg.MCPConfig,
	}, b.cfg.SessionID), nil
}

// newestUserMessage returns the last user turn. Claude Code owns the rest.
func newestUserMessage(msgs []backend.Message) (string, error) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" && strings.TrimSpace(msgs[i].Content) != "" {
			return msgs[i].Content, nil
		}
	}
	return "", fmt.Errorf("claudecode agent: no user message to send")
}

// appendExecutedTools collects tool_use blocks from assistant lines and fills
// their results from the following user line's tool_result blocks.
func appendExecutedTools(acc []backend.ExecutedTool, raw []byte) []backend.ExecutedTool {
	var sl streamLine
	if err := json.Unmarshal(raw, &sl); err != nil {
		return acc
	}
	for _, blk := range sl.Message.Content {
		switch blk.Type {
		case "tool_use":
			acc = append(acc, backend.ExecutedTool{
				Call: backend.ToolCall{
					ID:       blk.ID,
					Function: backend.ToolCallFunction{Name: blk.Name, Arguments: blk.Input},
				},
			})
		case "tool_result":
			for i := range acc {
				if acc[i].Call.ID == blk.ToolUseID {
					acc[i].Result = rawToString(blk.Content)
				}
			}
		}
	}
	return acc
}
