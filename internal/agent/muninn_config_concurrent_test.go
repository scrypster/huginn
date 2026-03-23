// Package agent — hardening iteration 4 tests.
// Focus: SetMuninnConfigPath thread-safety and concurrent AgentChat.
package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/modelconfig"
)

// TestOrchestrator_SetMuninnConfigPath_ThreadSafe spawns 10 goroutines that each
// call SetMuninnConfigPath and AgentChat concurrently. The test verifies there are
// no data races under -race and no panics.
//
// AgentChat falls back to plain Chat when toolRegistry is nil, which avoids
// needing a fake MuninnDB server — the mock backend responds immediately.
func TestOrchestrator_SetMuninnConfigPath_ThreadSafe(t *testing.T) {
	// Provide enough responses for all concurrent Chat calls (10 goroutines × 1 call each).
	mb := newMockBackend("ok") // mockBackend returns "ok" for all beyond the response list
	models := &modelconfig.Models{
		Reasoner: "test-model",
	}

	o := mustNewOrchestrator(t, mb, models, nil, nil, nil, nil)

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			// Interleave SetMuninnConfigPath with AgentChat to exercise the lock path.
			o.SetMuninnConfigPath("/fake/path")
			_ = o.AgentChat(
				context.Background(),
				"hello",
				5,
				nil, // onToken
				nil, // onToolCall
				nil, // onToolDone
				nil, // onPermDenied
				nil, // onBeforeWrite
				nil, // onEvent
			)
		}()
	}
	wg.Wait()
}
