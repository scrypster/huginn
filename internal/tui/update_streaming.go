package tui

// update_streaming.go — handlers for streaming/token/tool lifecycle messages.
// These were extracted from the monolithic Update() in app.go.
// All methods are on *App and return (tea.Model, tea.Cmd).

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// handleTokenMsg appends a regular streaming token to the chat buffer and
// requests the next token from the active stream.
func (a *App) handleTokenMsg(msg tokenMsg) (tea.Model, tea.Cmd) {
	a.chat.streaming.WriteString(string(msg))
	a.chat.tokenCount++
	a.refreshViewport()
	if a.chat.eventCh != nil {
		return a, waitForEvent(a.chat.eventCh, a.chat.errCh)
	}
	if a.chat.runner == nil {
		return a, nil
	}
	return a, waitForToken(a.chat.runner.TokenCh(), a.chat.runner.ErrCh())
}

// handleThinkingTokenMsg appends an extended-thinking token to the thought
// buffer and requests the next token from the active stream.
func (a *App) handleThinkingTokenMsg(msg thinkingTokenMsg) (tea.Model, tea.Cmd) {
	a.chat.thoughtStreaming.WriteString(StyleThought.Render(string(msg)))
	a.chat.tokenCount++
	a.refreshViewport()
	if a.chat.eventCh != nil {
		return a, waitForEvent(a.chat.eventCh, a.chat.errCh)
	}
	if a.chat.runner == nil {
		return a, nil
	}
	return a, waitForToken(a.chat.runner.TokenCh(), a.chat.runner.ErrCh())
}

// handleWarningMsg flushes any buffered assistant text, then appends the
// warning as a standalone "system" chat line. Writing pre-styled ANSI text
// into the streaming buffer causes raw escape codes to appear in the viewport
// when the buffer is later re-rendered — this avoids that problem entirely.
func (a *App) handleWarningMsg(msg warningMsg) (tea.Model, tea.Cmd) {
	if a.chat.streaming.Len() > 0 {
		a.addAssistantLine(a.chat.streaming.String())
		a.chat.streaming.Reset()
	}
	a.addLine("system", string(msg))
	a.refreshViewport()
	if a.chat.eventCh != nil {
		return a, waitForEvent(a.chat.eventCh, a.chat.errCh)
	}
	return a, nil
}

// handleToolCallMsg flushes any pending assistant text, increments the agent
// turn counter, and displays the tool invocation preview line.
func (a *App) handleToolCallMsg(msg toolCallMsg) (tea.Model, tea.Cmd) {
	if a.chat.eventCh == nil {
		return a, nil
	}
	if a.chat.streaming.Len() > 0 {
		a.addAssistantLine(a.chat.streaming.String())
		a.chat.streaming.Reset()
	}
	a.chat.thoughtStreaming.Reset()
	a.agentTurn++
	preview := fmtToolCallPreview(msg.name, msg.args)
	a.chat.history = append(a.chat.history, chatLine{role: "tool-call", content: preview, toolName: msg.name})
	a.refreshViewport()
	return a, waitForEvent(a.chat.eventCh, a.chat.errCh)
}

// handleToolDoneMsg appends a tool result line to the chat history, including
// duration and optional truncation for long outputs.
func (a *App) handleToolDoneMsg(msg toolDoneMsg) (tea.Model, tea.Cmd) {
	if a.chat.eventCh == nil {
		return a, nil
	}
	if msg.isError {
		a.chat.history = append(a.chat.history, chatLine{
			role:     "tool-error",
			content:  fmt.Sprintf("✗ %s: %s", msg.name, msg.preview),
			toolName: msg.name,
		})
	} else {
		fullLines := strings.Split(msg.fullOutput, "\n")
		truncated := 0
		if len(fullLines) > toolOutputMaxLines {
			truncated = len(fullLines) - toolOutputMaxLines
		}
		durationStr := ""
		if msg.duration > 0 {
			durationStr = formatDuration(msg.duration)
		}
		a.chat.history = append(a.chat.history, chatLine{
			role:       "tool-done",
			content:    msg.preview,
			toolName:   msg.name,
			duration:   durationStr,
			truncated:  truncated,
			fullOutput: msg.fullOutput,
		})
	}
	a.refreshViewport()
	return a, waitForEvent(a.chat.eventCh, a.chat.errCh)
}

func (a *App) handleCtrlCResetMsg(_ ctrlCResetMsg) (tea.Model, tea.Cmd) {
	a.ctrlCPending = false
	return a, nil
}

