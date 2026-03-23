package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/scheduler"
)

func TestHandleListWorkflows_Empty(t *testing.T) {
	_, ts := newTestServer(t)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/workflows", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body []any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
}

func TestHandleCreateWorkflow(t *testing.T) {
	_, ts := newTestServer(t)

	payload := `{"name":"Test WF","enabled":true,"schedule":"0 9 * * 1-5","steps":[{"routine":"pr-review","position":10}]}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var a map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&a); err != nil {
		t.Fatal(err)
	}
	if a["id"] == nil || a["id"] == "" {
		t.Error("want non-empty id in response")
	}
}

func TestHandleGetWorkflow_NotFound(t *testing.T) {
	_, ts := newTestServer(t)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/workflows/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestHandleRunWorkflow_NoScheduler(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.sched = nil

	// First create a workflow so the run handler can find it.
	payload := `{"name":"WF","enabled":false,"schedule":"0 9 * * 1-5","steps":[]}`
	createReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	createReq.Header.Set("Authorization", "Bearer "+testToken)
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatal(err)
	}
	defer createResp.Body.Close()
	var created map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("no id returned from create")
	}

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

func TestHandleUpdateWorkflow_NotFound(t *testing.T) {
	_, ts := newTestServer(t)

	payload := `{"name":"Updated","enabled":false,"schedule":"0 9 * * 1-5","steps":[]}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/workflows/nonexistent", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestHandleDeleteWorkflow_NotFound(t *testing.T) {
	_, ts := newTestServer(t)

	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/workflows/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestHandleListWorkflowRuns_NoStore(t *testing.T) {
	_, ts := newTestServer(t)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/workflows/someid/runs", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	var body []any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 0 {
		t.Errorf("want empty slice, got %d items", len(body))
	}
}

// ---------------------------------------------------------------------------
// Validation: dangling from_step reference
// ---------------------------------------------------------------------------

// TestHandleCreateWorkflow_DanglingFromStep ensures that a POST with a step
// whose Inputs[].from_step references a step name that does not exist in the
// workflow is rejected with 422. Without this guard the dangling reference
// reaches the runner, which silently resolves it to an empty string and
// produces a confusing empty-variable bug at execution time.
func TestHandleCreateWorkflow_DanglingFromStep(t *testing.T) {
	_, ts := newTestServer(t)

	// "step-b" references "step-a" via from_step, but "step-a" is never defined
	// (the only step is "step-b" itself).
	payload := `{
		"name": "bad-ref",
		"enabled": false,
		"schedule": "0 9 * * 1-5",
		"steps": [
			{
				"name": "step-b",
				"agent": "Chris",
				"prompt": "do something with {{output_of_step_a}}",
				"position": 0,
				"inputs": [{"from_step": "step-a", "as": "output_of_step_a"}]
			}
		]
	}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("want 422 for dangling from_step, got %d", resp.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body) //nolint:errcheck
	errMsg, _ := body["error"].(string)
	if errMsg == "" {
		t.Error("want non-empty error message in response body")
	}
}

// TestHandlePutWorkflow_DanglingFromStep ensures that PUT also rejects dangling
// from_step references. This catches the case where a workflow is valid at
// create time but becomes invalid after an inline edit via the UI.
func TestHandlePutWorkflow_DanglingFromStep(t *testing.T) {
	_, ts := newTestServer(t)

	// Create a valid workflow first.
	createPayload := `{"name":"wf","enabled":false,"schedule":"0 9 * * 1-5","steps":[{"name":"step-a","position":0}]}`
	createReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(createPayload))
	createReq.Header.Set("Authorization", "Bearer "+testToken)
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatal(err)
	}
	defer createResp.Body.Close()
	var created map[string]any
	json.NewDecoder(createResp.Body).Decode(&created) //nolint:errcheck
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("no id from create")
	}

	// Now PUT a body that introduces a dangling reference.
	updatePayload := `{
		"name": "wf",
		"enabled": false,
		"schedule": "0 9 * * 1-5",
		"steps": [
			{
				"name": "step-b",
				"position": 0,
				"inputs": [{"from_step": "nonexistent", "as": "x"}]
			}
		]
	}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/workflows/"+id, strings.NewReader(updatePayload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("want 422 for dangling from_step on PUT, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Validation: deliver_to with type=space and empty space_id
// ---------------------------------------------------------------------------

// TestHandleCreateWorkflow_SpaceDeliveryEmptySpaceID ensures that a step-level
// notification delivery of type "space" with an empty space_id is rejected at
// save time (422). Without this guard the empty space_id reaches the delivery
// layer and produces a runtime error instead of a clear API-level rejection.
func TestHandleCreateWorkflow_SpaceDeliveryEmptySpaceID(t *testing.T) {
	_, ts := newTestServer(t)

	payload := `{
		"name": "bad-delivery",
		"enabled": false,
		"schedule": "0 9 * * 1-5",
		"steps": [
			{
				"name": "step-a",
				"position": 0,
				"notify": {
					"on_success": true,
					"deliver_to": [{"type": "space", "space_id": ""}]
				}
			}
		]
	}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("want 422 for space delivery with empty space_id, got %d", resp.StatusCode)
	}
}

// TestHandleCreateWorkflow_WorkflowLevelSpaceDeliveryEmptySpaceID verifies
// the same guard for the workflow-level notification config (not per-step).
func TestHandleCreateWorkflow_WorkflowLevelSpaceDeliveryEmptySpaceID(t *testing.T) {
	_, ts := newTestServer(t)

	payload := `{
		"name": "bad-wf-delivery",
		"enabled": false,
		"schedule": "0 9 * * 1-5",
		"steps": [],
		"notification": {
			"on_success": true,
			"deliver_to": [{"type": "space", "space_id": ""}]
		}
	}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("want 422 for wf-level space delivery with empty space_id, got %d", resp.StatusCode)
	}
}

