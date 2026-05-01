package threadmgr

import (
	"strings"
	"sync"
	"time"
)

// DefaultThreadBusCapacity bounds per-session context history.
const DefaultThreadBusCapacity = 128

// ThreadContextMessage is a short context update shared between sibling threads.
type ThreadContextMessage struct {
	ThreadID string
	AgentID  string
	// TargetAgentID scopes the update to a single delegate. Empty means broadcast.
	TargetAgentID string
	Content       string
	At            time.Time
}

// ThreadBus keeps bounded per-session context messages that sibling threads can
// consume to stay coordinated without sharing full chat history.
type ThreadBus struct {
	mu        sync.RWMutex
	capacity  int
	bySession map[string][]ThreadContextMessage
}

// NewThreadBus returns a ThreadBus with bounded per-session capacity.
func NewThreadBus(capacity int) *ThreadBus {
	if capacity <= 0 {
		capacity = DefaultThreadBusCapacity
	}
	return &ThreadBus{
		capacity:  capacity,
		bySession: map[string][]ThreadContextMessage{},
	}
}

// Publish stores a context update for a session. Empty content is ignored.
func (b *ThreadBus) Publish(sessionID string, msg ThreadContextMessage) {
	if b == nil || sessionID == "" {
		return
	}
	msg.Content = strings.TrimSpace(msg.Content)
	if msg.Content == "" {
		return
	}
	if msg.At.IsZero() {
		msg.At = time.Now()
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	items := append(b.bySession[sessionID], msg)
	if len(items) > b.capacity {
		items = append([]ThreadContextMessage(nil), items[len(items)-b.capacity:]...)
	}
	b.bySession[sessionID] = items
}

// SiblingContext returns recent context updates from a session, excluding the
// given thread ID. When forAgentID is set, directed updates are returned only
// when TargetAgentID matches forAgentID (case-insensitive).
func (b *ThreadBus) SiblingContext(sessionID, excludeThreadID, forAgentID string, limit int) []ThreadContextMessage {
	if b == nil || sessionID == "" {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}

	b.mu.RLock()
	src := b.bySession[sessionID]
	b.mu.RUnlock()
	if len(src) == 0 {
		return nil
	}

	out := make([]ThreadContextMessage, 0, limit)
	for i := len(src) - 1; i >= 0 && len(out) < limit; i-- {
		if excludeThreadID != "" && src[i].ThreadID == excludeThreadID {
			continue
		}
		if src[i].TargetAgentID != "" && !strings.EqualFold(strings.TrimSpace(src[i].TargetAgentID), strings.TrimSpace(forAgentID)) {
			continue
		}
		out = append(out, src[i])
	}
	for l, r := 0, len(out)-1; l < r; l, r = l+1, r-1 {
		out[l], out[r] = out[r], out[l]
	}
	return out
}

// ClearSession removes all context updates for a session.
func (b *ThreadBus) ClearSession(sessionID string) {
	if b == nil || sessionID == "" {
		return
	}
	b.mu.Lock()
	delete(b.bySession, sessionID)
	b.mu.Unlock()
}