func (a *App) handleShellResultMsg(msg shellResultMsg) (tea.Model, tea.Cmd) {
	a.chat.cancelStream = nil
	a.state = stateChat
	a.chat.tokenCount = 0
	if msg.err != nil && len(msg.output) == 0 {
		a.addLine("error", fmt.Sprintf("$ %s\n%v", msg.cmd, msg.err))
		a.shellContext = ""
	} else {
		output := strings.TrimRight(msg.output, "\n")
		if output == "" {
			output = "(no output)"
		}
		a.addLine("system", fmt.Sprintf("$ %s\n%s", msg.cmd, output))
		a.shellContext = fmt.Sprintf("Shell command: %s\nOutput:\n%s", msg.cmd, output)
	}
	a.recalcViewportHeight()
	a.refreshViewport()
	return a, nil
}

// handleStreamDoneMsg finalises a completed stream: commits buffered text to
// history, records pricing, persists the session manifest, and dispatches any
// queued follow-up message.
func (a *App) handleStreamDoneMsg(msg streamDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		// Auto-recover from "model does not support tools" without showing
		// a raw HTTP error. Switch to chat-only mode and retry transparently.
		if strings.Contains(msg.err.Error(), "does not support tools") {
			return a.recoverToolsUnsupported()
		}
		slog.Warn("tui: streaming error", "err", msg.err)
		a.addLine("error", fmt.Sprintf("Error: %v", msg.err))
	} else if a.chat.streaming.Len() > 0 {
		a.addAssistantLine(a.chat.streaming.String())
	}
	a.chat.streaming.Reset()
	a.chat.thoughtStreaming.Reset()
	lastModel := a.activeModel
	a.activeModel = ""
	a.agentTurn = 0
	a.chat.runner = nil
	a.chat.eventCh = nil
	a.state = stateChat
	if a.priceTracker != nil && a.orch != nil {
		prompt, completion := a.orch.LastUsage()
		if prompt > 0 || completion > 0 {
			a.priceTracker.Add(lastModel, prompt, completion)
		}
	}
	if a.sessionStore != nil && a.activeSession != nil {
		go func() {
			if err := a.sessionStore.SaveManifest(a.activeSession); err != nil {
				slog.Warn("tui: failed to save session manifest", "err", err)
			}
		}()
	}
	a.recalcViewportHeight()
	a.refreshViewport()
	if a.queuedMsg != "" && msg.err == nil {
		queued := a.queuedMsg
		a.queuedMsg = ""
		a.recalcViewportHeight()
		return a, a.submitMessage(queued)
	}
	return a, nil
}

// recoverToolsUnsupported handles the case where the active model doesn't
// support tool calling. It:
//  1. Permanently disables the agent loop for this session
//  2. Shows a subtle one-line notice (not a red error)
//  3. Finds the last user message and retries it in plain chat mode
//
// The user sees their message answered as if nothing went wrong.
func (a *App) recoverToolsUnsupported() (tea.Model, tea.Cmd) {
	// Resolve the actual model in use — same logic as the footer renderer.
	modelName := a.cfg.DefaultModel
	if a.primaryAgent != "" && a.agentReg != nil {
		if ag, ok := a.agentReg.ByName(a.primaryAgent); ok {
			if m := ag.GetModelID(); m != "" {
				modelName = m
			}
		}
	}
	if a.activeModel != "" {
		modelName = a.activeModel
	}

	// Reset all streaming state exactly as handleStreamDoneMsg would.
	a.chat.streaming.Reset()
	a.chat.thoughtStreaming.Reset()
	a.activeModel = ""
	a.agentTurn = 0
	a.chat.runner = nil
	a.chat.eventCh = nil
	a.chat.errCh = nil
	a.state = stateChat

	// Disable tools permanently for the remainder of the session.
	a.useAgentLoop = false

	// Show a gentle, informative notice — not a red error.
	notice := fmt.Sprintf("⚠  %s doesn't support tools — switched to chat mode", modelName)
	a.addLine("system", notice)

	// Find the last user message in history to retry.
	var lastUserMsg string
	for i := len(a.chat.history) - 1; i >= 0; i-- {
		if a.chat.history[i].role == "user" {
			lastUserMsg = a.chat.history[i].content
			break
		}
	}

	a.recalcViewportHeight()
	a.refreshViewport()

	// Auto-retry without tools so the user gets an answer.
	if lastUserMsg != "" && a.orch != nil {
		ctx, cancel := context.WithCancel(context.Background())
		a.chat.cancelStream = cancel
		a.state = stateStreaming
		return a, a.streamChat(ctx, lastUserMsg)
	}
	return a, nil
}

func (a *App) handleParallelDoneMsg(msg parallelDoneMsg) (tea.Model, tea.Cmd) {
	a.state = stateChat
	a.activeModel = ""
	if msg.output != "" {
		a.addAssistantLine(msg.output)
	}
	a.refreshViewport()
	return a, nil
}
