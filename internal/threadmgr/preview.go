package threadmgr

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const defaultPreviewTimeout = 30 * time.Second

// DelegationPreviewMode controls whether and when delegation previews require
// explicit user approval before a thread is spawned.
type DelegationPreviewMode string

const (
	PreviewModeOff         DelegationPreviewMode = "off"
	PreviewModeManual      DelegationPreviewMode = "manual"
	PreviewModeConditional DelegationPreviewMode = "conditional"
	PreviewModeAuto        DelegationPreviewMode = "auto"
)

// DelegationPreviewGate optionally waits for user acknowledgment before
// a thread is spawned. When disabled, Approve() returns true immediately.
type DelegationPreviewGate struct {
	mode    DelegationPreviewMode
	timeout time.Duration

	mu   sync.Mutex
	acks map[string]chan bool // key: sessionID+":"+threadID
}

// NewDelegationPreviewGate creates a gate with the given enabled state.
func NewDelegationPreviewGate(enabled bool) *DelegationPreviewGate {
	mode := PreviewModeOff
	if enabled {
		mode = PreviewModeManual
	}
	return NewDelegationPreviewGateWithConfig(string(mode), defaultPreviewTimeout)
}

// NewDelegationPreviewGateWithConfig creates a gate with an explicit mode and
// timeout. Invalid modes fall back to "manual"; non-positive timeouts fall back
// to the default timeout.
func NewDelegationPreviewGateWithConfig(mode string, timeout time.Duration) *DelegationPreviewGate {
	parsedMode := normalizePreviewMode(mode)
	if timeout <= 0 {
		timeout = defaultPreviewTimeout
	}
	return &DelegationPreviewGate{
		mode:    parsedMode,
		timeout: timeout,
		acks:    make(map[string]chan bool),
	}
}

func normalizePreviewMode(mode string) DelegationPreviewMode {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", string(PreviewModeManual):
		return PreviewModeManual
	case string(PreviewModeConditional):
		return PreviewModeConditional
	case string(PreviewModeAuto):
		return PreviewModeAuto
	case "disabled", string(PreviewModeOff):
		return PreviewModeOff
	default:
		return PreviewModeManual
	}
}

func requiresManualApproval(task string) bool {
	riskyHints := []string{
		"delete", "remove", "drop", "destroy", "truncate",
		"prod", "production", "live", "customer data",
		"secret", "credential", "token", "key",
		"permission", "billing", "invoice", "payment",
	}
	text := strings.ToLower(task)
	for _, hint := range riskyHints {
		if strings.Contains(text, hint) {
			return true
		}
	}
	return false
}

// ackKey returns a unique key for the session+thread pair.
// Uses a null byte separator that is never valid in session or thread IDs,
// preventing collisions when sessionID contains a colon character.
func ackKey(sessionID, threadID string) string {
	return sessionID + "\x00" + threadID
}

// Approve blocks until the user acknowledges (Ack) or the timeout elapses.
// Returns true if approved (or preview disabled / timeout). The broadcastFn
// is called with "delegation_preview" before blocking — may be nil.
// Returns false immediately if another Approve call for the same key is already pending.
// parentMessageID is the chat message that triggered this delegation; included in the
// broadcast payload so the frontend can link the preview to its originating message.
func (g *DelegationPreviewGate) Approve(
	ctx context.Context,
	sessionID, threadID, agentName, task, parentMessageID string,
	broadcastFn func(sessionID, msgType string, payload map[string]any),
) bool {
	mode := g.mode
	switch mode {
	case PreviewModeOff, PreviewModeAuto:
		return true
	case PreviewModeConditional:
		if !requiresManualApproval(task) {
			return true
		}
	}
	return g.approveCore(ctx, sessionID, threadID, agentName, task, parentMessageID, mode, nil, true, broadcastFn)
}

// SpecialistPreviewInfo carries the model and verified per-MTok cost of a
// one-off specialist spawn, surfaced in the delegation_preview payload so
// the human approving it can see what it will cost (S10).
type SpecialistPreviewInfo struct {
	Model             string
	InputCostPerMTok  float64
	OutputCostPerMTok float64
}

