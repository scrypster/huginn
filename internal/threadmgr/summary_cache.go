package threadmgr

import "sync"

// summaryEntry holds a cached summary and the message count when it was generated.
type summaryEntry struct {
	text     string
	msgCount int
}

// SummaryCache is a thread-safe per-session rolling summary cache.
// It only stores a summary if its msgCount is greater than what's currently stored
// (newer summaries win over older ones).
//
// Used for instant primary agent switching: the new agent receives the cached
// summary of the conversation so far instead of blocking on a live LLM call.
type SummaryCache struct {
	mu      sync.Mutex
	entries map[string]summaryEntry // sessionID → latest summary entry
}

// NewSummaryCache returns a ready-to-use SummaryCache.
func NewSummaryCache() *SummaryCache {
	return &SummaryCache{entries: make(map[string]summaryEntry)}
}

// Store saves a summary for the given session.
// Only stores if msgCount > the currently stored count (newer wins).
// Safe for concurrent use.
func (c *SummaryCache) Store(sessionID string, msgCount int, summary string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	existing, ok := c.entries[sessionID]
	if ok && existing.msgCount >= msgCount {
		return // don't overwrite with same-or-older data
	}
	c.entries[sessionID] = summaryEntry{text: summary, msgCount: msgCount}
}

// Get returns the latest cached summary for the session and whether one exists.
// Safe for concurrent use.
func (c *SummaryCache) Get(sessionID string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[sessionID]
	if !ok {
		return "", false
	}
	return e.text, true
}

// Invalidate removes the cached summary for the given session.
// No-op if the session has no cached entry. Safe for concurrent use.
func (c *SummaryCache) Invalidate(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, sessionID)
}
