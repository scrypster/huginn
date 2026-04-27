package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/scheduler"
)

// memRunStore is a test-only in-memory implementation of WorkflowRunStoreInterface.
// We can't reuse mockRunStore from internal/scheduler tests because it's
// unexported, but the interface itself is exported so satisfying it here is
// straightforward.
type memRunStore struct {
	mu   sync.Mutex
	runs map[string]map[string]*scheduler.WorkflowRun // workflowID -> runID -> run
}

func newMemRunStore() *memRunStore {
	return &memRunStore{runs: map[string]map[string]*scheduler.WorkflowRun{}}
}

func (s *memRunStore) Append(workflowID string, run *scheduler.WorkflowRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[workflowID]; !ok {
		s.runs[workflowID] = map[string]*scheduler.WorkflowRun{}
	}
	s.runs[workflowID][run.ID] = run
	return nil
}

func (s *memRunStore) List(workflowID string, n int) ([]*scheduler.WorkflowRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*scheduler.WorkflowRun{}
	for _, r := range s.runs[workflowID] {
		out = append(out, r)
		if n > 0 && len(out) >= n {
			break
		}
	}
	return out, nil
}

func (s *memRunStore) Get(workflowID, runID string) (*scheduler.WorkflowRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.runs[workflowID]; ok {
		return m[runID], nil
	}
	return nil, nil
}

// seedRun is a tiny test helper that inserts a synthetic run into the store
// so the replay/fork/diff handlers have something to act on.
func seedRun(t *testing.T, srv *Server, workflowID, runID string, snap *scheduler.Workflow, inputs map[string]string, status scheduler.WorkflowRunStatus, steps []scheduler.WorkflowStepResult) {
	t.Helper()
	ms, ok := srv.workflowRunStore.(*memRunStore)
	if !ok {
		t.Fatalf("test-only seed: workflowRunStore is not memRunStore (got %T)", srv.workflowRunStore)
	}
	completedAt := time.Now().UTC()
	run := &scheduler.WorkflowRun{
		ID:               runID,
		WorkflowID:       workflowID,
		Status:           status,
		StartedAt:        time.Now().UTC().Add(-time.Minute),
		CompletedAt:      &completedAt,
		Steps:            steps,
		TriggerInputs:    inputs,
		WorkflowSnapshot: snap,
	}
	if err := ms.Append(workflowID, run); err != nil {
		t.Fatalf("seed run: %v", err)
	}
}

// installCapturingScheduler wires a scheduler whose runner records the
// trigger inputs supplied through the context so tests can verify replay/
// fork plumbed the values correctly.
func installCapturingScheduler(t *testing.T, srv *Server) (chan map[string]string, chan struct{}) {
	t.Helper()
	sched := scheduler.New()
	sched.Start()
	t.Cleanup(func() { sched.Stop(context.Background()) })
	seen := make(chan map[string]string, 4)
	done := make(chan struct{}, 4)
	sched.SetWorkflowRunner(func(ctx context.Context, _ *scheduler.Workflow) error {
		got := scheduler.InitialInputs(ctx)
		cp := make(map[string]string, len(got))
		for k, v := range got {
			cp[k] = v
		}
		seen <- cp
		done <- struct{}{}
		return nil
	})
	srv.mu.Lock()
	srv.sched = sched
	srv.mu.Unlock()
	return seen, done
}

// TestHandleReplayWorkflowRun_UsesSnapshotAndInputs verifies replay reuses
// the stored TriggerInputs verbatim and runs against the stored snapshot.
func TestHandleReplayWorkflowRun_UsesSnapshotAndInputs(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.workflowRunStore = newMemRunStore()

	id := createTestWorkflowHTTP(t, ts.URL,
		`{"name":"replay-target","enabled":false,"schedule":"0 9 * * 1-5","steps":[]}`)
	snap := &scheduler.Workflow{ID: id, Name: "replay-target", Description: "snapshotted-v1"}
	seedRun(t, srv, id, "run-1", snap, map[string]string{"k": "v", "m": "n"}, scheduler.WorkflowRunStatusComplete, nil)

	seen, done := installCapturingScheduler(t, srv)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/"+id+"/runs/run-1/replay", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replay: want 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Replay-Source"); got != "snapshot" {
		t.Errorf("X-Replay-Source = %q, want %q", got, "snapshot")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for replay runner")
	}
	got := <-seen
	if got["k"] != "v" || got["m"] != "n" {
		t.Errorf("replay inputs not propagated: %#v", got)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["used_snapshot"] != true {
		t.Errorf("response body used_snapshot = %v, want true", body["used_snapshot"])
	}
}

// TestHandleReplayWorkflowRun_NoSnapshot_FallsBackToLive verifies the
// backwards-compat path: pre-Phase 6 runs with no snapshot fall back to the
// live YAML definition.
func TestHandleReplayWorkflowRun_NoSnapshot_FallsBackToLive(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.workflowRunStore = newMemRunStore()

	id := createTestWorkflowHTTP(t, ts.URL,
		`{"name":"replay-live","enabled":false,"schedule":"0 9 * * 1-5","steps":[]}`)
	seedRun(t, srv, id, "run-old", nil, map[string]string{"old": "input"}, scheduler.WorkflowRunStatusComplete, nil)

	seen, done := installCapturingScheduler(t, srv)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/"+id+"/runs/run-old/replay", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("legacy replay: want 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Replay-Source"); got != "live-definition" {
		t.Errorf("X-Replay-Source = %q, want %q", got, "live-definition")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for legacy replay runner")
	}
	if got := <-seen; got["old"] != "input" {
		t.Errorf("legacy replay inputs not propagated: %#v", got)
	}
}