// TestHandleCreateWorkflow_ValidFromStep ensures that a workflow with a valid
// from_step reference (the referenced step exists in the same workflow) is
// accepted. This is the "happy path" companion to the dangling-ref rejection.
func TestHandleCreateWorkflow_ValidFromStep(t *testing.T) {
	_, ts := newTestServer(t)

	payload := `{
		"name": "good-ref",
		"enabled": false,
		"schedule": "0 9 * * 1-5",
		"steps": [
			{"name": "step-a", "position": 0},
			{
				"name": "step-b",
				"position": 1,
				"inputs": [{"from_step": "step-a", "as": "a_output"}]
			}
		]
	}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200 for valid from_step reference, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Validation: self-referencing from_step
// ---------------------------------------------------------------------------

// TestHandleCreateWorkflow_SelfReferencingFromStep verifies that a step whose
// from_step points to its own name is rejected with 422. A step cannot read
// its own output because it has not yet run when its inputs are resolved.
func TestHandleCreateWorkflow_SelfReferencingFromStep(t *testing.T) {
	_, ts := newTestServer(t)

	payload := `{
		"name": "self-ref",
		"enabled": false,
		"schedule": "0 9 * * 1-5",
		"steps": [
			{
				"name": "step-a",
				"agent": "A",
				"prompt": "do something with {{self_out}}",
				"position": 0,
				"inputs": [{"from_step": "step-a", "as": "self_out"}]
			}
		]
	}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("want 422 for self-referencing from_step, got %d", resp.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body) //nolint:errcheck
	errMsg, _ := body["error"].(string)
	if errMsg == "" {
		t.Error("want non-empty error message for self-referencing from_step")
	}
}

// ---------------------------------------------------------------------------
// Validation: duplicate step names
// ---------------------------------------------------------------------------

// TestHandleCreateWorkflow_DuplicateStepNames verifies that a workflow with two
// steps that share the same name is rejected with 422. Duplicate names make
// from_step resolution ambiguous (the runner resolves to the first match).
func TestHandleCreateWorkflow_DuplicateStepNames(t *testing.T) {
	_, ts := newTestServer(t)

	payload := `{
		"name": "dup-names",
		"enabled": false,
		"schedule": "0 9 * * 1-5",
		"steps": [
			{"name": "step-a", "agent": "A", "prompt": "do A", "position": 0},
			{"name": "step-a", "agent": "B", "prompt": "do B", "position": 1}
		]
	}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("want 422 for duplicate step names, got %d", resp.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body) //nolint:errcheck
	errMsg, _ := body["error"].(string)
	if errMsg == "" {
		t.Error("want non-empty error message for duplicate step names")
	}
}

// ---------------------------------------------------------------------------
// Validation: nil/empty steps (smoke test)
// ---------------------------------------------------------------------------

// TestValidateWorkflow_NilSteps ensures validateWorkflow does not panic or
// error on a workflow with no steps. The loop over nil slice is safe but this
// test documents the contract.
func TestValidateWorkflow_NilSteps(t *testing.T) {
	_, ts := newTestServer(t)

	payload := `{"name":"empty-steps","enabled":false,"schedule":"0 9 * * 1-5","steps":null}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200 for nil steps (valid workflow), got %d", resp.StatusCode)
	}
}

