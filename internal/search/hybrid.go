package search

import (
	"context"
	"log/slog"
	"sort"

	"github.com/scrypster/huginn/internal/search/hnsw"
)

const rrfK = 60

// HybridSearcher combines BM25 (keyword) and semantic (vector) search using reciprocal rank fusion.
type HybridSearcher struct {
	bm25     *BM25Searcher
	hnswIdx  *hnsw.Index
	embedder Embedder
	chunks   map[uint64]Chunk
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
	// Store chunks for lookup
	for _, c := range chunks {
		h.chunks[c.ID] = c
	}

	// Index in BM25
	if err := h.bm25.Index(ctx, chunks); err != nil {
		return err
	}

	// Index vectors in HNSW if embedder is available
	if h.embedder != nil && h.hnswIdx != nil {
		var embeddingFailures int
		for _, c := range chunks {
			vec, err := h.embedder.Embed(ctx, c.Content)
			if err != nil {
				embeddingFailures++
				if embeddingFailures == 1 {
					slog.Warn("hybrid: embedding failed for chunk, excluding from semantic index",
						"chunk_id", c.ID, "path", c.Path, "err", err)
				}
				continue
			}
			_ = h.hnswIdx.Insert(c.ID, vec)
		}
		if embeddingFailures > 1 {
			slog.Warn("hybrid: embedding failures during index build",
				"failed", embeddingFailures, "total", len(chunks))
		}
	}

	return nil
}

// Search performs hybrid search using reciprocal rank fusion (RRF).
// It combines BM25 results (keyword relevance) with HNSW results (semantic similarity).
func (h *HybridSearcher) Search(ctx context.Context, query string, n int) ([]Chunk, error) {
	if n <= 0 {
		return nil, nil
	}

	// Get BM25 results
	bm25Results, _ := h.bm25.Search(ctx, query, n*2)

	scores := make(map[uint64]float64)

	// Apply RRF to BM25 results
	for rank, chunk := range bm25Results {
		scores[chunk.ID] += 1.0 / float64(rrfK+rank+1)
	}

	// Get semantic results if embedder and HNSW are available
	if h.embedder != nil && h.hnswIdx != nil {
		vec, err := h.embedder.Embed(ctx, query)
		if err != nil {
			slog.Warn("hybrid: query embedding failed, returning BM25-only results", "err", err)
		} else {
			hnswIDs, _ := h.hnswIdx.Search(vec, n*2)
			// Apply RRF to semantic results
			for rank, id := range hnswIDs {
				scores[id] += 1.0 / float64(rrfK+rank+1)
			}
		}
	}

	// Sort by combined score
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
		if ch, ok := h.chunks[sorted[i].id]; ok {
			result = append(result, ch)
		}
	}

	return result, nil
}

// Close closes the hybrid searcher and underlying resources.
func (h *HybridSearcher) Close() error {
	return nil
}

var _ Searcher = (*HybridSearcher)(nil)
