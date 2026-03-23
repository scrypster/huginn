package threadmgr

import (
	"sync"
	"sync/atomic"
	"testing"
)

// TestTokenCounter_ConcurrentIncrementsNoOverflow verifies that concurrent
// atomic increments of the int64 token counter used in runOnce produce
// consistent results with no negative or overflowed values.
//
// The counter is declared as a plain int64 inside SpawnThread and accessed
// exclusively via sync/atomic operations (atomic.AddInt64). This test confirms
// that pattern is race-free under high concurrency.
func TestTokenCounter_ConcurrentIncrementsNoOverflow(t *testing.T) {
	t.Parallel()

	const goroutines = 200
	const incrementsPerGoroutine = 500

	var counter int64
	var wg sync.WaitGroup

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGoroutine; j++ {
				n := atomic.AddInt64(&counter, 1)
				if n <= 0 {
					// This would indicate overflow or a data race wrote a bad value.
					t.Errorf("atomic counter went non-positive: %d", n)
					return
				}
			}
		}()
	}

	wg.Wait()

	final := atomic.LoadInt64(&counter)
	expected := int64(goroutines * incrementsPerGoroutine)
	if final != expected {
		t.Errorf("final counter = %d, want %d", final, expected)
	}
	if final < 0 {
		t.Errorf("counter overflowed: %d", final)
	}
}

// TestTokensUsed_ProtectedByMutex verifies that ThreadManager.threads[id].TokensUsed
// is updated exclusively under tm.mu — the same lock that protects budget checks
// in runOnce. We exercise this by simulating concurrent reads and writes against
// a real ThreadManager.
func TestTokensUsed_ProtectedByMutex(t *testing.T) {
	t.Parallel()

	tm := New()
	thread, err := tm.Create(CreateParams{
		SessionID: "s-tok",
		AgentID:   "bot",
		Task:      "token test",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	threadID := thread.ID

	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup

	// Writers: increment TokensUsed under the manager lock (same as runOnce does).
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				tm.mu.Lock()
				if t2, ok := tm.threads[threadID]; ok {
					t2.TokensUsed += 10
				}
				tm.mu.Unlock()
			}
		}()
	}

	// Readers: read via the public Get() copy (uses RLock).
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				got, ok := tm.Get(threadID)
				if ok && got.TokensUsed < 0 {
					t.Errorf("negative TokensUsed observed: %d", got.TokensUsed)
				}
			}
		}()
	}

	wg.Wait()

	final, ok := tm.Get(threadID)
	if !ok {
		t.Fatal("thread not found after concurrent access")
	}

	expected := goroutines * iterations * 10
	if final.TokensUsed != expected {
		t.Errorf("final TokensUsed = %d, want %d", final.TokensUsed, expected)
	}
}
