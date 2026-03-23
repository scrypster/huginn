package tui

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/swarm"
	"github.com/scrypster/huginn/internal/tools"
)

// tokenMsg carries a streamed token from the model.
type tokenMsg string

// thinkingTokenMsg carries a StreamThought (extended thinking) token from the model.
// It is rendered in muted gray italic and NOT appended to the final message content.
type thinkingTokenMsg string

// streamDoneMsg signals streaming is complete.
type streamDoneMsg struct{ err error }

// warningMsg carries a non-fatal warning to display in the chat stream.
type warningMsg string

// streamEventToMsg converts a backend.StreamEvent into a tea.Msg for dispatch in Update().
func streamEventToMsg(e backend.StreamEvent) tea.Msg {
	switch e.Type {
	case backend.StreamThought:
		return thinkingTokenMsg(e.Content)
	case backend.StreamDone:
		return streamDoneMsg{}
	case backend.StreamWarning:
		return warningMsg(e.Content)
	default: // StreamText and any future types
		return tokenMsg(e.Content)
	}
}

// dotTickMsg advances the animated typing-indicator one frame.
type dotTickMsg struct{}

// dotTickCmd fires a dotTickMsg after ~400 ms.
func dotTickCmd() tea.Cmd {
	return tea.Tick(400*time.Millisecond, func(_ time.Time) tea.Msg {
		return dotTickMsg{}
	})
}

// ctrlCResetMsg is sent 3 seconds after the first ctrl+c to clear the pending state.
type ctrlCResetMsg struct{}

func ctrlCResetCmd() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(3 * time.Second)
		return ctrlCResetMsg{}
	}
}

// shellResultMsg is sent when a ! shell command finishes.
type shellResultMsg struct {
	cmd    string
	output string
	err    error
}

// toolCallMsg is sent when the agent invokes a tool.
type toolCallMsg struct {
	name string
	args map[string]any
}

// toolDoneMsg is sent when a tool finishes executing.
type toolDoneMsg struct {
	name       string
	isError    bool
	preview    string
	duration   time.Duration // execution time
	fullOutput string        // full output for expand feature
}

// delegationStartMsg is sent when an agent begins consulting another agent.
type delegationStartMsg struct {
	From     string
	To       string
	Question string
}

// delegationTokenMsg carries a streaming token from the consulted agent.
type delegationTokenMsg struct {
	Agent string
	Token string
}

// delegationDoneMsg is sent when the consulted agent finishes.
type delegationDoneMsg struct {
	From   string
	To     string
	Answer string
}

// agentDispatchFallbackMsg signals that orch.Dispatch determined the input
// was not an agent directive; the TUI should fall through to normal chat routing.
type agentDispatchFallbackMsg struct{ input string }

// wsEventMsg carries an inbound WebSocket-style event dispatched into the TUI
// update loop (e.g. "primary_agent_changed" broadcast from the server).
type wsEventMsg struct {
	Type    string
	Payload map[string]any
}

// swarmEventMsg carries a single SwarmEvent from the swarm event channel.
type swarmEventMsg struct {
	event swarm.SwarmEvent
}

// swarmDoneMsg signals swarm execution is complete.
type swarmDoneMsg struct {
	output string
}

// parallelDoneMsg is sent when BatchChat completes all parallel tasks.
type parallelDoneMsg struct{ output string }

// waitForToken returns a Cmd that reads one token from the channel.
// Each tokenMsg triggers another waitForToken, driving the streaming loop.
func waitForToken(ch <-chan string, errCh <-chan error) tea.Cmd {
	return func() tea.Msg {
		token, ok := <-ch
		if !ok {
			err := <-errCh
			return streamDoneMsg{err: err}
		}
		return tokenMsg(token)
	}
}

// readSwarmEvent returns a tea.Cmd that reads one event from the swarm channel.
// Chained calls form an event bridge without goroutine leaks.
func readSwarmEvent(ch <-chan swarm.SwarmEvent) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		e, ok := <-ch
		if !ok {
			return swarmDoneMsg{}
		}
		return swarmEventMsg{event: e}
	}
}

