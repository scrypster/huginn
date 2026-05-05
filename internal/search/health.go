package search

// HealthSnapshot captures semantic-search degradation and indexing telemetry.
type HealthSnapshot struct {
	EmbedFailures          int64 `json:"embed_failures"`
	HNSWInsertFailures     int64 `json:"hnsw_insert_failures"`
	QueryEmbedFailures     int64 `json:"query_embed_failures"`
	HNSWSearchFailures     int64 `json:"hnsw_search_failures"`
	BM25SearchFailures     int64 `json:"bm25_search_failures"`
	SemanticFallbacks      int64 `json:"semantic_fallbacks"`
	LastIndexTotalChunks   int64 `json:"last_index_total_chunks"`
	LastIndexSemanticCount int64 `json:"last_index_semantic_count"`
	LastIndexDurationMs    int64 `json:"last_index_duration_ms"`
}

// HealthReporter is implemented by searchers that expose health counters.
type HealthReporter interface {
	SearchHealth() HealthSnapshot
}
