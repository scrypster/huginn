package search

import "context"

// Chunk represents a semantic unit of code or text.
type Chunk struct {
	ID        uint64
	Path      string
	Content   string
	StartLine int
}

// Searcher indexes chunks and retrieves relevant results for a query.
type Searcher interface {
	Index(ctx context.Context, chunks []Chunk) error
	Search(ctx context.Context, query string, n int) ([]Chunk, error)
	Close() error
}

// Embedder produces vector embeddings for text.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Dimensions() int
}