// waitForEvent reads from the unified event channel used by the agentic loop.
func waitForEvent(ch <-chan tea.Msg, errCh <-chan error) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			err := <-errCh
			return streamDoneMsg{err: err}
		}
		return msg
	}
}

// tryDispatch attempts to dispatch input to a named agent via orch.Dispatch.
// The caller has already verified ContainsAgentName is true.
// If Dispatch returns handled=false (neither Tier 1 nor future Tier 2 matched),
// an agentDispatchFallbackMsg is sent so Update() can route to normal chat.
// Returns nil only when the orchestrator or registry guards fail.
func (a *App) tryDispatch(ctx context.Context, input string) tea.Cmd {
	if a.orch == nil || a.agentReg == nil {
		return nil
	}

	evCh := make(chan tea.Msg, 256)
	errCh := make(chan error, 1)
	a.chat.eventCh = evCh
	a.chat.errCh = errCh

	maxTurns := 50
	if a.cfg != nil && a.cfg.MaxTurns > 0 {
		maxTurns = a.cfg.MaxTurns
	}

	var once sync.Once
	go func() {
		defer func() {
			if r := recover(); r != nil {
				once.Do(func() { close(evCh) })
				errCh <- fmt.Errorf("internal panic: %v", r)
			}
		}()
		handled, err := a.orch.Dispatch(ctx, input,
			nil, // OnToken: use OnEvent instead (avoids double delivery)
			func(name string, args map[string]any) {
				slog.Debug("tui: tool call", "tool", name)
				evCh <- toolCallMsg{name: name, args: args}
			},
			func(name string, result tools.ToolResult) {
				start := time.Now()
				preview := result.Output
				if len(preview) > 120 {
					preview = preview[:120] + "…"
				}
				if result.IsError && result.Error != "" {
					preview = result.Error
				}
				slog.Debug("tui: tool execution done", "tool", name, "is_error", result.IsError)
				evCh <- toolDoneMsg{
					name:       name,
					isError:    result.IsError,
					preview:    preview,
					duration:   time.Since(start),
					fullOutput: result.Output,
				}
			},
			func(name string) {
				slog.Debug("tui: tool permission denied", "tool", name)
				evCh <- toolDoneMsg{name: name, isError: true, preview: "permission denied"}
			},
			&maxTurns,
			func(event backend.StreamEvent) {
				evCh <- streamEventToMsg(event)
			},
		)
		if err != nil {
			once.Do(func() { close(evCh) })
			errCh <- err
			return
		}
		if !handled {
			// Neither Tier 1 nor Tier 2 matched — signal TUI to fall through to
			// normal chat routing so the input is not silently dropped.
			evCh <- agentDispatchFallbackMsg{input: input}
		}
		once.Do(func() { close(evCh) })
		errCh <- nil
	}()

	return waitForEvent(evCh, errCh)
}

