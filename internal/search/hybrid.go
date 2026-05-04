package search

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/scrypster/huginn/internal/search/hnsw"
)

const rrfK = 60

// HybridSearcher combines BM25 (keyword) and semantic (vector) search using reciprocal rank fusion.
type HybridSearcher struct {
	bm25     *BM25Searcher
	hnswIdx  *hnsw.Index
	embedder Embedder
	chunks   map[uint64]Chunk
	mu       sync.RWMutex // guards runtime snapshots for concurrent Index/Search access

	embedFailures          atomic.Int64
	hnswInsertFailures     atomic.Int64
	queryEmbedFailures     atomic.Int64
	hnswSearchFailures     atomic.Int64
	bm25SearchFailures     atomic.Int64
	semanticFallbacks      atomic.Int64
	lastIndexTotalChunks   atomic.Int64
	lastIndexSemanticCount atomic.Int64
	lastIndexDurationMs    atomic.Int64
}

// NewHybridSearcher creates a hybrid searcher combining keyword and semantic approaches.
func NewHybridSearcher(bm25 *BM25Searcher, hnswIdx *hnsw.Index, embedder Embedder) *HybridSearcher {
	return &HybridSearcher{
		bm25:     bm25,
		hnswIdx:  hnswIdx,
		embedder: embedder,
		chunks:   make(map[uint64]Chunk),
	}
}

// Index indexes chunks for both BM25 and semantic search.
func (h *HybridSearcher) Index(ctx context.Context, chunks []Chunk) error {
	start := time.Now()
	if h.bm25 == nil {
		return fmt.Errorf("hybrid: bm25 searcher is not configured")
	}

	// Index in BM25
	if err := h.bm25.Index(ctx, chunks); err != nil {
		return err
	}

	newChunks := make(map[uint64]Chunk, len(chunks))
	for _, c := range chunks {
		newChunks[c.ID] = c
	}

	h.mu.RLock()
	embedder := h.embedder
	existingHNSW := h.hnswIdx
	h.mu.RUnlock()

	semanticIndexed := 0
	var newHNSW *hnsw.Index

	// Build a fresh HNSW snapshot, then atomically swap it in after success.
	if embedder != nil && existingHNSW != nil {
		newHNSW = hnsw.New(existingHNSW.M, existingHNSW.EfConstruct)
		for _, c := range chunks {
			vec, err := embedder.Embed(ctx, c.Content)
			if err != nil {
				h.embedFailures.Add(1)
				slog.Warn("hybrid: embedding failed for chunk, excluding from semantic index",
					"chunk_id", c.ID, "path", c.Path, "err", err)
				continue
			}
			if err := newHNSW.Insert(c.ID, vec); err != nil {
				h.hnswInsertFailures.Add(1)
				slog.Warn("hybrid: hnsw insert failed for chunk, excluding from semantic index",
					"chunk_id", c.ID, "path", c.Path, "err", err)
				continue
			}
			semanticIndexed++
		}
	}

	h.mu.Lock()
	h.chunks = newChunks
	if existingHNSW != nil {
		h.hnswIdx = newHNSW
	}
	h.mu.Unlock()

	h.lastIndexTotalChunks.Store(int64(len(chunks)))
	h.lastIndexSemanticCount.Store(int64(semanticIndexed))
	h.lastIndexDurationMs.Store(time.Since(start).Milliseconds())

	return nil
}

// Search performs hybrid search using reciprocal rank fusion (RRF).
// It combines BM25 results (keyword relevance) with HNSW results (semantic similarity).
func (h *HybridSearcher) Search(ctx context.Context, query string, n int) ([]Chunk, error) {
	if n <= 0 {
		return nil, nil
	}

	h.mu.RLock()
	bm25 := h.bm25
	hnswIdx := h.hnswIdx
	embedder := h.embedder
	chunks := h.chunks
	h.mu.RUnlock()
	if bm25 == nil {
		return nil, fmt.Errorf("hybrid: bm25 searcher is not configured")
	}

	scores := make(map[uint64]float64)

	// Get BM25 results.
	bm25Results, bm25Err := bm25.Search(ctx, query, n*2)
	if bm25Err != nil {
		h.bm25SearchFailures.Add(1)
		slog.Warn("hybrid: bm25 search failed", "err", bm25Err)
	} else {
		for rank, chunk := range bm25Results {
			scores[chunk.ID] += 1.0 / float64(rrfK+rank+1)
		}
	}

	semanticFailed := false
	semanticUsed := false

	// Get semantic results if embedder and HNSW are available.
	if embedder != nil && hnswIdx != nil {
		vec, err := embedder.Embed(ctx, query)
		if err != nil {
			semanticFailed = true
			h.queryEmbedFailures.Add(1)
			h.semanticFallbacks.Add(1)
			slog.Warn("hybrid: query embedding failed, returning BM25-only results", "err", err)
		} else {
			hnswIDs, hnswErr := hnswIdx.Search(vec, n*2)
			if hnswErr != nil {
				semanticFailed = true
				h.hnswSearchFailures.Add(1)
				h.semanticFallbacks.Add(1)
				slog.Warn("hybrid: hnsw search failed, returning BM25-only results", "err", hnswErr)
			}
			// Apply RRF to semantic results.
			for rank, id := range hnswIDs {
				scores[id] += 1.0 / float64(rrfK+rank+1)
				semanticUsed = true
			}
		}
	}

	if bm25Err != nil && (semanticFailed || !semanticUsed) {
		return nil, fmt.Errorf("hybrid: no viable search path: %w", bm25Err)
	}

	// Sort by combined score.
	type scored struct {
		id    uint64
		score float64
	}
	var sorted []scored
	for id, s := range scores {
		sorted = append(sorted, scored{id, s})
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].score > sorted[j].score
	})

	// Collect top-n results
	k := n
	if k > len(sorted) {
		k = len(sorted)
	}
	result := make([]Chunk, 0, k)
	for i := 0; i < k; i++ {
		if ch, ok := chunks[sorted[i].id]; ok {
			result = append(result, ch)
		}
	}

	return result, nil
}

// Close closes the hybrid searcher and underlying resources.
func (h *HybridSearcher) Close() error {
	return nil
}

// SearchHealth exposes hybrid-index telemetry for health endpoints.
func (h *HybridSearcher) SearchHealth() HealthSnapshot {
	return HealthSnapshot{
		EmbedFailures:          h.embedFailures.Load(),
		HNSWInsertFailures:     h.hnswInsertFailures.Load(),
		QueryEmbedFailures:     h.queryEmbedFailures.Load(),
		HNSWSearchFailures:     h.hnswSearchFailures.Load(),
		BM25SearchFailures:     h.bm25SearchFailures.Load(),
		SemanticFallbacks:      h.semanticFallbacks.Load(),
		LastIndexTotalChunks:   h.lastIndexTotalChunks.Load(),
		LastIndexSemanticCount: h.lastIndexSemanticCount.Load(),
		LastIndexDurationMs:    h.lastIndexDurationMs.Load(),
	}
}

var _ Searcher = (*HybridSearcher)(nil)
var _ HealthReporter = (*HybridSearcher)(nil)
