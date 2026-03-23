package agent

import (
	"context"
	"time"

	"github.com/scrypster/huginn/internal/tools"
)

const semanticPrefetchTTL = 60 * time.Second

// briefingCacheTTL is how long a where_left_off result is cached per agent+vault.
const briefingCacheTTL = 5 * time.Minute

// Default cache parameters.
const (
	// prefetchCacheMaxAge is the default TTL applied during Get — entries older
	// than this are evicted on access.
	prefetchCacheMaxAge = 30 * time.Minute

	// prefetchCacheMaxSize is the maximum number of entries in a prefetch cache.
	// When full, the LRU entry is evicted before inserting a new one.
	prefetchCacheMaxSize = 100
)

// prefetchCacheEntry holds a single cached prefetch result.
type prefetchCacheEntry struct {
	key        string
	prompt     string
	expires    time.Time // TTL-based expiry
	lastAccess time.Time // for LRU eviction
}

// briefingCacheEntry is kept for backward compatibility with the Orchestrator
// struct fields (used only as named fields, not as a multi-entry cache).
type briefingCacheEntry = prefetchCacheEntry

// prefetchCache is a bounded, TTL-aware in-memory cache for prefetch results.
type prefetchCache struct {
	maxAge  time.Duration
	maxSize int
	entries map[string]*prefetchCacheEntry
}

func newPrefetchCache(maxAge time.Duration, maxSize int) *prefetchCache {
	if maxAge <= 0 {
		maxAge = prefetchCacheMaxAge
	}
	if maxSize <= 0 {
		maxSize = prefetchCacheMaxSize
	}
	return &prefetchCache{
		maxAge:  maxAge,
		maxSize: maxSize,
		entries: make(map[string]*prefetchCacheEntry),
	}
}

// get returns the cached value for key, or "" if not present / expired.
// Evicts all entries older than maxAge on each call.
func (c *prefetchCache) get(key string) string {
	now := time.Now()
	// Evict expired entries.
	for k, e := range c.entries {
		if now.After(e.expires) || now.Sub(e.lastAccess) > c.maxAge {
			delete(c.entries, k)
		}
	}
	e, ok := c.entries[key]
	if !ok {
		return ""
	}
	e.lastAccess = now
	return e.prompt
}

// set stores key→content with a TTL. If the cache is full, the LRU entry is
// evicted first.
func (c *prefetchCache) set(key, content string, ttl time.Duration) {
	now := time.Now()
	// If key already exists, update in place.
	if e, ok := c.entries[key]; ok {
		e.prompt = content
		e.expires = now.Add(ttl)
		e.lastAccess = now
		return
	}
	// Evict LRU entry if at capacity.
	if len(c.entries) >= c.maxSize {
		var lruKey string
		var lruTime time.Time
		for k, e := range c.entries {
			if lruKey == "" || e.lastAccess.Before(lruTime) {
				lruKey = k
				lruTime = e.lastAccess
			}
		}
		delete(c.entries, lruKey)
	}
	c.entries[key] = &prefetchCacheEntry{
		key:        key,
		prompt:     content,
		expires:    now.Add(ttl),
		lastAccess: now,
	}
}

const (
	// prefetchTimeout is the hard deadline for the silent muninn_where_left_off
	// call. If Muninn does not respond within this window the pre-fetch is
	// skipped and the chat proceeds without injected context.
	prefetchTimeout = 2 * time.Second

	// prefetchMaxItems caps how many lines of where_left_off output we inject
	// into the system prompt to avoid bloating the context window.
	// Bumped from 10 to 20 for better context retention.
	prefetchMaxItems = 20
)

