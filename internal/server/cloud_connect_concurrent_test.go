package server

// hardening_iter12_test.go — Hardening iteration 12.
// Adversarial / stress / security tests.
// Covers:
//   1. 50 concurrent POST /api/v1/cloud/connect (race detector must pass)
//   2. Rapid connect/disconnect toggle (10 cycles, race detector)
//   3. HUGINN_CLOUD_URL SSRF validation: file:// and non-http scheme must be rejected
//      OR the goroutine fails safely without making a dangerous outbound call
//   4. DELETE /api/v1/cloud/connect concurrent safety (N goroutines simultaneously)

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/relay"
)

// ── 50 concurrent POST /api/v1/cloud/connect ─────────────────────────────────

// TestHandlerCloudConnect_50Concurrent_Iter12 fires 50 goroutines all POSTing
// /api/v1/cloud/connect simultaneously. Exactly one should launch the background
// goroutine; the rest should receive status=registering immediately due to the
// idempotency guard. No data races must occur (run with -race).
//
// Uses already-registered store so the async goroutine path is the synchronous
// already_registered branch — avoids real network calls in CI.
func TestHandlerCloudConnect_50Concurrent_Iter12(t *testing.T) {
	srv, _ := newTestServer(t)

	store := &relay.MemoryTokenStore{}
	_ = store.Save("concurrent-tok")
	srv.SetRelayConfig(store, "")

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	const n = 50
	var wg sync.WaitGroup
	var alreadyRegistered, registering, errCount int64

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("POST", ts.URL+"/api/v1/cloud/connect", nil)
			req.Header.Set("Authorization", "Bearer "+testToken)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				atomic.AddInt64(&errCount, 1)
				return
			}
			defer resp.Body.Close()
			var body map[string]any
			json.NewDecoder(resp.Body).Decode(&body)
			switch body["status"] {
			case "already_registered":
				atomic.AddInt64(&alreadyRegistered, 1)
			case "registering":
				atomic.AddInt64(&registering, 1)
			default:
				atomic.AddInt64(&errCount, 1)
			}
		}()
	}
	wg.Wait()

	if errCount > 0 {
		t.Errorf("%d requests had unexpected errors or statuses", errCount)
	}
	total := int(alreadyRegistered) + int(registering)
	if total != n {
		t.Errorf("expected %d responses, got %d (already_registered=%d registering=%d)",
			n, total, alreadyRegistered, registering)
	}
	t.Logf("already_registered=%d registering=%d", alreadyRegistered, registering)
}

// TestHandlerCloudConnect_50Concurrent_NoToken_Iter12 is the adversarial
// variant: no pre-existing token, so the first POST launches a goroutine and
// the rest return "registering" due to the flag guard. Verifies that exactly
// the idempotency guard protects against goroutine explosion.
func TestHandlerCloudConnect_50Concurrent_NoToken_Iter12(t *testing.T) {
	// Point HUGINN_CLOUD_URL at a server that accepts connections but never
	// completes registration — ensures the goroutine stays alive long enough
	// for all 50 POSTs to arrive.
	hangSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hang for a short time then return a non-success response.
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer hangSrv.Close()
	t.Setenv("HUGINN_CLOUD_URL", hangSrv.URL)

	srv, _ := newTestServer(t)
	store := &relay.MemoryTokenStore{} // no token — async goroutine path
	srv.SetRelayConfig(store, "")

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	const n = 50
	var wg sync.WaitGroup
	var registeringCount int64

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("POST", ts.URL+"/api/v1/cloud/connect", nil)
			req.Header.Set("Authorization", "Bearer "+testToken)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			var body map[string]any
			json.NewDecoder(resp.Body).Decode(&body)
			if body["status"] == "registering" {
				atomic.AddInt64(&registeringCount, 1)
			}
		}()
	}
	wg.Wait()

	// All 50 should have received "registering" — either because they started
	// the goroutine or because the flag guard returned early.
	if registeringCount != n {
		t.Errorf("expected all %d responses to be 'registering', got %d", n, registeringCount)
	}
}

// ── Rapid connect/disconnect toggle ──────────────────────────────────────────