// TestHandleCreateWorkflow_ValidSpaceDelivery ensures that a space delivery
// entry with a non-empty space_id passes validation and the workflow is saved.
func TestHandleCreateWorkflow_ValidSpaceDelivery(t *testing.T) {
	_, ts := newTestServer(t)

	payload := `{
		"name": "good-delivery",
		"enabled": false,
		"schedule": "0 9 * * 1-5",
		"steps": [
			{
				"name": "step-a",
				"position": 0,
				"notify": {
					"on_success": true,
					"deliver_to": [{"type": "space", "space_id": "space-xyz"}]
				}
			}
		]
	}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200 for valid space delivery, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Round-trip: new fields survive YAML save/load
// ---------------------------------------------------------------------------

// TestHandleCreateWorkflow_NewFieldsRoundTrip verifies that the new step-level
// fields — Inputs, Notify.DeliverTo, and the workflow-level Notification.DeliverTo
// — are correctly written to YAML and read back via GET. This catches the class
// of bug where a field is JSON-decoded from the request body but dropped during
// yaml.Marshal because a yaml tag is missing.
func TestHandleCreateWorkflow_NewFieldsRoundTrip(t *testing.T) {
	_, ts := newTestServer(t)

	payload := `{
		"name": "round-trip",
		"enabled": false,
		"schedule": "0 9 * * 1-5",
		"steps": [
			{
				"name": "step-a",
				"agent": "Chris",
				"prompt": "do step A",
				"position": 0
			},
			{
				"name": "step-b",
				"agent": "Chris",
				"prompt": "do step B with {{a_out}}",
				"position": 1,
				"inputs": [{"from_step": "step-a", "as": "a_out"}],
				"notify": {
					"on_success": true,
					"on_failure": true,
					"deliver_to": [
						{"type": "inbox"},
						{"type": "space", "space_id": "sp-1"}
					]
				}
			}
		],
		"notification": {
			"on_success": true,
			"severity": "warning",
			"deliver_to": [{"type": "inbox"}]
		}
	}`

	// POST to create.
	createReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	createReq.Header.Set("Authorization", "Bearer "+testToken)
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatal(err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("create: want 200, got %d", createResp.StatusCode)
	}
	var created map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("no id from create")
	}

	// GET the workflow back.
	getReq, _ := http.NewRequest("GET", ts.URL+"/api/v1/workflows/"+id, nil)
	getReq.Header.Set("Authorization", "Bearer "+testToken)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get: want 200, got %d", getResp.StatusCode)
	}

	var wf map[string]any
	if err := json.NewDecoder(getResp.Body).Decode(&wf); err != nil {
		t.Fatal(err)
	}

	// Verify steps are present.
	steps, _ := wf["steps"].([]any)
	if len(steps) != 2 {
		t.Fatalf("want 2 steps, got %d", len(steps))
	}

	// Verify step-b's inputs round-tripped.
	stepB, _ := steps[1].(map[string]any)
	inputs, _ := stepB["inputs"].([]any)
	if len(inputs) != 1 {
		t.Fatalf("step-b: want 1 input, got %d", len(inputs))
	}
	inp, _ := inputs[0].(map[string]any)
	if inp["from_step"] != "step-a" {
		t.Errorf("step-b input from_step: want step-a, got %v", inp["from_step"])
	}
	if inp["as"] != "a_out" {
		t.Errorf("step-b input as: want a_out, got %v", inp["as"])
	}

	// Verify step-b's notify.deliver_to round-tripped.
	notifyCfg, _ := stepB["notify"].(map[string]any)
	if notifyCfg == nil {
		t.Fatal("step-b notify is nil after round-trip")
	}
	deliverTo, _ := notifyCfg["deliver_to"].([]any)
	if len(deliverTo) != 2 {
		t.Fatalf("step-b notify deliver_to: want 2, got %d", len(deliverTo))
	}
	d1, _ := deliverTo[1].(map[string]any)
	if d1["space_id"] != "sp-1" {
		t.Errorf("step-b deliver_to[1].space_id: want sp-1, got %v", d1["space_id"])
	}

	// Verify workflow-level notification.deliver_to round-tripped.
	wfNotif, _ := wf["notification"].(map[string]any)
	if wfNotif == nil {
		t.Fatal("workflow notification is nil after round-trip")
	}
	wfDeliverTo, _ := wfNotif["deliver_to"].([]any)
	if len(wfDeliverTo) != 1 {
		t.Fatalf("workflow notification deliver_to: want 1, got %d", len(wfDeliverTo))
	}
}

// ---------------------------------------------------------------------------
// Concurrent trigger: 409 when workflow is already running
// ---------------------------------------------------------------------------

// TestHandleRunWorkflow_AlreadyRunning_Returns409 verifies that when a
// workflow is already executing, a second POST to /run returns 409 Conflict
// rather than 500 Internal Server Error. The scheduler guard sets
// ErrWorkflowAlreadyRunning; the handler must detect it via errors.Is.
func TestHandleRunWorkflow_AlreadyRunning_Returns409(t *testing.T) {
	srv, ts := newTestServer(t)

	// Create a workflow so the handler can find it.
	payload := `{"name":"blocking-wf","enabled":false,"schedule":"0 9 * * 1-5","steps":[]}`
	createReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	createReq.Header.Set("Authorization", "Bearer "+testToken)
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatal(err)
	}
	defer createResp.Body.Close()
	var created map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("no id from create")
	}

	// Build a scheduler with a runner that blocks until the test context is done.
	// This simulates a long-running workflow so we can trigger a concurrent
	// second run and observe the 409 response, then unblock on cleanup.
	ready := make(chan struct{})
	unblock := make(chan struct{})
	var runnerCalled sync.Once

	sched := scheduler.New()
	sched.Start()
	t.Cleanup(func() {
		close(unblock)                      // unblock the runner goroutine
		sched.Stop(context.Background())   // wait for cron to drain
	})
	sched.SetWorkflowRunner(func(ctx context.Context, w *scheduler.Workflow) error {
		runnerCalled.Do(func() { close(ready) })
		select {
		case <-unblock:
		case <-ctx.Done():
		}
		return nil
	})
	srv.mu.Lock()
	srv.sched = sched
	srv.mu.Unlock()

	// Use a custom HTTP client without keep-alive so the test server can drain.
	client := &http.Client{
		Transport: &http.Transport{DisableKeepAlives: true},
	}

	// Fire the first trigger in a goroutine — it will block inside the runner.
	go func() {
		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/"+id+"/run", nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		resp, _ := client.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
	}()

	// Wait until the runner has started (workflow is now marked as running).
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for first runner to start")
	}

	// Fire the second trigger — should get 409 because the workflow is running.
	req2, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/"+id+"/run", nil)
	req2.Header.Set("Authorization", "Bearer "+testToken)
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusConflict {
		t.Errorf("want 409 for concurrent trigger, got %d", resp2.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Atomicity: scheduler registration failure rolls back the workflow file
// ---------------------------------------------------------------------------

// TestHandleCreateWorkflow_RegisterFail_RollsBackFile verifies the compensating
// rollback introduced in handleCreateWorkflow: when the scheduler's
// RegisterWorkflow call fails after the YAML file has been written, the handler
// must delete the file so disk and scheduler remain consistent. Without the
// rollback the workflow file would exist on disk but never be scheduled.
func TestHandleCreateWorkflow_RegisterFail_RollsBackFile(t *testing.T) {
	srv, ts := newTestServer(t)

	// Wire a scheduler that has NO workflow runner. RegisterWorkflow will return
	// an error ("workflow runner not configured") for any enabled workflow
	// with a non-empty schedule, triggering the compensating rollback.
	sched := scheduler.New()
	sched.Start()
	t.Cleanup(func() { sched.Stop(context.Background()) })
	// Intentionally do NOT call sched.SetWorkflowRunner — leave it nil.

	srv.mu.Lock()
	srv.sched = sched
	srv.mu.Unlock()

	// POST an enabled workflow with a valid schedule. The file will be written
	// successfully but RegisterWorkflow will fail → the file must be deleted.
	payload := `{"name":"rollback-test","enabled":true,"schedule":"0 9 * * 1-5","steps":[]}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Expect 500 because registration failed.
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", resp.StatusCode)
	}

	// The workflows directory must NOT contain any YAML file — the compensating
	// rollback should have removed the file written by SaveWorkflow.
	workflowsDir := filepath.Join(srv.huginnDir, "workflows")
	entries, readErr := os.ReadDir(workflowsDir)
	if os.IsNotExist(readErr) {
		// Directory was never created — rollback is consistent (file never persisted).
		return
	}
	if readErr != nil {
		t.Fatalf("ReadDir: %v", readErr)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".yml") {
			t.Errorf("workflow file %q should have been deleted by compensating rollback", e.Name())
		}
	}
}
