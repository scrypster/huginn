package memory

import "context"

// MemoryRecord is a single recalled memory item.
type MemoryRecord struct {
	Content     string
	Tags        []string
	Score       float64
	SourceVault string
}

// MemoryBackend abstracts over MuninnDB and the Pebble fallback.
// Implementations must be safe for concurrent use.
type MemoryBackend interface {
	// Available reports whether this backend is configured and reachable.
	Available() bool

	// Write stores a memory in the given vault.
	Write(ctx context.Context, vault, key, content string, tags []string) error

	// Recall retrieves the top-k memories most relevant to queryParts from vault.
	Recall(ctx context.Context, vault string, queryParts []string, topK int) ([]MemoryRecord, error)

	// EnsureVault creates the vault if it does not already exist.
	// Implementations that don't require explicit creation may return nil immediately.
	EnsureVault(ctx context.Context, vault string) error
}
