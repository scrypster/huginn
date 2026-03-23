package repo

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
)

// BM25 constants (Robertson-Sparck Jones variant)
const (
	bm25K1 = 1.5  // term frequency saturation parameter
	bm25B  = 0.75 // length normalization parameter
)

// BuildContext returns a formatted prompt prefix of the top relevant chunks
// for the given query, capped at maxBytes.
func (idx *Index) BuildContext(query string, maxBytes int) string {
	if len(idx.Chunks) == 0 {
		return ""
	}

	scored := scoreChunks(idx.Chunks, query)
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	var sb strings.Builder
	sb.WriteString("## Repository Context\n\n")
	remaining := maxBytes - sb.Len()

	for _, sc := range scored {
		block := fmt.Sprintf("### %s (line %d)\n```\n%s\n```\n\n",
			sc.chunk.Path, sc.chunk.StartLine, sc.chunk.Content)
		if len(block) > remaining {
			break
		}
		sb.WriteString(block)
		remaining -= len(block)
	}
	return sb.String()
}

type scoredChunk struct {
	chunk FileChunk
	score float64
}

// scoreChunks ranks chunks by BM25 relevance to the query.
func scoreChunks(chunks []FileChunk, query string) []scoredChunk {
	queryTerms := tokenize(query)
	if len(queryTerms) == 0 {
		result := make([]scoredChunk, len(chunks))
		for i, ch := range chunks {
			result[i] = scoredChunk{chunk: ch, score: 1}
		}
		return result
	}

	n := float64(len(chunks))

	// First pass: compute document frequencies and document lengths
	df := make(map[string]float64)
	docLengths := make(map[int]float64) // map from chunk index to length
	var totalLength float64

	for i, ch := range chunks {
		doc := tokenize(ch.Path + " " + ch.Content)
		docLengths[i] = float64(len(doc))
		totalLength += float64(len(doc))

		seen := make(map[string]bool)
		for _, t := range doc {
			if !seen[t] {
				df[t]++
				seen[t] = true
			}
		}
	}

	// Compute average document length
	avgdl := totalLength / n

	// Second pass: apply BM25 formula
	result := make([]scoredChunk, 0, len(chunks))
	for i, ch := range chunks {
		doc := tokenize(ch.Path + " " + ch.Content)
		tf := make(map[string]float64)
		for _, t := range doc {
			tf[t]++
		}

		var score float64
		docLen := docLengths[i]

		for _, qt := range queryTerms {
			if tf[qt] == 0 {
				continue
			}

			// BM25 IDF: log( (N - df(t) + 0.5) / (df(t) + 0.5) + 1 )
			idf := math.Log((n - df[qt] + 0.5) / (df[qt] + 0.5) + 1)

			// BM25 scoring component:
			// score += IDF(t) * [ tf(t,D) * (k1+1) ] / [ tf(t,D) + k1 * (1 - b + b * |D|/avgdl) ]
			numerator := tf[qt] * (bm25K1 + 1)
			denominator := tf[qt] + bm25K1*(1-bm25B+bm25B*(docLen/avgdl))
			score += idf * (numerator / denominator)
		}

		// Path boost: if any query term is in the path, multiply by 2
		pathLower := strings.ToLower(ch.Path)
		for _, qt := range queryTerms {
			if strings.Contains(pathLower, qt) {
				score *= 2
				break
			}
		}

		result = append(result, scoredChunk{chunk: ch, score: score})
	}
	return result
}

func tokenize(s string) []string {
	s = strings.ToLower(s)
	var tokens []string
	var cur strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
		} else {
			if cur.Len() > 1 {
				tokens = append(tokens, cur.String())
			}
			cur.Reset()
		}
	}
	if cur.Len() > 1 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

// BuildTree returns a compact directory-tree string from the index.
func (idx *Index) BuildTree() string {
	var sb strings.Builder
	sb.WriteString("## Repository Structure\n```\n")

	seen := make(map[string]bool)
	for _, ch := range idx.Chunks {
		parts := strings.SplitN(ch.Path, "/", 2)
		top := parts[0]
		if !seen[top] {
			seen[top] = true
			if len(parts) > 1 || filepath.Ext(top) == "" {
				sb.WriteString(top + "/\n")
			} else {
				sb.WriteString(top + "\n")
			}
		}
	}
	sb.WriteString("```\n\n")
	return sb.String()
}
