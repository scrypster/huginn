package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/scrypster/huginn/internal/streaming"
)

// ChatModel holds all state related to the active chat session.
// It groups streaming channels, history, and cancellation into a single
// sub-struct so that App's top-level field count stays manageable.
type ChatModel struct {
	// history is the list of messages displayed in the viewport.
	history []chatLine

	// streaming accumulates regular tokens from the model during a response.
	streaming strings.Builder

	// thoughtStreaming accumulates extended-thinking tokens; displayed but
	// NOT committed to history.
	thoughtStreaming strings.Builder

	// tokenCount tracks the number of tokens received in the current stream.
	tokenCount int

	// cancelStream cancels the in-flight streaming context.
	cancelStream context.CancelFunc

	// runner is the Runner-based streaming backend (streamChat path).
	runner *streaming.Runner

	// tokenCh carries raw tokens from the agent loop / dispatch / approval paths.
	tokenCh chan string

	// errCh is the shared error channel for both streaming patterns.
	errCh chan error

	// eventCh is the unified event channel for the agent loop (nil when idle).
	eventCh chan tea.Msg
}

// Reset clears all transient streaming state, preparing the ChatModel for a
// new interaction. History is preserved.
func (c *ChatModel) Reset() {
	c.streaming.Reset()
	c.thoughtStreaming.Reset()
	c.tokenCount = 0
	c.cancelStream = nil
	c.runner = nil
	c.tokenCh = nil
	c.errCh = nil
	c.eventCh = nil
}

// AddLine appends a chat line to history.
func (c *ChatModel) AddLine(role, content string) {
	c.history = append(c.history, chatLine{role: role, content: content})
}

// IsStreaming returns true if any streaming state is active (tokens accumulated
// or a cancel function is set).
func (c *ChatModel) IsStreaming() bool {
	return c.cancelStream != nil
}

// ClearHistory removes all chat lines.
func (c *ChatModel) ClearHistory() {
	c.history = nil
}

// SetArtifactContent stores the content for the artifact overlay so it can be
// displayed when the user presses ctrl+o on an artifact line.
func (a *App) SetArtifactContent(id, content, kind, title string) {
	a.artifactOverlay.content = content
	a.artifactOverlay.kind = kind
	a.artifactOverlay.title = title
}

// acceptArtifactAtCursor marks the most recent artifact line as "accepted" and
// adds a system confirmation message, then fires a non-blocking API call to
// persist the status change via PATCH /api/v1/artifacts/{id}/status.
func (a *App) acceptArtifactAtCursor() {
	for i := len(a.chat.history) - 1; i >= 0; i-- {
		if a.chat.history[i].isArtifactLine {
			a.chat.history[i].renderedCache = "" // invalidate cached render
			a.chat.history[i].artifactStatus = "accepted"
			a.addLine("system", fmt.Sprintf("✓ Artifact accepted: %s", a.chat.history[i].artifactTitle))
			a.refreshViewport()
			if id := a.chat.history[i].artifactID; id != "" {
				go a.patchArtifactStatus(id, "accepted")
			}
			return
		}
	}
}

// rejectArtifactAtCursor marks the most recent artifact line as "rejected" and
// adds a system rejection message, then fires a non-blocking API call to
// persist the status change via PATCH /api/v1/artifacts/{id}/status.
func (a *App) rejectArtifactAtCursor() {
	for i := len(a.chat.history) - 1; i >= 0; i-- {
		if a.chat.history[i].isArtifactLine {
			a.chat.history[i].renderedCache = "" // invalidate cached render
			a.chat.history[i].artifactStatus = "rejected"
			a.addLine("system", fmt.Sprintf("✗ Artifact rejected: %s", a.chat.history[i].artifactTitle))
			a.refreshViewport()
			if id := a.chat.history[i].artifactID; id != "" {
				go a.patchArtifactStatus(id, "rejected")
			}
			return
		}
	}
}