// TestHandleReplayWorkflowRun_NotFound returns 404 when the run id is wrong.
func TestHandleReplayWorkflowRun_NotFound(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.workflowRunStore = newMemRunStore()
	id := createTestWorkflowHTTP(t, ts.URL,
		`{"name":"replay-404","enabled":false,"schedule":"0 9 * * 1-5","steps":[]}`)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/"+id+"/runs/nope/replay", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("replay 404: want 404, got %d", resp.StatusCode)
	}
}

// TestHandleForkWorkflowRun_OverridesWin verifies fork merges base inputs
// from the prior run with body overrides — overrides win on collision.
func TestHandleForkWorkflowRun_OverridesWin(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.workflowRunStore = newMemRunStore()
	id := createTestWorkflowHTTP(t, ts.URL,
		`{"name":"fork-target","enabled":false,"schedule":"0 9 * * 1-5","steps":[]}`)
	snap := &scheduler.Workflow{ID: id, Name: "fork-target"}
	seedRun(t, srv, id, "run-2", snap,
		map[string]string{"a": "1", "b": "2"},
		scheduler.WorkflowRunStatusComplete, nil)

	seen, done := installCapturingScheduler(t, srv)

	body := `{"inputs":{"b":"BB","c":"3"}}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/"+id+"/runs/run-2/fork", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fork: want 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Fork-Source"); got != "snapshot" {
		t.Errorf("X-Fork-Source = %q, want %q", got, "snapshot")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for fork runner")
	}
	got := <-seen
	if got["a"] != "1" || got["b"] != "BB" || got["c"] != "3" {
		t.Errorf("fork merge mismatch: %#v", got)
	}
}

// TestHandleForkWorkflowRun_UseLiveDefinition verifies the
// use_live_definition flag bypasses the snapshot.
func TestHandleForkWorkflowRun_UseLiveDefinition(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.workflowRunStore = newMemRunStore()
	id := createTestWorkflowHTTP(t, ts.URL,
		`{"name":"fork-live","enabled":false,"schedule":"0 9 * * 1-5","steps":[]}`)
	snap := &scheduler.Workflow{ID: id, Name: "fork-live", Description: "snapshotted"}
	seedRun(t, srv, id, "run-3", snap,
		map[string]string{"a": "1"},
		scheduler.WorkflowRunStatusComplete, nil)

	_, done := installCapturingScheduler(t, srv)

	body := `{"use_live_definition":true}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/"+id+"/runs/run-3/fork", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fork (live): want 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Fork-Source"); got != "live-definition" {
		t.Errorf("X-Fork-Source = %q, want %q", got, "live-definition")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for fork runner")
	}
}

// TestHandleDiffWorkflowRuns verifies the diff endpoint returns a structured
// payload with per-step rows aligned by Position.
func TestHandleDiffWorkflowRuns(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.workflowRunStore = newMemRunStore()
	id := createTestWorkflowHTTP(t, ts.URL,
		`{"name":"diff-target","enabled":false,"schedule":"0 9 * * 1-5","steps":[]}`)

	seedRun(t, srv, id, "L", nil, nil,
		scheduler.WorkflowRunStatusComplete,
		[]scheduler.WorkflowStepResult{
			{Position: 1, Slug: "fetch", Status: "complete", Output: "ok"},
			{Position: 2, Slug: "summarize", Status: "complete", Output: "tldr-A"},
		})
	seedRun(t, srv, id, "R", nil, nil,
		scheduler.WorkflowRunStatusFailed,
		[]scheduler.WorkflowStepResult{
			{Position: 1, Slug: "fetch", Status: "complete", Output: "ok"},
			{Position: 2, Slug: "summarize", Status: "failed", Error: "boom"},
		})

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/workflows/"+id+"/runs/L/diff/R", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("diff: want 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["left_run_id"] != "L" || body["right_run_id"] != "R" {
		t.Errorf("ids: %+v", body)
	}
	if body["status_changed"] != true {
		t.Errorf("status_changed: got %v, want true", body["status_changed"])
	}
	steps, _ := body["steps"].([]any)
	if len(steps) != 2 {
		t.Fatalf("steps len = %d, want 2", len(steps))
	}
	step1 := steps[0].(map[string]any)
	if step1["changed"] == true {
		t.Errorf("step1 should be unchanged: %+v", step1)
	}
	step2 := steps[1].(map[string]any)
	if step2["changed"] != true {
		t.Errorf("step2 should be changed: %+v", step2)
	}
}

// TestHandleDiffWorkflowRuns_LeftMissing returns 404 cleanly.
func TestHandleDiffWorkflowRuns_LeftMissing(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.workflowRunStore = newMemRunStore()
	id := createTestWorkflowHTTP(t, ts.URL,
		`{"name":"diff-404","enabled":false,"schedule":"0 9 * * 1-5","steps":[]}`)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/workflows/"+id+"/runs/missing/diff/also-missing", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("diff 404: want 404, got %d", resp.StatusCode)
	}
}
