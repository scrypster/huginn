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

// createTestWorkflowHTTP is a small helper that POSTs a minimal workflow and
// returns its assigned ID. Reused across the Phase 5 trigger tests so each
// test stays focused on the trigger semantics rather than the create path.
// (Distinct from createTestWorkflow in workflow_skills_connections_test.go,
// which writes the YAML file directly without going through the HTTP API.)
func createTestWorkflowHTTP(t *testing.T, ts string, payload string) string {
	t.Helper()
	req, _ := http.NewRequest("POST", ts+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create workflow: want 200, got %d", resp.StatusCode)
	}
	var created map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("create workflow: empty id")
	}
	return id
}

// captureRunner returns a workflow runner that writes the
// trigger-supplied initial inputs to the provided channel exactly once.
// Tests use it to verify the HTTP body → context → runner plumbing without
// having to spin up a real agent or scheduler runtime.
func captureRunner(seen chan<- map[string]string, done chan<- struct{}) func(ctx context.Context, w *scheduler.Workflow) error {
	var once sync.Once
	return func(ctx context.Context, _ *scheduler.Workflow) error {
		// Take a defensive copy so the goroutine that reads `seen` can't be
		// surprised by mutation in the runner's caller.
		got := scheduler.InitialInputs(ctx)
		cp := make(map[string]string, len(got))
		for k, v := range got {
			cp[k] = v
		}
		once.Do(func() {
			seen <- cp
			close(done)
		})
		return nil
	}
}

// TestHandleRunWorkflow_WithInputsBody verifies a manual `/run` trigger that
// posts `{"inputs":{...}}` propagates the values to the runner via context.
// This is the single most-requested workflow capability — without it manual
// runs can't supply variables to the first step.
func TestHandleRunWorkflow_WithInputsBody(t *testing.T) {
	srv, ts := newTestServer(t)
	id := createTestWorkflowHTTP(t, ts.URL,
		`{"name":"manual-inputs","enabled":false,"schedule":"0 9 * * 1-5","steps":[]}`)

	sched := scheduler.New()
	sched.Start()
	t.Cleanup(func() { sched.Stop(context.Background()) })

	seen := make(chan map[string]string, 1)
	done := make(chan struct{})
	sched.SetWorkflowRunner(captureRunner(seen, done))

	srv.mu.Lock()
	srv.sched = sched
	srv.mu.Unlock()

	body := `{"inputs":{"greeting":"hello","count":7,"flag":null,"nested":{"a":1}}}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/"+id+"/run", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("manual run: want 200, got %d", resp.StatusCode)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for runner to receive context inputs")
	}
	got := <-seen
	if got["greeting"] != "hello" {
		t.Errorf("greeting: got %q, want %q", got["greeting"], "hello")
	}
	if got["count"] != "7" {
		t.Errorf("count: got %q, want %q (numbers re-marshalled to JSON)", got["count"], "7")
	}
	if got["flag"] != "" {
		t.Errorf("flag (null): got %q, want empty string", got["flag"])
	}
	if got["nested"] != `{"a":1}` {
		t.Errorf("nested: got %q, want compact JSON", got["nested"])
	}
}

// TestHandleRunWorkflow_NoBody_NoInputs verifies the existing no-body trigger
// keeps working — Phase 5 must not break the legacy "fire-and-forget" flow
// used by every cron/scheduled run today.
func TestHandleRunWorkflow_NoBody_NoInputs(t *testing.T) {
	srv, ts := newTestServer(t)
	id := createTestWorkflowHTTP(t, ts.URL,
		`{"name":"no-body","enabled":false,"schedule":"0 9 * * 1-5","steps":[]}`)

	sched := scheduler.New()
	sched.Start()
	t.Cleanup(func() { sched.Stop(context.Background()) })

	seen := make(chan map[string]string, 1)
	done := make(chan struct{})
	sched.SetWorkflowRunner(captureRunner(seen, done))

	srv.mu.Lock()
	srv.sched = sched
	srv.mu.Unlock()

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/"+id+"/run", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("no-body run: want 200, got %d", resp.StatusCode)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for runner")
	}
	if got := <-seen; len(got) != 0 {
		t.Errorf("expected empty inputs for no-body run, got %#v", got)
	}
}

// TestHandleRunWorkflow_MalformedBody_DoesNotReject verifies a manual run
// with a malformed JSON body is forgiving — the legacy "no body" flow used
// to send no Content-Type at all and we do not want to rejector spurious
// invalid-bodies sent by buggy clients.
func TestHandleRunWorkflow_MalformedBody_DoesNotReject(t *testing.T) {
	srv, ts := newTestServer(t)
	id := createTestWorkflowHTTP(t, ts.URL,
		`{"name":"bad-body","enabled":false,"schedule":"0 9 * * 1-5","steps":[]}`)

	sched := scheduler.New()
	sched.Start()
	t.Cleanup(func() { sched.Stop(context.Background()) })

	seen := make(chan map[string]string, 1)
	done := make(chan struct{})
	sched.SetWorkflowRunner(captureRunner(seen, done))
	srv.mu.Lock()
	srv.sched = sched
	srv.mu.Unlock()

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/"+id+"/run", strings.NewReader("not json {{"))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("malformed body: want 200 (forgiving), got %d", resp.StatusCode)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for runner")
	}
	if got := <-seen; len(got) != 0 {
		t.Errorf("expected empty inputs for malformed body, got %#v", got)
	}
}

// TestHandleTriggerWebhook_FlatPayload verifies the webhook endpoint seeds
// the run scratchpad with both the full payload and each top-level key, so a
// simple webhook workflow can read either {{run.scratch.payload}} (raw JSON)
// or {{run.scratch.action}} (a flat key).
func TestHandleTriggerWebhook_FlatPayload(t *testing.T) {
	srv, ts := newTestServer(t)
	id := createTestWorkflowHTTP(t, ts.URL,
		`{"name":"webhook-flat","enabled":false,"schedule":"0 9 * * 1-5","steps":[]}`)

	sched := scheduler.New()
	sched.Start()
	t.Cleanup(func() { sched.Stop(context.Background()) })

	seen := make(chan map[string]string, 1)
	done := make(chan struct{})
	sched.SetWorkflowRunner(captureRunner(seen, done))

	srv.mu.Lock()
	srv.sched = sched
	srv.mu.Unlock()

	body := `{"action":"opened","ref":"main","sender":{"login":"alice"}}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/"+id+"/webhook", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("webhook: want 200, got %d", resp.StatusCode)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for webhook runner")
	}
	got := <-seen
	if got["action"] != "opened" {
		t.Errorf("action: got %q, want %q", got["action"], "opened")
	}
	if got["ref"] != "main" {
		t.Errorf("ref: got %q, want %q", got["ref"], "main")
	}
	if got["sender"] != `{"login":"alice"}` {
		t.Errorf("sender (nested): got %q, want compact JSON", got["sender"])
	}
	// payload key always present, equals the full body re-marshalled.
	if got["payload"] != `{"action":"opened","ref":"main","sender":{"login":"alice"}}` {
		t.Errorf("payload: got %q, want full JSON", got["payload"])
	}
}

