package server

import (
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/scrypster/huginn/internal/symbol"
)

// symbolQuerier is the minimal interface the symbol handlers require.
// *storage.Store satisfies this via GetAllSymbolEdges().
type symbolQuerier interface {
	GetAllSymbolEdges() []symbol.Edge
}

// SetSymbolStore wires the symbol data store used by /api/v1/symbols/* handlers.
func (s *Server) SetSymbolStore(sq symbolQuerier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.symbolStore = sq
	s.symbolCache = &symbolIndexCache{}
}

// symbolNameRe restricts symbol names to safe characters (alphanumeric, underscore, dot,
// dollar, colon, hyphen) with a maximum length of 128 characters.
var symbolNameRe = regexp.MustCompile(`^[a-zA-Z0-9_.$:-]{1,128}$`)

const (
	symbolCacheTTL = 30 * time.Second
	defaultLimit   = 50
	maxLimit       = 200
)

// symbolIndexCache holds a pre-built SymbolIndex refreshed on a TTL basis.
// Building the index on every request would be O(n) allocation; the TTL
// bounds staleness to 30 seconds while avoiding per-request allocations.
type symbolIndexCache struct {
	mu      sync.RWMutex
	idx     symbol.SymbolIndex
	builtAt time.Time
}

func (c *symbolIndexCache) get(sq symbolQuerier) symbol.SymbolIndex {
	c.mu.RLock()
	if c.idx != nil && time.Since(c.builtAt) < symbolCacheTTL {
		idx := c.idx
		c.mu.RUnlock()
		return idx
	}
	c.mu.RUnlock()

	// Double-checked locking — only one goroutine rebuilds at a time.
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.idx != nil && time.Since(c.builtAt) < symbolCacheTTL {
		return c.idx
	}
	edges := sq.GetAllSymbolEdges()
	c.idx = symbol.BuildSymbolIndex(edges)
	c.builtAt = time.Now()
	return c.idx
}

// handleSymbolImpact returns an ImpactReport for the named symbol:
// files that import, call, or extend it, grouped by confidence (high/medium/low).
//
//	GET /api/v1/symbols/impact/{symbol}
func (s *Server) handleSymbolImpact(w http.ResponseWriter, r *http.Request) {
	if s.symbolStore == nil {
		jsonError(w, http.StatusServiceUnavailable, "symbol store not available")
		return
	}
	sym := r.PathValue("symbol")
	if !symbolNameRe.MatchString(sym) {
		jsonError(w, http.StatusBadRequest, "invalid symbol name")
		return
	}
	idx := s.symbolCache.get(s.symbolStore)
	report := symbol.ImpactQueryIndexed(sym, idx)
	jsonOK(w, report)
}

// handleSymbolSearch returns symbol names that contain the query string (case-insensitive).
// Useful for autocomplete / partial-symbol lookups.
//
//	GET /api/v1/symbols/search?q=<name>[&limit=N]
func (s *Server) handleSymbolSearch(w http.ResponseWriter, r *http.Request) {
	if s.symbolStore == nil {
		jsonError(w, http.StatusServiceUnavailable, "symbol store not available")
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		jsonError(w, http.StatusBadRequest, "query param q required")
		return
	}
	limit := defaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = min(n, maxLimit)
		}
	}
	idx := s.symbolCache.get(s.symbolStore)
	var matches []string
	qLower := strings.ToLower(q)
	for name := range idx {
		if strings.Contains(strings.ToLower(name), qLower) {
			matches = append(matches, name)
		}
	}
	sort.Strings(matches)
	truncated := len(matches) > limit
	if truncated {
		matches = matches[:limit]
	}
	jsonOK(w, map[string]any{"symbols": matches, "truncated": truncated})
}
