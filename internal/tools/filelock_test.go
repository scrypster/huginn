package tools_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/tools"
)

func TestFileLockManager_SerializesWrites(t *testing.T) {
	flm := tools.NewFileLockManager()
	const path = "testfile.go"
	const goroutines = 8
	var inProgress int64
	var maxConcurrent int64
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			flm.Lock(path)
			defer flm.Unlock(path)
			cur := atomic.AddInt64(&inProgress, 1)
			for {
				old := atomic.LoadInt64(&maxConcurrent)
				if cur <= old {
					break
				}
				if atomic.CompareAndSwapInt64(&maxConcurrent, old, cur) {
					break
				}
			}
			time.Sleep(2 * time.Millisecond)
			atomic.AddInt64(&inProgress, -1)
		}()
	}
	wg.Wait()
	if maxConcurrent > 1 {
		t.Errorf("expected max concurrent=1, got %d", maxConcurrent)
	}
}

func TestFileLockManager_AllowsConcurrentDifferentPaths(t *testing.T) {
	flm := tools.NewFileLockManager()
	var wg sync.WaitGroup
	for _, path := range []string{"a.go", "b.go"} {
		path := path
		wg.Add(1)
		go func() {
			defer wg.Done()
			flm.Lock(path)
			time.Sleep(10 * time.Millisecond)
			flm.Unlock(path)
		}()
	}
	wg.Wait() // must complete without deadlock
}

func TestFileLockManager_LockUnlock_NoDeadlock(t *testing.T) {
	flm := tools.NewFileLockManager()
	flm.Lock("exists.go")
	flm.Unlock("exists.go") // must not deadlock
}
