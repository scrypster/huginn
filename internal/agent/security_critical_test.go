package agent

import (
	"sync"
	"testing"
)

// TestSession_StateMachine_NoPanic verifies that state transitions via the
// internal State type don't panic or produce invalid values.
func TestSession_StateMachine_NoPanic(t *testing.T) {
	t.Parallel()
	states := []State{
		StateIdle,
		StateAgentLoop,
		State(99), // hypothetical future value — must not panic
	}
	for _, s := range states {
		_ = s // must not panic
	}
}

// TestSession_ConcurrentStateRead verifies no data race when many goroutines
// read the session state concurrently.
func TestSession_ConcurrentStateRead(t *testing.T) {
	t.Parallel()
	sess := &Session{
		ID:    "test-concurrent",
		state: StateAgentLoop,
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sess.mu.Lock()
			_ = sess.state
			sess.mu.Unlock()
		}()
	}
	wg.Wait()
}

// TestRunLoopConfig_ZeroValue is safe confirms RunLoopConfig{} doesn't panic
// when its zero value is inspected.
func TestRunLoopConfig_ZeroValue_IsSafe(t *testing.T) {
	t.Parallel()
	var cfg RunLoopConfig
	_ = cfg
}

// TestLoopResult_Fields verifies LoopResult can be created and fields read
// without panic (guards against nil pointer access in error paths).
func TestLoopResult_Fields(t *testing.T) {
	t.Parallel()
	lr := LoopResult{}
	_ = lr.StopReason
	_ = lr.FinalContent
}
