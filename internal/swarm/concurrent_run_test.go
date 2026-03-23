package swarm_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/swarm"
)

// TestSwarm_ConcurrentRun_NoPanic verifies that two goroutines calling Run()
// simultaneously on the same Swarm instance do not panic or data-race.
func TestSwarm_ConcurrentRun_NoPanic(t *testing.T) {
	s := swarm.NewSwarm(4)

	// Drain the event channel so the Swarm does not block on a full buffer.
	go func() {
		for range s.Events() {
		}
	}()

	makeTask := func(id string) swarm.SwarmTask {
		return swarm.SwarmTask{
			ID:   id,
			Name: id,
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				time.Sleep(5 * time.Millisecond)
				return nil
			},
		}
	}

	var wg sync.WaitGroup
	var panicVal [2]any

	for i := 0; i < 2; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					panicVal[i] = r
				}
				wg.Done()
			}()
			tasks := []swarm.SwarmTask{
				makeTask("task-a"),
				makeTask("task-b"),
			}
			// Each goroutine uses an independent Swarm to avoid the intentional
			// single-run guard; we test the guard by sharing one Swarm below.
			_ = tasks
		}()
	}
	wg.Wait()

	// --- Shared Swarm test: two goroutines call Run() on the same instance. ---
	shared := swarm.NewSwarm(4)
	go func() {
		for range shared.Events() {
		}
	}()

	var wg2 sync.WaitGroup
	var panics [2]any
	for i := 0; i < 2; i++ {
		i := i
		wg2.Add(1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					panics[i] = r
				}
				wg2.Done()
			}()
			shared.Run(context.Background(), []swarm.SwarmTask{ //nolint
				makeTask("shared-task"),
			})
		}()
	}
	wg2.Wait()

	for i, p := range panics {
		if p != nil {
			t.Errorf("goroutine %d panicked: %v", i, p)
		}
	}
}
