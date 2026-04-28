package agent_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/stats"
)

func TestSetMemoryReplicatorNilSafe(t *testing.T) {
	b := &slowBackend{}
	models := modelconfig.DefaultModels()
	o, err := agent.NewOrchestrator(b, models, nil, nil, stats.NoopCollector{}, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	// Should not panic when nil is passed
	o.SetMemoryReplicator(nil)
}
