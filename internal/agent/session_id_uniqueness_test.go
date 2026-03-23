package agent

import (
	"sync"
	"testing"
)

func TestGenerateSessionID_Uniqueness(t *testing.T) {
	const goroutines = 10
	const perGoroutine = 1000
	const total = goroutines * perGoroutine

	ids := make(map[string]struct{}, total)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]string, perGoroutine)
			for j := 0; j < perGoroutine; j++ {
				id, err := generateSessionID()
				if err != nil {
					t.Errorf("generateSessionID() error: %v", err)
					return
				}
				local[j] = id
			}
			mu.Lock()
			for _, id := range local {
				ids[id] = struct{}{}
			}
			mu.Unlock()
		}()
	}

	wg.Wait()

	if len(ids) != total {
		t.Errorf("expected %d unique IDs, got %d (duplicates detected)", total, len(ids))
	}
}