// patchArtifactStatus calls PATCH /api/v1/artifacts/{id}/status to persist an
// artifact status change to the backend. It is intended to be called in a
// goroutine so it does not block the TUI event loop. Errors are silently
// discarded because the local state has already been updated.
func (a *App) patchArtifactStatus(artifactID, status string) {
	baseURL := a.artifactServerBaseURL()
	if baseURL == "" {
		return
	}
	url := baseURL + "/api/v1/artifacts/" + artifactID + "/status"
	body, err := json.Marshal(map[string]string{"status": status})
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// artifactServerBaseURL returns the local server base URL derived from the
// WebUI configuration, e.g. "http://127.0.0.1:8421". Returns empty string
// when the config is nil or the web UI is disabled.
func (a *App) artifactServerBaseURL() string {
	if a.cfg == nil {
		return ""
	}
	bind := a.cfg.WebUI.Bind
	if bind == "" {
		bind = "127.0.0.1"
	}
	port := a.cfg.WebUI.Port
	if port == 0 {
		return ""
	}
	return fmt.Sprintf("http://%s:%d", bind, port)
}

// updateSwarmBar finds an existing swarm bar in chat history by swarmID and
// updates it in place. If no bar exists yet, a new one is appended.
func (a *App) updateSwarmBar(swarmID string, agents []swarmAgentStatus) {
	for i := range a.chat.history {
		if a.chat.history[i].role == "swarm-bar" && a.chat.history[i].swarmID == swarmID {
			a.chat.history[i].swarmAgents = agents
			a.chatLineOffsetsDirty = true
			a.refreshViewport()
			return
		}
	}
	// Not found — append a new swarm bar.
	a.chat.history = append(a.chat.history, chatLine{
		role:        "swarm-bar",
		swarmID:     swarmID,
		swarmTitle:  swarmID,
		swarmAgents: agents,
	})
	a.chatLineOffsetsDirty = true
	a.refreshViewport()
}

// openArtifactOverlay transitions to the full-screen artifact view state.
// It loads the most recent artifact line's content into the overlay viewport.
func (a *App) openArtifactOverlay() {
	// Find the most recent artifact line.
	for i := len(a.chat.history) - 1; i >= 0; i-- {
		if a.chat.history[i].isArtifactLine {
			line := a.chat.history[i]
			if a.artifactOverlay.content == "" || a.artifactOverlay.title != line.artifactTitle {
				a.artifactOverlay.title = line.artifactTitle
				a.artifactOverlay.kind = line.artifactKind
				// content may have been pre-loaded via SetArtifactContent
			}
			a.artifactOverlay.viewport.Width = 0 // force rebuild
			a.state = stateArtifactView
			return
		}
	}
	// If no artifact line found but we have overlay content already loaded, show it.
	if a.artifactOverlay.content != "" {
		a.artifactOverlay.viewport.Width = 0
		a.state = stateArtifactView
	}
}

// openThreadOverlay transitions to the full-screen thread overlay for the most
// recent expanded (non-collapsed) thread header.
func (a *App) openThreadOverlay() {
	for i := len(a.chat.history) - 1; i >= 0; i-- {
		line := a.chat.history[i]
		if line.role == "thread-header" && !line.threadCollapsed {
			a.threadOverlay.threadID = line.threadID
			a.threadOverlay.title = line.content
			a.threadOverlay.agentChain = []string{line.threadAgentName}
			a.threadOverlay.lines = collectThreadLines(a.chat.history, line.threadID)
			a.threadOverlay.viewport.Width = 0
			a.state = stateThreadOverlay
			return
		}
	}
	// If no expanded thread, take the most recent thread header.
	for i := len(a.chat.history) - 1; i >= 0; i-- {
		line := a.chat.history[i]
		if line.role == "thread-header" {
			a.threadOverlay.threadID = line.threadID
			a.threadOverlay.title = line.content
			a.threadOverlay.agentChain = []string{line.threadAgentName}
			a.threadOverlay.lines = collectThreadLines(a.chat.history, line.threadID)
			a.threadOverlay.viewport.Width = 0
			a.state = stateThreadOverlay
			return
		}
	}
}

// collectThreadLines returns all chat lines that belong to a given thread.
// If threadID is empty, returns all lines.
func collectThreadLines(history []chatLine, threadID string) []chatLine {
	if threadID == "" {
		return history
	}
	var result []chatLine
	for _, line := range history {
		if line.threadID == threadID {
			result = append(result, line)
		}
	}
	return result
}

// openObservationDeck transitions to the observation deck overlay and builds
// a narrated walkthrough from the current thread context.
func (a *App) openObservationDeck() {
	// Collect relevant lines (thread lines or all history if no thread focused).
	var contextLines []chatLine
	for i := len(a.chat.history) - 1; i >= 0; i-- {
		if a.chat.history[i].role == "thread-header" && !a.chat.history[i].threadCollapsed {
			contextLines = collectThreadLines(a.chat.history, a.chat.history[i].threadID)
			a.observationDeck.title = a.chat.history[i].threadAgentName + " · " + a.chat.history[i].content
			break
		}
	}
	if len(contextLines) == 0 {
		contextLines = a.chat.history
		if len(contextLines) > 0 {
			a.observationDeck.title = "session context"
		}
	}
	a.observationDeck.lines = buildObservationDeck(contextLines)
	a.observationDeck.viewport.Width = 0
	a.state = stateObservationDeck
}

// buildObservationDeck generates a numbered narrated walkthrough from a slice
// of chat lines. It is a pure transformation — no LLM calls are made.
func buildObservationDeck(lines []chatLine) []string {
	var steps []string
	step := 1

	toolCalls := make(map[string]int)    // tool name → count
	fileReads := []string{}
	fileWrites := []string{}
	artifacts := []string{}
	agentNames := map[string]bool{}

	for _, line := range lines {
		if line.agentName != "" {
			agentNames[line.agentName] = true
		}
		switch line.role {
		case "tool-call":
			toolCalls[line.toolName]++
			// Track file reads/writes.
			if line.toolName == "read_file" {
				if line.content != "" {
					fileReads = append(fileReads, line.content)
				}
			} else if line.toolName == "write_file" || line.toolName == "edit_file" {
				if line.content != "" {
					fileWrites = append(fileWrites, line.content)
				}
			}
		case "delegation-start":
			agentLabel := ""
			if line.agentName != "" {
				agentLabel = line.agentName
			}
			steps = append(steps, fmt.Sprintf("%d. %s delegated task", step, agentLabel))
			step++
		case "artifact":
			if line.isArtifactLine && line.artifactTitle != "" {
				artifacts = append(artifacts, line.artifactTitle)
			}
		}
	}

	// Synthesize steps from collected data.
	for _, name := range sortedKeys(agentNames) {
		_ = name
	}

	// Memory recalls.
	if n, ok := toolCalls["muninn_recall"]; ok && n > 0 {
		steps = append(steps, fmt.Sprintf("%d. Recalled memory (via muninn_recall, %dx)", step, n))
		step++
	}

	// File reads.
	if len(fileReads) > 0 {
		steps = append(steps, fmt.Sprintf("%d. Read %d files: %s", step, len(fileReads), strings.Join(fileReads, ", ")))
		step++
	}

	// Tool summary (bash, etc.).
	for toolName, count := range toolCalls {
		if toolName == "muninn_recall" || toolName == "read_file" || toolName == "write_file" || toolName == "edit_file" {
			continue
		}
		steps = append(steps, fmt.Sprintf("%d. Ran: %s (%dx)", step, toolName, count))
		step++
	}

	// File writes.
	if len(fileWrites) > 0 {
		steps = append(steps, fmt.Sprintf("%d. Wrote %d files: %s", step, len(fileWrites), strings.Join(fileWrites, ", ")))
		step++
	}

	// Produced artifacts.
	if len(artifacts) > 0 {
		steps = append(steps, fmt.Sprintf("%d. Produced: %s", step, strings.Join(artifacts, ", ")))
		step++
	}

	if len(steps) == 0 {
		steps = append(steps, "(no recorded steps)")
	}

	return steps
}

// sortedKeys returns the keys of a map[string]bool in sorted order.
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple sort: iterate to build a sorted slice.
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

// AddThreadSummaryLine appends a collapsed thread-header line to the history.
// agentName is the agent that ran the thread, tools is a comma-separated list
// of tool names used, elapsed is a human-readable duration (e.g. "14s"), and
// content is a one-line summary of what the thread accomplished.
//
// The thread starts collapsed; the user can expand it with ↵ and collapse
// again with space.
func (c *ChatModel) AddThreadSummaryLine(agentName, tools, elapsed, content string) {
	color := agentColorFromName(agentName)
	c.history = append(c.history, chatLine{
		role:            "thread-header",
		content:         content,
		isThreadHeader:  true,
		threadAgentName: agentName,
		threadToolsUsed: tools,
		threadElapsed:   elapsed,
		threadCollapsed: true,
		agentName:       agentName,
		agentColor:      color,
		agentIcon:       agentIconFromName(agentName),
	})
}