func (a *App) streamAgentChat(ctx context.Context, userMsg string) tea.Cmd {
	evCh := make(chan tea.Msg, 256)
	errCh := make(chan error, 1)
	a.chat.eventCh = evCh
	a.chat.errCh = errCh
	maxTurns := 50
	if a.cfg != nil && a.cfg.MaxTurns > 0 {
		maxTurns = a.cfg.MaxTurns
	}

	// Wire consult_agent tool with TUI delegation callbacks.
	if a.agentReg != nil {
		if toolReg := a.orch.ToolRegistry(); toolReg != nil {
			agents.RegisterConsultTool(
				toolReg,
				a.agentReg,
				a.orch.Backend(),
				&a.consultDepth,
				func(from, to, question string) {
					evCh <- delegationStartMsg{From: from, To: to, Question: question}
				},
				func(from, to, answer string) {
					evCh <- delegationDoneMsg{From: from, To: to, Answer: answer}
				},
				func(agentName, token string) {
					evCh <- delegationTokenMsg{Agent: agentName, Token: token}
				},
			)
		}
	}

	var once sync.Once
	go func() {
		defer func() {
			if r := recover(); r != nil {
				once.Do(func() { close(evCh) })
				errCh <- fmt.Errorf("internal panic: %v", r)
			}
		}()
		err := a.orch.AgentChat(ctx, userMsg, maxTurns,
			nil, // OnToken: use OnEvent instead (avoids double delivery)
			func(name string, args map[string]any) { evCh <- toolCallMsg{name: name, args: args} },
			func(name string, result tools.ToolResult) {
				start := time.Now()
				preview := result.Output
				if len(preview) > 120 {
					preview = preview[:120] + "…"
				}
				if result.IsError && result.Error != "" {
					preview = result.Error
				}
				evCh <- toolDoneMsg{
					name:       name,
					isError:    result.IsError,
					preview:    preview,
					duration:   time.Since(start),
					fullOutput: result.Output,
				}
			},
			func(name string) {
				evCh <- toolDoneMsg{name: name, isError: true, preview: "permission denied"}
			},
			func(path string, oldContent, newContent []byte) bool {
				respCh := make(chan bool, 1)
				evCh <- writeApprovalMsg{
					Path:       path,
					OldContent: oldContent,
					NewContent: newContent,
					RespCh:     respCh,
				}
				select {
				case allowed := <-respCh:
					return allowed
				case <-ctx.Done():
					return false // context cancelled — deny the write
				}
			},
			func(event backend.StreamEvent) {
				evCh <- streamEventToMsg(event)
			},
		)
		once.Do(func() { close(evCh) })
		errCh <- err
	}()
	return waitForEvent(evCh, errCh)
}

func (a *App) streamChat(ctx context.Context, userMsg string) tea.Cmd {
	evCh := make(chan tea.Msg, 256)
	errCh := make(chan error, 1)
	a.chat.eventCh = evCh
	a.chat.errCh = errCh

	var once sync.Once
	go func() {
		defer func() {
			if r := recover(); r != nil {
				once.Do(func() { close(evCh) })
				errCh <- fmt.Errorf("internal panic: %v", r)
			}
		}()
		err := a.orch.Chat(ctx, userMsg,
			nil, // OnToken: use OnEvent instead (avoids double delivery)
			func(event backend.StreamEvent) {
				evCh <- streamEventToMsg(event)
			},
		)
		once.Do(func() { close(evCh) })
		errCh <- err
	}()
	return waitForEvent(evCh, errCh)
}

// runShellCmd runs a shell command asynchronously and returns a Cmd that
// delivers a shellResultMsg when complete. The command is cancellable via ctx.
func (a *App) runShellCmd(ctx context.Context, shellCmd string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.CommandContext(ctx, "sh", "-c", shellCmd)
		out, err := cmd.CombinedOutput()
		return shellResultMsg{cmd: shellCmd, output: string(out), err: err}
	}
}

// fmtToolCallPreview formats a tool name + args into a short display string.
func fmtToolCallPreview(name string, args map[string]any) string {
	switch name {
	case "bash":
		if cmd, ok := args["command"].(string); ok {
			cmd = strings.ReplaceAll(cmd, "\n", " ")
			if len(cmd) > 80 {
				cmd = cmd[:80] + "…"
			}
			return fmt.Sprintf("bash: %s", cmd)
		}
	case "read_file", "write_file", "edit_file":
		if path, ok := args["file_path"].(string); ok {
			return fmt.Sprintf("%s: %s", name, path)
		}
	case "list_dir":
		if path, ok := args["path"].(string); ok {
			return fmt.Sprintf("list_dir: %s", path)
		}
	case "search_files":
		if pat, ok := args["pattern"].(string); ok {
			return fmt.Sprintf("search_files: %s", pat)
		}
	case "grep":
		if pat, ok := args["pattern"].(string); ok {
			return fmt.Sprintf("grep: %s", pat)
		}
	}
	return fmt.Sprintf("%s(…)", name)
}

// formatDuration formats a duration as a short human-readable string (e.g. "1.2s", "450ms").
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
