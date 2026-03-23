package hnsw

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"
)

type node struct {
	ID     uint64
	Vector []float32
	Layers [][]uint64
}

// Index is a Hierarchical Navigable Small World (HNSW) index for approximate nearest neighbor search.
type Index struct {
	mu          sync.RWMutex
	nodes       map[uint64]*node
	entryPoint  uint64
	maxLayer    int
	M           int
	Mmax0       int
	EfConstruct int
	ml          float64
	dim         int
}

// New creates a new HNSW index with parameters M (graph connectivity) and efConstruct (construction complexity).
func New(M, efConstruct int) *Index {
	if M <= 0 {
		M = 16
	}
	if efConstruct <= 0 {
		efConstruct = 200
	}
	return &Index{
		nodes:       make(map[uint64]*node),
		M:           M,
		Mmax0:       M * 2,
		EfConstruct: efConstruct,
		ml:          1.0 / math.Log(float64(M)),
	}
}

// Insert adds a vector to the index.
func (idx *Index) Insert(id uint64, vector []float32) error {
	if len(vector) == 0 {
		return fmt.Errorf("hnsw: cannot insert zero-length vector")
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	if idx.dim == 0 {
		idx.dim = len(vector)
	} else if len(vector) != idx.dim {
		return fmt.Errorf("hnsw: dimension mismatch: got %d, want %d", len(vector), idx.dim)
	}

	level := int(math.Floor(-math.Log(rand.Float64()) * idx.ml))
	n := &node{ID: id, Vector: vector, Layers: make([][]uint64, level+1)}
	idx.nodes[id] = n

	if len(idx.nodes) == 1 {
		idx.entryPoint = id
		idx.maxLayer = level
		return nil
	}

	// Greedy search from top layer down to level+1
	ep := idx.entryPoint
	for lc := idx.maxLayer; lc > level; lc-- {
		candidates := idx.searchLayer(vector, []uint64{ep}, 1, lc)
		if len(candidates) > 0 {
			ep = candidates[0]
		}
	}

	// Insert at all layers from level down to 0
	for lc := min(level, idx.maxLayer); lc >= 0; lc-- {
		Mmax := idx.M
		if lc == 0 {
			Mmax = idx.Mmax0
		}

		candidates := idx.searchLayer(vector, []uint64{ep}, idx.EfConstruct, lc)
		// Select top-M neighbors
		neighbors := candidates
		if len(neighbors) > Mmax {
			neighbors = neighbors[:Mmax]
		}

		n.Layers[lc] = neighbors

		// Add bidirectional edges
		for _, nb := range neighbors {
			nbNode := idx.nodes[nb]
			for len(nbNode.Layers) <= lc {
				nbNode.Layers = append(nbNode.Layers, nil)
			}
			nbNode.Layers[lc] = append(nbNode.Layers[lc], id)
			// Shrink if over limit
			if len(nbNode.Layers[lc]) > Mmax {
				nbNode.Layers[lc] = nbNode.Layers[lc][:Mmax]
			}
		}

		if len(candidates) > 0 {
			ep = candidates[0]
		}
	}

	if level > idx.maxLayer {
		idx.maxLayer = level
		idx.entryPoint = id
	}

	return nil
}

// Search returns the k nearest neighbors to the query vector.
func (idx *Index) Search(query []float32, k int) ([]uint64, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if k <= 0 || len(idx.nodes) == 0 {
		return nil, nil
	}

	ep := idx.entryPoint

	// Greedy search from top layer down to layer 1
	for lc := idx.maxLayer; lc > 0; lc-- {
		candidates := idx.searchLayer(query, []uint64{ep}, 1, lc)
		if len(candidates) > 0 {
			ep = candidates[0]
		}
	}

	// Search at layer 0
	ef := k * 2
	if ef < 50 {
		ef = 50
	}
	candidates := idx.searchLayer(query, []uint64{ep}, ef, 0)
	if len(candidates) > k {
		candidates = candidates[:k]
	}
	return candidates, nil
}

// searchLayer returns IDs sorted by ascending distance to query.
func (idx *Index) searchLayer(query []float32, eps []uint64, ef, layer int) []uint64 {
	type dist struct {
		id uint64
		d  float32
	}

	visited := make(map[uint64]bool)
	var candidates []dist

	for _, ep := range eps {
		if !visited[ep] {
			visited[ep] = true
			n := idx.nodes[ep]
			if n != nil {
				candidates = append(candidates, dist{ep, CosineDistance(query, n.Vector)})
			}
		}
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].d < candidates[j].d })

	W := make([]dist, len(candidates))
	copy(W, candidates)

	for len(candidates) > 0 {
		c := candidates[0]
		candidates = candidates[1:]

		if len(W) >= ef && c.d > W[len(W)-1].d {
			break
		}

		cn := idx.nodes[c.id]
		if cn == nil || layer >= len(cn.Layers) {
			continue
		}

		for _, nb := range cn.Layers[layer] {
			if visited[nb] {
				continue
			}
			visited[nb] = true

			nbNode := idx.nodes[nb]
			if nbNode == nil {
				continue
			}

			d := CosineDistance(query, nbNode.Vector)
			if len(W) < ef || d < W[len(W)-1].d {
				candidates = append(candidates, dist{nb, d})
				W = append(W, dist{nb, d})
				sort.Slice(W, func(i, j int) bool { return W[i].d < W[j].d })
				if len(W) > ef {
					W = W[:ef]
				}
				sort.Slice(candidates, func(i, j int) bool { return candidates[i].d < candidates[j].d })
			}
		}
	}

	ids := make([]uint64, len(W))
	for i, w := range W {
		ids[i] = w.id
	}
	return ids
}

// CosineDistance computes 1 - cosine_similarity between two vectors.
func CosineDistance(a, b []float32) float32 {
	if len(a) != len(b) {
		return 1
	}

	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 1
	}

	return 1 - dot/float32(math.Sqrt(float64(normA))*math.Sqrt(float64(normB)))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
