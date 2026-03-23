package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// defaultEmbeddingTimeout is the per-request timeout applied when Timeout == 0.
const defaultEmbeddingTimeout = 5 * time.Second

// OllamaEmbedder produces embeddings using Ollama's API.
type OllamaEmbedder struct {
	baseURL string
	model   string
	dim     int32 // atomic
	client  *http.Client
	// Timeout is the per-request deadline for embedding calls. When 0,
	// defaultEmbeddingTimeout (5s) is used. Set after construction.
	Timeout time.Duration
}

// NewOllamaEmbedder creates an embedder for the given Ollama base URL and model.
func NewOllamaEmbedder(baseURL, model string) *OllamaEmbedder {
	return &OllamaEmbedder{
		baseURL: baseURL,
		model:   model,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Embed produces a vector embedding for the given text.
func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	// Apply per-request timeout (caller context still takes precedence if shorter).
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = defaultEmbeddingTimeout
	}
	embedCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body, _ := json.Marshal(map[string]string{"model": e.model, "prompt": text})
	req, err := http.NewRequestWithContext(embedCtx, http.MethodPost, e.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed: status %d", resp.StatusCode)
	}

	var result struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if len(result.Embedding) > 0 {
		atomic.StoreInt32(&e.dim, int32(len(result.Embedding)))
	}

	return result.Embedding, nil
}

// Dimensions returns the dimensionality of the embeddings.
func (e *OllamaEmbedder) Dimensions() int {
	return int(atomic.LoadInt32(&e.dim))
}

// Probe checks if the Ollama server is reachable by attempting a single embedding.
func (e *OllamaEmbedder) Probe(ctx context.Context) error {
	_, err := e.Embed(ctx, "test")
	return err
}

var _ Embedder = (*OllamaEmbedder)(nil)
