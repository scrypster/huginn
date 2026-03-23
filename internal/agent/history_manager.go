package agent

import (
	"context"
	"fmt"

	"github.com/scrypster/huginn/internal/backend"
)

const (
	// hydrateMaxMessages is the maximum number of messages loaded during session
	// re-hydration. Capped to stay within a reasonable context window budget.
	hydrateMaxMessages = 50
	// hydrateMaxBytes is the byte budget for re-hydration content (64 KB).
	// After fetching up to hydrateMaxMessages, messages are trimmed from the
	// front until total content length is within budget.
	hydrateMaxBytes = 64 * 1024
)

// ExportHistory returns a copy of the current conversation history for the default session.
func (o *Orchestrator) ExportHistory() []backend.Message {
	o.mu.RLock()
	sess := o.defaultSession()
	o.mu.RUnlock()
	return sess.snapshotHistory()
}

// ImportHistory replaces the conversation history for the default session (for session resume).
func (o *Orchestrator) ImportHistory(msgs []backend.Message) {
	o.mu.RLock()
	sess := o.defaultSession()
	o.mu.RUnlock()
	cp := make([]backend.Message, len(msgs))
	copy(cp, msgs)
	sess.replaceHistory(cp)
}

// HydrateSession loads the persistent conversation history for the given session
// ID into the orchestrator's in-memory session, enabling the LLM to have context
// from previous conversation turns after a restart or session resume.
//
// Up to hydrateMaxMessages messages are fetched, then trimmed from the front until
// total content is within hydrateMaxBytes. Tool-call messages (tool_name != "") are
// intentionally dropped: they reference tool invocations from a prior run whose
// results are stale and would confuse the LLM if re-sent.
//
// If no session store has been wired (SetSessionStore), this is a no-op.
func (o *Orchestrator) HydrateSession(_ context.Context, sessionID string) error {
	o.mu.RLock()
	store := o.sessionStore
	o.mu.RUnlock()

	if store == nil {
		return nil
	}

	rawMsgs, err := store.TailMessages(sessionID, hydrateMaxMessages)
	if err != nil {
		return fmt.Errorf("HydrateSession: TailMessages: %w", err)
	}

	// Convert to backend.Message, dropping stale tool-call entries.
	// Tool messages from a previous session are stale: the tool results
	// no longer exist in any active tool-call cycle and would produce
	// invalid API payloads if resent.
	backendMsgs := make([]backend.Message, 0, len(rawMsgs))
	for _, m := range rawMsgs {
		if m.ToolName != "" || m.ToolCallID != "" {
			continue
		}
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		backendMsgs = append(backendMsgs, backend.Message{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	// Trim from the front until total content is within the byte budget.
	for len(backendMsgs) > 0 {
		total := 0
		for _, m := range backendMsgs {
			total += len(m.Content)
		}
		if total <= hydrateMaxBytes {
			break
		}
		backendMsgs = backendMsgs[1:]
	}

	// Find the in-memory session and replace its history.
	o.mu.RLock()
	sess, ok := o.sessions[sessionID]
	if !ok {
		// Fall back to the default session (single-session TUI usage).
		sess = o.defaultSession()
	}
	o.mu.RUnlock()

	sess.replaceHistory(backendMsgs)
	return nil
}
