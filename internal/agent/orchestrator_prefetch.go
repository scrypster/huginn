package agent

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/proactivity"
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

type continuityModeCtxKey struct{}

const (
	// ContinuityModeConversational is the default interactive-chat behavior.
	ContinuityModeConversational = "conversational"
	// ContinuityModeDeterministic is the workflow-safe, task-scoped behavior.
	ContinuityModeDeterministic = "deterministic"
)

// WithContinuityMode sets how continuity context is assembled for this request.
func WithContinuityMode(ctx context.Context, mode string) context.Context {
	mode = strings.TrimSpace(strings.ToLower(mode))
	switch mode {
	case ContinuityModeDeterministic, ContinuityModeConversational:
		return context.WithValue(ctx, continuityModeCtxKey{}, mode)
	default:
		return ctx
	}
}

func continuityModeFromContext(ctx context.Context) proactivity.ContinuityMode {
	mode, _ := ctx.Value(continuityModeCtxKey{}).(string)
	if mode == ContinuityModeDeterministic {
		return proactivity.ContinuityModeDeterministic
	}
	return proactivity.ContinuityModeConversational
}

// pinMuninnVault returns the vault name Huginn must send on every Muninn call.
// Omitting vault makes Muninn write/read the `default` vault — a leak.
// Empty or explicit "default" is rewritten to huginn (this work's vault).
func pinMuninnVault(vaultName string) string {
	v := strings.TrimSpace(vaultName)
	if v == "" || strings.EqualFold(v, "default") {
		return "huginn"
	}
	if !muninnVaultNameOK(v) {
		return "huginn"
	}
	return v
}

func muninnVaultNameOK(v string) bool {
	if len(v) < 1 || len(v) > 64 {
		return false
	}
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

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
	return o.prefetchMemoryContextWithEvents(ctx, sessionReg, agentName, vaultName, userMsg, nil)
}

// prefetchMemoryContextWithEvents is identical to prefetchMemoryContext but
// invokes onPrefetch when a muninn_* call actually fires (i.e. on cache miss
// or first call). The callback runs synchronously after the tool returns; it
// must not block. Used by the WS path to surface synthetic tool_call /
// tool_result events to the UI so the user can see "agent recalled memory"
// even when the call happens in the silent prefetch phase.
//
// onPrefetch may be nil — in which case behaviour is identical to the
// no-event variant above. cached=true indicates this invocation served the
// block from cache without dispatching a real MCP call (callers can use this
// to suppress duplicate UI events).
func (o *Orchestrator) prefetchMemoryContextWithEvents(
	ctx context.Context,
	sessionReg *tools.Registry,
	agentName, vaultName, userMsg string,
	onPrefetch func(toolName string, args map[string]any, output string, cached bool),
) string {
	if sessionReg == nil {
		return ""
	}
	whereTool, hasWhere := sessionReg.Get("muninn_where_left_off")
	recallTool, hasRecall := sessionReg.Get("muninn_recall")
	if !hasWhere && !hasRecall {
		return ""
	}

	gate := memoryGateFromContext(ctx)
	pinned := pinMuninnVault(vaultName)
	mode := normalizeMemoryMode(gate.Mode)
	sessionKey := gate.SessionID
	if sessionKey == "" {
		sessionKey = agentName
	}
	doWhere, doRecall, reuseRecall := o.pullCadence(sessionKey, mode, userMsg)
	slog.Info("memory.inject", "kind", "start", "vault", pinned, "mode", mode, "agent", agentName)

	wloKey := agentName + ":" + vaultName
	whereOutput := ""
	if hasWhere {
		whereOutput = o.getCachedMemoryPrefetch(wloKey)
		switch {
		case whereOutput != "":
			slog.Info("memory.inject", "kind", "where_left_off", "vault", pinned, "cached", true)
			if onPrefetch != nil {
				onPrefetch("muninn_where_left_off", map[string]any{"vault": pinned}, whereOutput, true)
			}
		case doWhere:
			prefetchCtx, cancel := context.WithTimeout(ctx, prefetchTimeout)
			result := whereTool.Execute(prefetchCtx, map[string]any{"vault": pinned})
			cancel()
			if !result.IsError && result.Output != "" {
				whereOutput = trimToLines(result.Output, prefetchMaxItems)
				o.setCachedMemoryPrefetch(wloKey, whereOutput)
				slog.Info("memory.inject", "kind", "where_left_off", "vault", pinned, "cached", false)
				if onPrefetch != nil {
					onPrefetch("muninn_where_left_off", map[string]any{"vault": pinned}, whereOutput, false)
				}
			}
		}
	}

	recallQuery := sanitizeRecallQuery(userMsg)
	recallOutput := ""
	if recallQuery != "" && hasRecall {
		recallArgs := map[string]any{
			"vault":     pinned,
			"context":   []string{recallQuery},
			"mode":      "balanced",
			"limit":     5,
			"threshold": 0.6,
		}
		if !doRecall {
			recallOutput = reuseRecall
			if recallOutput != "" {
				slog.Info("memory.inject", "kind", "recall", "vault", pinned, "cached", true)
				if onPrefetch != nil {
					onPrefetch("muninn_recall", recallArgs, recallOutput, true)
				}
			}
		} else {
			recallKey := agentName + ":" + vaultName + ":recall:" + hashMessage(recallQuery)
			if cachedRecall := o.getCachedSemanticPrefetch(recallKey); cachedRecall != "" {
				recallOutput = cachedRecall
				slog.Info("memory.inject", "kind", "recall", "vault", pinned, "cached", true)
				if onPrefetch != nil {
					onPrefetch("muninn_recall", recallArgs, recallOutput, true)
				}
			} else {
				recallCtx, recallCancel := context.WithTimeout(ctx, prefetchTimeout)
				recallResult := recallTool.Execute(recallCtx, recallArgs)
				recallCancel()
				if !recallResult.IsError && recallResult.Output != "" {
					recallOutput = trimToLines(recallResult.Output, 10)
					o.setCachedSemanticPrefetch(recallKey, recallOutput)
					slog.Info("memory.inject", "kind", "recall", "vault", pinned, "cached", false)
					if onPrefetch != nil {
						onPrefetch("muninn_recall", recallArgs, recallOutput, false)
					}
				}
			}
		}
	}
	o.rememberPull(sessionKey, userMsg, recallOutput)

	// Immersive session-start also guide, once per session.
	if normalizeMemoryMode(gate.Mode) == MemoryModeImmersive {
		sessionKey := gate.SessionID
		if sessionKey == "" {
			sessionKey = agentName
		}
		if sessionKey != "" && o.shouldGuideSession(sessionKey) {
			if guideTool, hasGuide := sessionReg.Get("muninn_guide"); hasGuide {
				gCtx, gCancel := context.WithTimeout(ctx, prefetchTimeout)
				gRes := guideTool.Execute(gCtx, map[string]any{"vault": pinned})
				gCancel()
				if !gRes.IsError && gRes.Output != "" {
					slog.Info("memory.inject", "kind", "guide", "vault", pinned, "cached", false)
					if onPrefetch != nil {
						onPrefetch("muninn_guide", map[string]any{"vault": pinned}, gRes.Output, false)
					}
				}
			}
		}
	}

	cmode := continuityModeFromContext(ctx)
	return proactivity.AssembleContinuityPack(proactivity.ContinuityPackInput{
		Mode:               cmode,
		UserMessage:        userMsg,
		WhereLeftOffOutput: whereOutput,
		RecallOutput:       recallOutput,
	})
}

