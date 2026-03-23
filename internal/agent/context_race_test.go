package agent

import (
	"context"
	"sync"
	"testing"
)

func TestContextBuilder_ConcurrentSetAndBuild(t *testing.T) {
	cb := NewContextBuilder(nil, nil, nil)

	var wg sync.WaitGroup
	const goroutines = 50

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				cb.SetGitRoot("/tmp")
			} else {
				cb.BuildCtx(context.Background(), "query", "model")
			}
		}()
	}

	wg.Wait()
}
