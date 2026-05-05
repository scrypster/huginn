package search

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
)

const bm25K1 = 1.5
const bm25B = 0.75

// BM25Searcher implements keyword-based search using the BM25 ranking function.
type BM25Searcher struct {
	mu       sync.RWMutex
	docs     []bm25Document
	postings map[string]map[int]float64
	idf      map[string]float64
	avgDocLn float64
}

type bm25Document struct {
	chunk  Chunk
	length float64
}

// NewBM25Searcher creates a new BM25 searcher.
func NewBM25Searcher() *BM25Searcher {
	return &BM25Searcher{
		postings: make(map[string]map[int]float64),
		idf:      make(map[string]float64),
	}
}

// Index builds a snapshot BM25 index for the provided chunks.
func (s *BM25Searcher) Index(_ context.Context, chunks []Chunk) error {
	docs := make([]bm25Document, len(chunks))
	postings := make(map[string]map[int]float64)
	df := make(map[string]float64)

	totalLen := 0.0
	for i, ch := range chunks {
		terms := tokenizeBM25(ch.Content + " " + ch.Path)
		tf := make(map[string]float64)
		for _, term := range terms {
			tf[term]++
			totalLen++
		}
		docLen := float64(len(terms))
		docs[i] = bm25Document{
			chunk:  ch,
			length: docLen,
		}

		for term, freq := range tf {
			if postings[term] == nil {
				postings[term] = make(map[int]float64)
			}
			postings[term][i] = freq
			df[term]++
		}
	}

	idf := make(map[string]float64, len(df))
	N := float64(len(docs))
	for term, docFreq := range df {
		idf[term] = math.Log((N-docFreq+0.5)/(docFreq+0.5) + 1)
	}

	avgDocLen := 0.0
	if len(docs) > 0 {
		avgDocLen = totalLen / float64(len(docs))
	}

	s.mu.Lock()
	s.docs = docs
	s.postings = postings
	s.idf = idf
	s.avgDocLn = avgDocLen
	s.mu.Unlock()

	return nil
}

// Search returns chunks ranked by BM25 relevance.
func (s *BM25Searcher) Search(_ context.Context, query string, n int) ([]Chunk, error) {
	if n <= 0 {
		return nil, nil
	}
	s.mu.RLock()
	docs := s.docs
	postings := s.postings
	idf := s.idf
	avgDocLen := s.avgDocLn
	s.mu.RUnlock()
	if len(docs) == 0 {
		return nil, nil
	}

	terms := tokenizeBM25(query)
	if len(terms) == 0 {
		k := min2(n, len(docs))
		result := make([]Chunk, k)
		for i := 0; i < k; i++ {
			result[i] = docs[i].chunk
		}
		return result, nil
	}

	type scored struct {
		idx   int
		score float64
	}
	scoresByDoc := make(map[int]float64)
	for _, term := range terms {
		termIDF, ok := idf[term]
		if !ok {
			continue
		}
		posting := postings[term]
		for docIdx, tfVal := range posting {
			docLen := docs[docIdx].length
			if docLen == 0 || avgDocLen == 0 {
				continue
			}
			num := tfVal * (bm25K1 + 1)
			den := tfVal + bm25K1*(1-bm25B+bm25B*(docLen/avgDocLen))
			scoresByDoc[docIdx] += termIDF * (num / den)
		}
	}

	if len(scoresByDoc) == 0 {
		k := min2(n, len(docs))
		result := make([]Chunk, k)
		for i := 0; i < k; i++ {
			result[i] = docs[i].chunk
		}
		return result, nil
	}

	scores := make([]scored, 0, len(scoresByDoc))
	for idx, score := range scoresByDoc {
		scores = append(scores, scored{idx: idx, score: score})
	}
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	k := n
	if k > len(scores) {
		k = len(scores)
	}
	result := make([]Chunk, k)
	for i := 0; i < k; i++ {
		result[i] = docs[scores[i].idx].chunk
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

func tokenizeBM25(text string) []string {
	return strings.Fields(strings.ToLower(text))
}
