package threadmgr

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
)

// CompletionNotifier broadcasts a structured thread_result event to the frontend
// when a sub-agent thread finishes. No LLM call is made — this is a lightweight
// structured notification that the frontend renders inline in the main chat.
type CompletionNotifier struct {
	// Backend and BackendFor are kept for interface compatibility but no longer used.
	Backend    backend.Backend
	BackendFor func(ag *agents.Agent) (backend.Backend, error)

	AgentReg     *agents.AgentRegistry
	Store        session.StoreInterface
	Broadcast    BroadcastFn
	PrimaryAgent func(sessionID string) *agents.Agent

	// ThreadLookup resolves a thread by ID so Notify can stamp ParentMessageID
	// onto the summary for reply threading. Nil is safe (skips lookup).
	ThreadLookup func(threadID string) (*Thread, bool)

	// FollowUpFn, if set, is called in a goroutine after broadcasting thread_result
	// to trigger a lead-agent synthesis reply in the main chat.
	// Called with a background context that has a 3-minute timeout.
	// Implementations must be non-blocking (the function runs in its own goroutine).
	FollowUpFn func(ctx context.Context, sessionID, agentID string, summary *FinishSummary)
}

// followUpTimeout caps how long a lead-agent follow-up synthesis may take.
const followUpTimeout = 3 * time.Minute

// Notify broadcasts a thread_result event to the frontend. No LLM call is made.
// The frontend renders this inline in the main chat as a structured completion card.
// If FollowUpFn is wired, it is called asynchronously so the primary agent can
// synthesize a reply acknowledging Sam's completion.
func (n *CompletionNotifier) Notify(_ context.Context, sessionID, threadID, agentID string, summary *FinishSummary) {
	if summary == nil {
		return
	}

	summaryText := strings.TrimSpace(summary.Summary)
	status := summary.Status
	if status == "" {
		status = "completed"
	}

	// Clip summary in the broadcast payload — the frontend no longer renders
	// the full text in main chat (thread panel is the authoritative display).
	// Cap defensively so the WS payload stays small.
	const maxBroadcastSummaryLen = 200
	broadcastSummary := summaryText
	if len(broadcastSummary) > maxBroadcastSummaryLen {
		broadcastSummary = broadcastSummary[:maxBroadcastSummaryLen] + "…"
	}

	// Broadcast a structured thread_result event. Frontend no-ops on this for
	// main-chat display; the event is retained for future telemetry/extensions.
	n.Broadcast(sessionID, "thread_result", map[string]any{
		"thread_id": threadID,
		"agent_id":  agentID,
		"status":    status,
		"summary":   broadcastSummary,
	})

	// Stamp ParentMessageID onto the summary so the FollowUpFn can thread
	// the lead agent's synthesis reply under the original user message.
	if n.ThreadLookup != nil {
		if t, ok := n.ThreadLookup(threadID); ok && t.ParentMessageID != "" {
			summary.ParentMessageID = t.ParentMessageID
		}
	}

	// Trigger lead-agent follow-up asynchronously so Tom can acknowledge
	// Sam's completion and synthesize a response in the main chat.
	if n.FollowUpFn != nil {
		s := *summary // copy for goroutine
		fn := n.FollowUpFn
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Warn("completion_notify: follow-up panic", "recover", r)
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), followUpTimeout)
			defer cancel()
			fn(ctx, sessionID, agentID, &s)
		}()
	}
}