// ApproveSpecialist is Approve's S10 counterpart for spawn_specialist
// threads: in PreviewModeConditional it ALWAYS requires approval (skips the
// risky-hint heuristic — every specialist spawn is inherently a judgment
// call about cost and roster gaps), the broadcast payload carries the
// specialist's model and estimated cost, and — critically — a timeout with
// no human response DENIES the spawn instead of auto-approving it. Off and
// Auto modes behave the same as Approve (global disable / trusted-auto).
func (g *DelegationPreviewGate) ApproveSpecialist(
	ctx context.Context,
	sessionID, threadID, agentName, task, parentMessageID string,
	info SpecialistPreviewInfo,
	broadcastFn func(sessionID, msgType string, payload map[string]any),
) bool {
	mode := g.mode
	switch mode {
	case PreviewModeOff, PreviewModeAuto:
		return true
	}
	extra := map[string]any{
		"model":                info.Model,
		"input_cost_per_mtok":  info.InputCostPerMTok,
		"output_cost_per_mtok": info.OutputCostPerMTok,
		"specialist":           true,
	}
	return g.approveCore(ctx, sessionID, threadID, agentName, task, parentMessageID, mode, extra, false, broadcastFn)
}

// approveCore is the shared blocking wait behind Approve and
// ApproveSpecialist. extraPayload, when non-nil, is merged into the
// delegation_preview broadcast payload. timeoutApproves controls what
// happens when no human response arrives before g.timeout: true preserves
// Approve's long-standing default-approve behavior; false is S10's
// default-deny for specialist spawns.
func (g *DelegationPreviewGate) approveCore(
	ctx context.Context,
	sessionID, threadID, agentName, task, parentMessageID string,
	mode DelegationPreviewMode,
	extraPayload map[string]any,
	timeoutApproves bool,
	broadcastFn func(sessionID, msgType string, payload map[string]any),
) bool {
	ch := make(chan bool, 1)
	key := ackKey(sessionID, threadID)
	g.mu.Lock()
	if _, exists := g.acks[key]; exists {
		// Another Approve is already pending for this key; reject to prevent
		// the first goroutine's channel from being orphaned (blocking forever).
		g.mu.Unlock()
		return false
	}
	g.acks[key] = ch
	g.mu.Unlock()

	defer func() {
		g.mu.Lock()
		delete(g.acks, key)
		g.mu.Unlock()
	}()

	if broadcastFn != nil {
		timeoutSeconds := int(g.timeout.Seconds())
		if timeoutSeconds < 1 {
			timeoutSeconds = 1
		}
		payload := map[string]any{
			"thread_id":          threadID,
			"agent_id":           agentName,
			"agent":              agentName, // backward-compatible alias for older clients
			"task":               task,
			"mode":               string(mode),
			"expires_in_seconds": timeoutSeconds,
		}
		for k, v := range extraPayload {
			payload[k] = v
		}
		if parentMessageID != "" {
			payload["parent_message_id"] = parentMessageID
		}
		broadcastFn(sessionID, "delegation_preview", payload)
	}

	select {
	case approved := <-ch:
		return approved
	case <-ctx.Done():
		return false
	case <-time.After(g.timeout):
		if broadcastFn != nil {
			timeoutSeconds := int(g.timeout.Seconds())
			if timeoutSeconds < 1 {
				timeoutSeconds = 1
			}
			payload := map[string]any{
				"thread_id":       threadID,
				"agent_id":        agentName,
				"agent":           agentName, // backward-compatible alias for older clients
				"task":            task,
				"timeout_seconds": timeoutSeconds,
			}
			if parentMessageID != "" {
				payload["parent_message_id"] = parentMessageID
			}
			broadcastFn(sessionID, "delegation_preview_timeout", payload)
		}
		if !timeoutApproves {
			slog.Warn("specialist delegation preview timed out without user ack — denying (S10 default-deny)",
				"session_id", sessionID, "thread_id", threadID, "agent", agentName, "timeout", g.timeout)
			return false
		}
		slog.Warn("delegation preview timed out without user ack — auto-approving",
			"session_id", sessionID, "thread_id", threadID, "agent", agentName, "timeout", g.timeout)
		return true // timeout → default approve
	}
}

// Ack delivers a user acknowledgment for the given session+thread.
// approved=true → thread spawns; false → thread is cancelled.
// Returns true when an in-flight preview was matched and updated.
func (g *DelegationPreviewGate) Ack(sessionID, threadID string, approved bool) bool {
	key := ackKey(sessionID, threadID)
	g.mu.Lock()
	ch, ok := g.acks[key]
	g.mu.Unlock()
	if ok {
		select {
		case ch <- approved:
			return true
		default:
		}
	}
	return false
}
