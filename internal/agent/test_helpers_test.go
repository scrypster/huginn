package agent

import (
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/compact"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/repo"
	"github.com/scrypster/huginn/internal/stats"
)

// mustNewOrchestrator wraps NewOrchestrator for tests, calling t.Fatal on error.
func mustNewOrchestrator(t testing.TB, b backend.Backend, models *modelconfig.Models, idx *repo.Index, registry *modelconfig.ModelRegistry, sc stats.Collector, compactor *compact.Compactor) *Orchestrator {
	t.Helper()
	o, err := NewOrchestrator(b, models, idx, registry, sc, compactor)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	return o
}

// mustNewSession wraps NewSession for tests, calling t.Fatal on error.
func mustNewSession(t testing.TB, o *Orchestrator, id string) *Session {
	t.Helper()
	sess, err := o.NewSession(id)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	return sess
}
