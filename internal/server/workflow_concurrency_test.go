package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/scheduler"
)

// ---------------------------------------------------------------------------
// Helper: create a workflow via the API and return its ID.
// ---------------------------------------------------------------------------

func createWorkflowForConcurrencyTest(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	payload := `{"name":"concurrency-wf","enabled":false,"schedule":"0 9 * * 1-5","steps":[]}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create workflow: want 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	id, _ := body["id"].(string)
	if id == "" {
		t.Fatal("create workflow: no id in response")
	}
	return id
}

// ---------------------------------------------------------------------------
// 503: ErrConcurrencyLimitReached → 503 status code
// ---------------------------------------------------------------------------

// TestHandleRunWorkflow_ConcurrencyLimit_Returns503 verifies that when the
// scheduler's semaphore is saturated (maxConcurrentWorkflows workflows already
// running), a new POST to /run returns HTTP 503 Service Unavailable.
func TestHandleRunWorkflow_ConcurrencyLimit_Returns503(t *testing.T) {
	srv, ts := newTestServer(t)

	id := createWorkflowForConcurrencyTest(t, ts)

	// Build a scheduler whose semaphore is pre-filled so the next
	// TriggerWorkflow call hits ErrConcurrencyLimitReached immediately.
	release := make(chan struct{})
	var started atomic.Int64

	sched := scheduler.New()
	sched.SetWorkflowRunner(func(ctx context.Context, w *scheduler.Workflow) error {
		started.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	})

	// Fill all 10 semaphore slots.
	for i := 0; i < 10; i++ {
		wf := &scheduler.Workflow{ID: "blocker-" + string(rune('a'+i)), Name: "Blocker"}
		if err := sched.TriggerWorkflow(context.Background(), wf); err != nil {
			close(release)
			t.Fatalf("fill slot %d: %v", i, err)
		}
	}

	// Wait for all 10 runner goroutines to start (they all block on release).
	deadline := time.After(3 * time.Second)
	for started.Load() < 10 {
		select {
		case <-deadline:
			close(release)
			t.Fatalf("not all runner goroutines started within 3s; started=%d", started.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Wire the saturated scheduler into the server.
	srv.mu.Lock()
	srv.sched = sched
	srv.mu.Unlock()

	t.Cleanup(func() {
		close(release)
		sched.Stop(context.Background())
	})

	// Trigger the workflow that already exists in the server's huginnDir.
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/"+id+"/run", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Retry-After: 60 header present on 503 concurrency response
// ---------------------------------------------------------------------------

func TestHandleRunWorkflow_ConcurrencyLimit_RetryAfterHeader(t *testing.T) {
	srv, ts := newTestServer(t)

	id := createWorkflowForConcurrencyTest(t, ts)

	release := make(chan struct{})
	var started atomic.Int64

	sched := scheduler.New()
	sched.SetWorkflowRunner(func(ctx context.Context, w *scheduler.Workflow) error {
		started.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	})

	for i := 0; i < 10; i++ {
		wf := &scheduler.Workflow{ID: "blocker2-" + string(rune('a'+i)), Name: "Blocker"}
		if err := sched.TriggerWorkflow(context.Background(), wf); err != nil {
			close(release)
			t.Fatalf("fill slot %d: %v", i, err)
		}
	}

	deadline := time.After(3 * time.Second)
	for started.Load() < 10 {
		select {
		case <-deadline:
			close(release)
			t.Fatalf("slots not filled within 3s; started=%d", started.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}

	srv.mu.Lock()
	srv.sched = sched
	srv.mu.Unlock()

	t.Cleanup(func() {
		close(release)
		sched.Stop(context.Background())
	})

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/"+id+"/run", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", resp.StatusCode)
	}

	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter != "60" {
		t.Errorf("Retry-After = %q, want \"60\"", retryAfter)
	}
}

// ---------------------------------------------------------------------------
// Response body has machine-readable error on concurrency limit
// ---------------------------------------------------------------------------

func TestHandleRunWorkflow_ConcurrencyLimit_ErrorBodyMachineReadable(t *testing.T) {
	srv, ts := newTestServer(t)

	id := createWorkflowForConcurrencyTest(t, ts)

	release := make(chan struct{})
	var started atomic.Int64

	sched := scheduler.New()
	sched.SetWorkflowRunner(func(ctx context.Context, w *scheduler.Workflow) error {
		started.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	})

	for i := 0; i < 10; i++ {
		wf := &scheduler.Workflow{ID: "blocker3-" + string(rune('a'+i)), Name: "Blocker"}
		if err := sched.TriggerWorkflow(context.Background(), wf); err != nil {
			close(release)
			t.Fatalf("fill slot %d: %v", i, err)
		}
	}

	deadline := time.After(3 * time.Second)
	for started.Load() < 10 {
		select {
		case <-deadline:
			close(release)
			t.Fatalf("slots not filled within 3s; started=%d", started.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}

	srv.mu.Lock()
	srv.sched = sched
	srv.mu.Unlock()

	t.Cleanup(func() {
		close(release)
		sched.Stop(context.Background())
	})

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/"+id+"/run", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}

	// The response must have a non-empty "error" key so clients can parse it.
	errMsg, _ := body["error"].(string)
	if errMsg == "" {
		t.Errorf("want non-empty 'error' key in 503 response body; got: %v", body)
	}
	// The error message should mention capacity / concurrency so it's machine-interpretable.
	lowerErr := strings.ToLower(errMsg)
	if !strings.Contains(lowerErr, "capacity") && !strings.Contains(lowerErr, "concurrent") && !strings.Contains(lowerErr, "retry") {
		t.Errorf("error message not machine-readable (missing capacity/concurrent/retry): %q", errMsg)
	}
}
