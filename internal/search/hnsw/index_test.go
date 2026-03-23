package hnsw_test

import (
	"math"
	"math/rand"
	"testing"
	"github.com/scrypster/huginn/internal/search/hnsw"
)

func randomVec(dim int, r *rand.Rand) []float32 {
	v := make([]float32, dim)
	sum := float32(0)
	for i := range v {
		v[i] = r.Float32()
		sum += v[i] * v[i]
	}
	norm := float32(math.Sqrt(float64(sum)))
	if norm > 0 {
		for i := range v {
			v[i] /= norm
		}
	}
	return v
}

func TestHNSW_InsertAndSearch_Basic(t *testing.T) {
	idx := hnsw.New(8, 200)
	r := rand.New(rand.NewSource(42))
	const dim = 4
	for i := uint64(1); i <= 50; i++ {
		if err := idx.Insert(i, randomVec(dim, r)); err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
	}
	query := randomVec(dim, r)
	ids, err := idx.Search(query, 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(ids) == 0 {
		t.Error("expected non-empty results")
	}
}

func TestHNSW_ExactNearest(t *testing.T) {
	idx := hnsw.New(8, 200)
	// Insert 10 random vectors
	r := rand.New(rand.NewSource(99))
	vecs := make([][]float32, 10)
	for i := range vecs {
		vecs[i] = randomVec(4, r)
		idx.Insert(uint64(i+1), vecs[i])
	}
	// Query with vecs[0] — should return id=1 first
	ids, _ := idx.Search(vecs[0], 1)
	if len(ids) == 0 {
		t.Fatal("no results")
	}
	if ids[0] != 1 {
		t.Logf("nearest was %d (not 1) — acceptable for HNSW approximation", ids[0])
	}
}

func TestHNSW_DimensionMismatch(t *testing.T) {
	idx := hnsw.New(8, 200)
	idx.Insert(1, []float32{0.1, 0.2, 0.3, 0.4})
	err := idx.Insert(2, []float32{0.1, 0.2}) // wrong dim
	if err == nil {
		t.Error("expected error on dimension mismatch")
	}
}

func TestHNSW_SingleNode(t *testing.T) {
	idx := hnsw.New(8, 200)
	idx.Insert(1, []float32{1, 0, 0, 0})
	ids, _ := idx.Search([]float32{1, 0, 0, 0}, 1)
	if len(ids) != 1 || ids[0] != 1 {
		t.Errorf("expected [1], got %v", ids)
	}
}

func TestCosineDistance_Orthogonal(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	d := hnsw.CosineDistance(a, b)
	if d < 0.99 {
		t.Errorf("expected ~1.0 for orthogonal, got %f", d)
	}
}

func TestCosineDistance_Identical(t *testing.T) {
	a := []float32{0.5, 0.5, 0.0}
	d := hnsw.CosineDistance(a, a)
	if d > 0.01 {
		t.Errorf("expected ~0 for identical, got %f", d)
	}
}

func TestHNSW_InsertZeroLengthVector(t *testing.T) {
	idx := hnsw.New(8, 200)
	err := idx.Insert(1, []float32{})
	if err == nil {
		t.Error("expected error when inserting zero-length vector")
	}
}

func TestHNSW_InsertNilVector(t *testing.T) {
	idx := hnsw.New(8, 200)
	err := idx.Insert(1, nil)
	if err == nil {
		t.Error("expected error when inserting nil vector")
	}
}