// shouldGuideSession returns true the first time sessionKey is seen.
func (o *Orchestrator) shouldGuideSession(sessionKey string) bool {
	if o == nil || sessionKey == "" {
		return false
	}
	_, loaded := o.memoryGuided.LoadOrStore(sessionKey, true)
	return !loaded
}

type sessionPullState struct {
	lastUser string
	recall   string
}

// pullCadence decides whether this turn may fire a Muninn MCP pull.
// immersive: every turn. conversational: session start + topic-shift.
// passive: session-start only. Cached inject is still allowed.
func (o *Orchestrator) pullCadence(sessionKey, mode, userMsg string) (doWhere, doRecall bool, reuseRecall string) {
	if o == nil {
		return true, true, ""
	}
	mode = normalizeMemoryMode(mode)
	if sessionKey == "" {
		sessionKey = "default"
	}
	if mode == MemoryModeImmersive {
		return true, true, ""
	}
	raw, ok := o.memoryPulled.Load(sessionKey)
	var st sessionPullState
	if ok {
		st, _ = raw.(sessionPullState)
	}
	if !ok {
		return true, true, ""
	}
	if mode == MemoryModePassive {
		return false, false, st.recall
	}
	if topicShifted(st.lastUser, userMsg) {
		return true, true, ""
	}
	return false, false, st.recall
}

func (o *Orchestrator) rememberPull(sessionKey, userMsg, recall string) {
	if o == nil || sessionKey == "" {
		return
	}
	prev, _ := o.memoryPulled.Load(sessionKey)
	st, _ := prev.(sessionPullState)
	if recall == "" {
		recall = st.recall
	}
	if userMsg == "" {
		userMsg = st.lastUser
	}
	o.memoryPulled.Store(sessionKey, sessionPullState{lastUser: userMsg, recall: recall})
}

// maxRecallQueryChars caps how much of the raw user message is sent to
// muninn_recall as the semantic query. A rambling turn shouldn't blow the
// query past what the vault's embedder expects.
const maxRecallQueryChars = 500

// sanitizeRecallQuery strips DM/channel @mention prefixes and truncates the
// user's ask before it becomes the muninn_recall "context" query. Leading
// mentions ("@Winston what's our production database called?") are pure
// routing noise — leaving them in the semantic query dilutes it against the
// actual question. Live repro: 2026-08-27 Winston DM, recall query polluted
// by the mention prefix returned no hits for a fact that was in the vault.
func sanitizeRecallQuery(userMsg string) string {
	s := stripLeadingMentions(userMsg)
	s = strings.TrimSpace(s)
	if len(s) > maxRecallQueryChars {
		r := []rune(s)
		if len(r) > maxRecallQueryChars {
			s = strings.TrimSpace(string(r[:maxRecallQueryChars]))
		}
	}
	return s
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
