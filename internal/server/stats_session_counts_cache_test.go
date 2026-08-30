package server

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
)

// countingManifestLoader wraps a fixed manifest set and counts how many
// times it was invoked, so tests can assert the TTL cache actually skips
// the underlying load rather than merely returning the right numbers.
func countingManifestLoader(manifests []session.Manifest, calls *int32) func() ([]session.Manifest, error) {
	return func() ([]session.Manifest, error) {
		atomic.AddInt32(calls, 1)
		return manifests, nil
	}
}

func TestComputeSessionCounts_TTLCache_SkipsReloadWithinWindow(t *testing.T) {
	srv := testServer(t)
	var calls int32
	srv.sessionCountsLoader = countingManifestLoader([]session.Manifest{
		{SessionID: "a", UpdatedAt: time.Now().UTC()},
		{SessionID: "b", UpdatedAt: time.Now().UTC().Add(-time.Hour)},
	}, &calls)

	total1, active1 := srv.computeSessionCounts()
	if total1 != 2 || active1 != 1 {
		t.Fatalf("first call: total=%d active=%d, want total=2 active=1", total1, active1)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected loader called once, got %d", got)
	}

	// Second call within the TTL window must reuse the cached result and
	// must NOT hit the loader again.
	total2, active2 := srv.computeSessionCounts()
	if total2 != total1 || active2 != active1 {
		t.Errorf("second call returned different counts: total=%d active=%d", total2, active2)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected loader still called once after cached hit, got %d", got)
	}
}

func TestComputeSessionCounts_TTLCache_ReloadsAfterTTLExpires(t *testing.T) {
	srv := testServer(t)
	var calls int32
	srv.sessionCountsLoader = countingManifestLoader([]session.Manifest{
		{SessionID: "a", UpdatedAt: time.Now().UTC()},
	}, &calls)

	srv.computeSessionCounts()
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 call, got %d", got)
	}

	// Force the cache to look stale without sleeping the real TTL.
	srv.sessionCountsMu.Lock()
	srv.sessionCountsCache.computedAt = time.Now().Add(-2 * sessionCountsTTL)
	srv.sessionCountsMu.Unlock()

	srv.computeSessionCounts()
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected loader called again after TTL expiry, got %d calls", got)
	}
}

func TestInvalidateSessionCountsCache_ForcesReloadOnNextCall(t *testing.T) {
	srv := testServer(t)
	var calls int32
	srv.sessionCountsLoader = countingManifestLoader([]session.Manifest{
		{SessionID: "a", UpdatedAt: time.Now().UTC()},
	}, &calls)

	srv.computeSessionCounts()
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 call, got %d", got)
	}

	srv.invalidateSessionCountsCache()

	srv.computeSessionCounts()
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected loader called again after invalidate, got %d calls", got)
	}
}

func TestHandleCreateSession_InvalidatesSessionCountsCache(t *testing.T) {
	srv, ts := newTestServer(t)
	var calls int32
	srv.sessionCountsLoader = countingManifestLoader(nil, &calls)

	srv.computeSessionCounts() // prime the cache
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 call priming cache, got %d", got)
	}

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 creating session, got %d", resp.StatusCode)
	}

	// A cached result from before the create is no longer trustworthy;
	// invalidation must force the next computeSessionCounts to reload.
	srv.computeSessionCounts()
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected loader called again after session create, got %d calls", got)
	}
}
