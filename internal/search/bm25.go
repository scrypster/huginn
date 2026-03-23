package search

import (
	"context"
	"math"
	"sort"
	"strings"
)

const bm25K1 = 1.5
const bm25B = 0.75

// BM25Searcher implements keyword-based search using the BM25 ranking function.
type BM25Searcher struct {
	chunks []Chunk
}

// NewBM25Searcher creates a new BM25 searcher.
func NewBM25Searcher() *BM25Searcher {
	return &BM25Searcher{}
}

// Index stores the chunks for searching.
func (s *BM25Searcher) Index(_ context.Context, chunks []Chunk) error {
	s.chunks = chunks
	return nil
}

// Search returns chunks ranked by BM25 relevance.
func (s *BM25Searcher) Search(_ context.Context, query string, n int) ([]Chunk, error) {
	if n <= 0 || len(s.chunks) == 0 {
		return nil, nil
	}

	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return s.chunks[:min2(n, len(s.chunks))], nil
	}

	// Compute term frequencies per doc and document frequencies
	type tf map[string]float64
	tfs := make([]tf, len(s.chunks))
	docLengths := make([]float64, len(s.chunks))
	df := make(map[string]float64)

	for i, ch := range s.chunks {
		words := strings.Fields(strings.ToLower(ch.Content + " " + ch.Path))
		tfs[i] = make(tf)
		for _, w := range words {
			tfs[i][w]++
			docLengths[i]++
		}
		seen := make(map[string]bool)
		for _, w := range words {
			if !seen[w] {
				df[w]++
				seen[w] = true
			}
		}
	}

	totalLen := 0.0
	for _, l := range docLengths {
		totalLen += l
	}
	avgdl := totalLen / float64(len(s.chunks))
	N := float64(len(s.chunks))

	type scored struct {
		idx   int
		score float64
	}
	scores := make([]scored, len(s.chunks))

	for i := range s.chunks {
		for _, qt := range terms {
			tfVal := tfs[i][qt]
			dfVal := df[qt]
			if dfVal == 0 {
				continue
			}

			// BM25 IDF component
			idf := math.Log((N-dfVal+0.5)/(dfVal+0.5) + 1)

			// BM25 TF component
			docLen := docLengths[i]
			num := tfVal * (bm25K1 + 1)
			den := tfVal + bm25K1*(1-bm25B+bm25B*(docLen/avgdl))
			scores[i].score += idf * (num / den)
		}
		scores[i].idx = i
	}

	// Sort by score descending — O(n log n) via pdqsort.
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	k := n
	if k > len(scores) {
		k = len(scores)
	}
	result := make([]Chunk, k)
	for i := 0; i < k; i++ {
		result[i] = s.chunks[scores[i].idx]
	}
	return result, nil
}

// Close is a no-op for BM25Searcher.
func (s *BM25Searcher) Close() error {
	return nil
}

var _ Searcher = (*BM25Searcher)(nil)

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}