// TestHandleTriggerWebhook_NonObjectPayload verifies the webhook endpoint
// accepts a non-object payload (array, scalar) by exposing it as `payload`
// only — without flattening top-level keys (which don't exist for arrays).
func TestHandleTriggerWebhook_NonObjectPayload(t *testing.T) {
	srv, ts := newTestServer(t)
	id := createTestWorkflowHTTP(t, ts.URL,
		`{"name":"webhook-array","enabled":false,"schedule":"0 9 * * 1-5","steps":[]}`)

	sched := scheduler.New()
	sched.Start()
	t.Cleanup(func() { sched.Stop(context.Background()) })

	seen := make(chan map[string]string, 1)
	done := make(chan struct{})
	sched.SetWorkflowRunner(captureRunner(seen, done))
	srv.mu.Lock()
	srv.sched = sched
	srv.mu.Unlock()

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/"+id+"/webhook", strings.NewReader(`[1,2,3]`))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("webhook (array): want 200, got %d", resp.StatusCode)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for webhook runner")
	}
	got := <-seen
	if got["payload"] != `[1,2,3]` {
		t.Errorf("payload: got %q, want %q", got["payload"], "[1,2,3]")
	}
	// No flattened keys for non-object payloads — only payload itself.
	if len(got) != 1 {
		t.Errorf("expected only `payload` key for array, got %#v", got)
	}
}

// TestHandleTriggerWebhook_NotFound verifies a webhook for a non-existent
// workflow id returns 404 — important so external callers don't silently
// queue triggers against deleted workflows.
func TestHandleTriggerWebhook_NotFound(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/does-not-exist/webhook", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("webhook for missing workflow: want 404, got %d", resp.StatusCode)
	}
}

// TestHandleTriggerWebhook_NoScheduler verifies the webhook handler returns
// 503 when the scheduler is not yet wired (e.g. early in startup or in a
// test environment with no scheduler).
func TestHandleTriggerWebhook_NoScheduler(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.sched = nil
	id := createTestWorkflowHTTP(t, ts.URL,
		`{"name":"webhook-no-sched","enabled":false,"schedule":"0 9 * * 1-5","steps":[]}`)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/"+id+"/webhook", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("webhook with no scheduler: want 503, got %d", resp.StatusCode)
	}
}

// TestHandleTriggerWebhook_EmptyBody verifies an empty webhook body still
// triggers the workflow with empty inputs — a "ping" webhook.
func TestHandleTriggerWebhook_EmptyBody(t *testing.T) {
	srv, ts := newTestServer(t)
	id := createTestWorkflowHTTP(t, ts.URL,
		`{"name":"webhook-empty","enabled":false,"schedule":"0 9 * * 1-5","steps":[]}`)

	sched := scheduler.New()
	sched.Start()
	t.Cleanup(func() { sched.Stop(context.Background()) })

	seen := make(chan map[string]string, 1)
	done := make(chan struct{})
	sched.SetWorkflowRunner(captureRunner(seen, done))
	srv.mu.Lock()
	srv.sched = sched
	srv.mu.Unlock()

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/"+id+"/webhook", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("empty webhook: want 200, got %d", resp.StatusCode)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for empty-webhook runner")
	}
	if got := <-seen; len(got) != 0 {
		t.Errorf("expected empty inputs for empty webhook, got %#v", got)
	}
}