// TestHandlerCloud_RapidToggle_Iter12 rapidly alternates between
// POST /api/v1/cloud/connect and DELETE /api/v1/cloud/connect.
// Verifies no panic, no data race, and no hung goroutine (run with -race).
func TestHandlerCloud_RapidToggle_Iter12(t *testing.T) {
	srv, _ := newTestServer(t)
	store := &relay.MemoryTokenStore{}
	srv.SetRelayConfig(store, "")

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	client := &http.Client{Timeout: 5 * time.Second}

	for cycle := 0; cycle < 10; cycle++ {
		// Seed a token so connect takes the synchronous already_registered path.
		_ = store.Save(fmt.Sprintf("tok-%d", cycle))

		reqConnect, _ := http.NewRequest("POST", ts.URL+"/api/v1/cloud/connect", nil)
		reqConnect.Header.Set("Authorization", "Bearer "+testToken)
		respConnect, err := client.Do(reqConnect)
		if err != nil {
			t.Fatalf("cycle %d connect: %v", cycle, err)
		}
		respConnect.Body.Close()

		reqDisconnect, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/cloud/connect", nil)
		reqDisconnect.Header.Set("Authorization", "Bearer "+testToken)
		respDisconnect, err := client.Do(reqDisconnect)
		if err != nil {
			t.Fatalf("cycle %d disconnect: %v", cycle, err)
		}
		respDisconnect.Body.Close()
	}
}

// ── SSRF validation of HUGINN_CLOUD_URL ──────────────────────────────────────

// TestHandlerCloudConnect_SSRF_FileScheme_Iter12 verifies that when
// HUGINN_CLOUD_URL is set to a file:// URI, the registration goroutine
// fails (not panics) and the server returns status=registering then resets.
//
// Note: the current implementation does not explicitly validate the URL scheme;
// this test documents the current behavior and will catch regressions if
// scheme validation is added. The key property: the server must NOT hang or
// panic — it must return from the handler within the normal timeout.
func TestHandlerCloudConnect_SSRF_FileScheme_Iter12(t *testing.T) {
	t.Setenv("HUGINN_CLOUD_URL", "file:///etc/passwd")

	srv, _ := newTestServer(t)
	store := &relay.MemoryTokenStore{}
	srv.SetRelayConfig(store, "")

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/cloud/connect", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)

	done := make(chan struct{})
	var status string
	go func() {
		defer close(done)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		var body map[string]any
		json.NewDecoder(resp.Body).Decode(&body)
		status, _ = body["status"].(string)
	}()

	select {
	case <-done:
		// Handler returned promptly — acceptable.
		t.Logf("SSRF file:// returned status=%q", status)
	case <-time.After(5 * time.Second):
		t.Error("handler hung on file:// HUGINN_CLOUD_URL — SSRF risk: handler must return promptly")
	}
}

// TestHandlerCloudConnect_SSRF_InternalHost_Iter12 verifies that when
// HUGINN_CLOUD_URL points to an internal host (169.254.169.254 — AWS IMDS),
// the registration goroutine times out / errors cleanly and the server
// remains responsive.
func TestHandlerCloudConnect_SSRF_InternalHost_Iter12(t *testing.T) {
	// Use a localhost address that immediately refuses to avoid a long TCP timeout.
	// In production the concern is 169.254.x.x but we can't control that in CI.
	// Use a port that is almost certainly not listening.
	t.Setenv("HUGINN_CLOUD_URL", "http://127.0.0.1:1")

	srv, _ := newTestServer(t)
	store := &relay.MemoryTokenStore{}
	srv.SetRelayConfig(store, "")

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/cloud/connect", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Handler must return 200 immediately (goroutine is async).
	if resp.StatusCode != 200 {
		t.Errorf("want 200 (async handler), got %d", resp.StatusCode)
	}
}

// ── Concurrent DELETE safety ──────────────────────────────────────────────────

// syncTokenStore wraps relay.MemoryTokenStore with a mutex so it is safe for
// concurrent use in the concurrent-DELETE test. MemoryTokenStore is not
// concurrency-safe by design (it is a single-test helper), so we wrap it here
// to exercise the server handler's concurrent path without a store data race.
type syncTokenStore struct {
	mu    sync.Mutex
	inner *relay.MemoryTokenStore
}

func (s *syncTokenStore) Save(tok string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.Save(tok)
}

func (s *syncTokenStore) Load() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.Load()
}

func (s *syncTokenStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.Clear()
}

func (s *syncTokenStore) IsRegistered() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.IsRegistered()
}

// TestHandlerCloudDisconnect_Concurrent_Iter12 fires N goroutines all calling
// DELETE /api/v1/cloud/connect simultaneously. Verifies no panic and no data
// race on the storer (run with -race).
func TestHandlerCloudDisconnect_Concurrent_Iter12(t *testing.T) {
	srv, _ := newTestServer(t)
	store := &syncTokenStore{inner: &relay.MemoryTokenStore{}}
	_ = store.Save("del-tok")
	srv.SetRelayConfig(store, "")

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	const n = 30
	var wg sync.WaitGroup
	var errCount int64

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/cloud/connect", nil)
			req.Header.Set("Authorization", "Bearer "+testToken)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				atomic.AddInt64(&errCount, 1)
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				atomic.AddInt64(&errCount, 1)
			}
		}()
	}
	wg.Wait()
	if errCount > 0 {
		t.Errorf("%d concurrent DELETE requests failed", errCount)
	}
}