// prefetchMemoryContext silently calls muninn_where_left_off (if registered)
// and returns a formatted block ready to append to the system prompt.
// Returns "" if the tool is unavailable, times out, or errors.
//
// The where_left_off result is cached per agentName+vaultName for briefingCacheTTL
// (5 min). The per-message semantic recall (muninn_recall) is cached separately
// per message hash for semanticPrefetchTTL (60s). The two caches are kept
// independent so a different message always gets its own recall block even when
// the where_left_off block is still warm in cache.
//
// Phase 4: when userMsg is non-empty, also calls muninn_recall for semantic context.
func (o *Orchestrator) prefetchMemoryContext(ctx context.Context, sessionReg *tools.Registry, agentName, vaultName, userMsg string) string {
	if sessionReg == nil {
		return ""
	}
	tool, ok := sessionReg.Get("muninn_where_left_off")
	if !ok {
		return ""
	}

	// Cache only the where_left_off block (not recall) under the agent+vault key.
	wloKey := agentName + ":" + vaultName
	wloBlock := o.getCachedMemoryPrefetch(wloKey)
	if wloBlock == "" {
		// Hard timeout so we never stall the chat waiting for a slow Muninn server.
		prefetchCtx, cancel := context.WithTimeout(ctx, prefetchTimeout)
		result := tool.Execute(prefetchCtx, map[string]any{})
		cancel()
		if result.IsError || result.Output == "" {
			return ""
		}
		content := trimToLines(result.Output, prefetchMaxItems)
		wloBlock = "## Memory Context\n\n" + content + "\n\n"
		o.setCachedMemoryPrefetch(wloKey, wloBlock)
	}

	formatted := wloBlock

	// Phase 4: per-message semantic recall. Always fetched/cached independently of
	// where_left_off so each unique message gets its own relevant memories.
	if userMsg != "" {
		if recallTool, ok := sessionReg.Get("muninn_recall"); ok {
			recallKey := agentName + ":" + vaultName + ":recall:" + hashMessage(userMsg)
			if recallBlock := o.getCachedSemanticPrefetch(recallKey); recallBlock != "" {
				formatted += recallBlock
			} else {
				recallCtx, recallCancel := context.WithTimeout(ctx, prefetchTimeout)
				recallResult := recallTool.Execute(recallCtx, map[string]any{
					"context":   []string{userMsg},
					"mode":      "balanced",
					"limit":     5,
					"threshold": 0.6,
				})
				recallCancel()
				if !recallResult.IsError && recallResult.Output != "" {
					block := "## Relevant Memory\n\n" + trimToLines(recallResult.Output, 10) + "\n\n"
					o.setCachedSemanticPrefetch(recallKey, block)
					formatted += block
				}
			}
		}
	}

	return formatted
}

// trimToLines returns at most n lines from s, appending "…" if truncated.
func trimToLines(s string, n int) string {
	lines := splitLines(s)
	if len(lines) <= n {
		return s
	}
	result := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			result += "\n"
		}
		result += lines[i]
	}
	return result + "\n…"
}

// splitLines splits s on newlines without allocating a full strings.Split.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// getCachedMemoryPrefetch returns the cached pre-fetch block for key, or "".
func (o *Orchestrator) getCachedMemoryPrefetch(key string) string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	if o.memoryPrefetchCache == nil {
		return ""
	}
	return o.memoryPrefetchCache.get(key)
}

// setCachedMemoryPrefetch stores the pre-fetch result with a TTL.
func (o *Orchestrator) setCachedMemoryPrefetch(key, content string) {
	o.mu.Lock()
	if o.memoryPrefetchCache == nil {
		o.memoryPrefetchCache = newPrefetchCache(prefetchCacheMaxAge, prefetchCacheMaxSize)
	}
	o.memoryPrefetchCache.set(key, content, briefingCacheTTL)
	o.mu.Unlock()
}

// getCachedSemanticPrefetch returns the cached semantic recall block for key, or "".
// TTL is semanticPrefetchTTL (60s) — shorter than briefingCacheTTL because messages vary.
func (o *Orchestrator) getCachedSemanticPrefetch(key string) string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	if o.semanticPrefetchCache == nil {
		return ""
	}
	return o.semanticPrefetchCache.get(key)
}

// setCachedSemanticPrefetch stores a semantic recall result with a short TTL.
func (o *Orchestrator) setCachedSemanticPrefetch(key, content string) {
	o.mu.Lock()
	if o.semanticPrefetchCache == nil {
		o.semanticPrefetchCache = newPrefetchCache(semanticPrefetchTTL, prefetchCacheMaxSize)
	}
	o.semanticPrefetchCache.set(key, content, semanticPrefetchTTL)
	o.mu.Unlock()
}
