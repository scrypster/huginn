package threadmgr

import (
	"context"
	"sync"
	"time"
)

const previewTimeout = 30 * time.Second

// DelegationPreviewGate optionally waits for user acknowledgment before
// a thread is spawned. When disabled, Approve() returns true immediately.
type DelegationPreviewGate struct {
	enabled bool

	mu   sync.Mutex
	acks map[string]chan bool // key: sessionID+":"+threadID
}

// NewDelegationPreviewGate creates a gate with the given enabled state.
func NewDelegationPreviewGate(enabled bool) *DelegationPreviewGate {
	return &DelegationPreviewGate{
		enabled: enabled,
		acks:    make(map[string]chan bool),
	}
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
	if !g.enabled {
		return true
	}

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
		payload := map[string]any{
			"thread_id": threadID,
			"agent":     agentName,
			"task":      task,
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
	case <-time.After(previewTimeout):
		return true // timeout → default approve
	}
}

// Ack delivers a user acknowledgment for the given session+thread.
// approved=true → thread spawns; false → thread is cancelled.
// No-op if no pending Approve for this key.
func (g *DelegationPreviewGate) Ack(sessionID, threadID string, approved bool) {
	key := ackKey(sessionID, threadID)
	g.mu.Lock()
	ch, ok := g.acks[key]
	g.mu.Unlock()
	if ok {
		select {
		case ch <- approved:
		default:
		}
	}
}
