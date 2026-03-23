package swarm_test

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/swarm"
)

// TestSwarmMemoryPressure verifies that running 100 agents, each streaming
// more than 512 KB of output, does not exceed the expected per-agent ring
// buffer cap. The ring buffer silently overwrites oldest bytes, so total
// heap growth attributable to agent output must stay below 100 * 512 KB.
func TestSwarmMemoryPressure(t *testing.T) {
	const (
		numAgents     = 100
		ringCap       = 512 * 1024              // must mirror outputRingCap in output_ring.go
		emitPerAgent  = 2 * ringCap             // 2× the cap to force wrap-around
		maxTotalBytes = numAgents * ringCap * 2 // generous headroom (×2) for Go runtime overhead
	)

	// Build a payload chunk that each agent will emit repeatedly until
	// it has written emitPerAgent bytes in total.
	chunk := strings.Repeat("x", 4096) // 4 KB per emit call

	tasks := make([]swarm.SwarmTask, numAgents)
	for i := range tasks {
		tasks[i] = swarm.SwarmTask{
			ID:   fmt.Sprintf("mem-agent-%d", i),
			Name: fmt.Sprintf("MemAgent%d", i),
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				written := 0
				for written < emitPerAgent {
					select {
					case <-ctx.Done():
						return ctx.Err()
					default:
					}
					emit(swarm.SwarmEvent{Type: swarm.EventToken, Payload: chunk})
					written += len(chunk)
				}
				return nil
			},
		}
	}

	s := swarm.NewSwarm(32)

	// Drain events concurrently so the swarm is not blocked by the channel.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range s.Events() {
		}
	}()

	// Snapshot heap allocations before the run.
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	if _, _, _, _, err := s.Run(context.Background(), tasks); err != nil {
		t.Fatalf("swarm.Run: %v", err)
	}
	<-done

	// Snapshot after and force a GC so live-heap reflects only retained data.
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	// TotalAlloc tracks all allocations (cumulative); use HeapInuse for
	// the live working set after GC.
	heapGrowth := int64(after.HeapInuse) - int64(before.HeapInuse)
	t.Logf("HeapInuse before=%d after=%d growth=%d limit=%d",
		before.HeapInuse, after.HeapInuse, heapGrowth, maxTotalBytes)

	if heapGrowth > int64(maxTotalBytes) {
		t.Errorf("heap growth %d exceeds limit %d — ring buffer may not be capping output",
			heapGrowth, maxTotalBytes)
	}
}
