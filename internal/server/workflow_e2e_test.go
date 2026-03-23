package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/scrypster/huginn/internal/notification"
	"github.com/scrypster/huginn/internal/scheduler"
)

// TestWorkflowE2E_CreateTriggerRun creates a workflow, triggers it via HTTP,
// and polls for the run to reach status "complete".
func TestWorkflowE2E_CreateTriggerRun(t *testing.T) {
	srv, ts := newTestServer(t)

	// Wire a real workflow run store.
	wfRunStore := scheduler.NewWorkflowRunStore(t.TempDir())
	srv.SetWorkflowRunStore(wfRunStore)

	// Wire a notification store (pebble-backed).
	db, err := pebble.Open(t.TempDir(), &pebble.Options{})
	if err != nil {
		t.Fatalf("pebble open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	srv.SetNotificationStore(notification.NewStore(db))

	// Create a real scheduler wired with a mock runner that immediately marks
	// the run as complete.
	sched := scheduler.New()
	sched.Start()
	t.Cleanup(func() { sched.Stop(context.Background()) })

	var triggeredWorkflowID string
	sched.SetWorkflowRunner(func(ctx context.Context, w *scheduler.Workflow) error {
		triggeredWorkflowID = w.ID
		now := time.Now().UTC()
		run := &scheduler.WorkflowRun{
			ID:          "e2e-run-" + w.ID,
			WorkflowID:  w.ID,
			Status:      scheduler.WorkflowRunStatusComplete,
			StartedAt:   now,
			CompletedAt: &now,
		}
		_ = wfRunStore.Append(w.ID, run)
		return nil
	})

	srv.mu.Lock()
	srv.sched = sched
	srv.mu.Unlock()

	// Step 1: create the workflow via POST.
	// Use "Chris" (a default agent from DefaultAgentsConfig) so the agent-existence check passes.
	createPayload := `{"name":"E2E Test WF","enabled":false,"schedule":"","steps":[{"name":"step1","agent":"Chris","prompt":"do work","position":0}]}`
	createReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(createPayload))
	createReq.Header.Set("Authorization", "Bearer "+testToken)
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("create workflow: want 200, got %d", createResp.StatusCode)
	}
	var created map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	wfID, _ := created["id"].(string)
	if wfID == "" {
		t.Fatal("no id returned from create workflow")
	}

	// Step 2: trigger the workflow.
	triggerReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/"+wfID+"/run", nil)
	triggerReq.Header.Set("Authorization", "Bearer "+testToken)
	triggerResp, err := http.DefaultClient.Do(triggerReq)
	if err != nil {
		t.Fatalf("trigger workflow: %v", err)
	}
	defer triggerResp.Body.Close()
	if triggerResp.StatusCode != http.StatusAccepted && triggerResp.StatusCode != http.StatusOK {
		t.Fatalf("trigger workflow: want 200/202, got %d", triggerResp.StatusCode)
	}

	// Step 3: poll for the run with status "complete".
	client := http.DefaultClient
	deadline := time.Now().Add(2 * time.Second)
	var finalStatus string
	for time.Now().Before(deadline) {
		runsReq, _ := http.NewRequest("GET", ts.URL+"/api/v1/workflows/"+wfID+"/runs", nil)
		runsReq.Header.Set("Authorization", "Bearer "+testToken)
		runsResp, err := client.Do(runsReq)
		if err != nil {
			t.Fatalf("list runs: %v", err)
		}
		var runs []map[string]any
		if err := json.NewDecoder(runsResp.Body).Decode(&runs); err != nil {
			runsResp.Body.Close()
			t.Fatalf("decode runs: %v", err)
		}
		runsResp.Body.Close()
		if len(runs) > 0 {
			finalStatus, _ = runs[0]["status"].(string)
			if finalStatus == "complete" {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	if finalStatus != "complete" {
		t.Errorf("expected run status complete, got %q (workflow_id=%s triggered_id=%s)", finalStatus, wfID, triggeredWorkflowID)
	}
}

// TestWorkflowE2E_StepContextPropagation creates a workflow with two steps where
// step 2 declares an input from step 1, then asserts the structure is persisted.
func TestWorkflowE2E_StepContextPropagation(t *testing.T) {
	_, ts := newTestServer(t)

	// Use agent names from DefaultAgentsConfig() ("Chris", "Steve") so that
	// validateWorkflowAgentsAndConnections passes without an agents.json on disk.
	payload := `{
		"name": "Context Propagation WF",
		"enabled": false,
		"schedule": "",
		"steps": [
			{"name": "step1", "agent": "Chris", "prompt": "do step 1", "position": 0},
			{
				"name": "step2",
				"agent": "Steve",
				"prompt": "use {{inputs.result}}",
				"position": 1,
				"inputs": [{"from_step": "step1", "as": "result"}]
			}
		]
	}`

	createReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	createReq.Header.Set("Authorization", "Bearer "+testToken)
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("create: want 200, got %d", createResp.StatusCode)
	}
	var created map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	wfID, _ := created["id"].(string)
	if wfID == "" {
		t.Fatal("no id from create")
	}

	// Assert the create response has the inputs field intact on step[1].
	steps, _ := created["steps"].([]any)
	if len(steps) < 2 {
		t.Fatalf("want 2 steps in response, got %d", len(steps))
	}
	step2, _ := steps[1].(map[string]any)
	inputs, _ := step2["inputs"].([]any)
	if len(inputs) == 0 {
		t.Fatalf("expected inputs on step[1], got none")
	}
	inp0, _ := inputs[0].(map[string]any)
	fromStep, _ := inp0["from_step"].(string)
	if fromStep != "step1" {
		t.Errorf("expected from_step=step1, got %q", fromStep)
	}

	// Also verify via GET that the workflow is retrievable with inputs intact.
	getReq, _ := http.NewRequest("GET", ts.URL+"/api/v1/workflows/"+wfID, nil)
	getReq.Header.Set("Authorization", "Bearer "+testToken)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get: want 200, got %d", getResp.StatusCode)
	}
	var fetched map[string]any
	if err := json.NewDecoder(getResp.Body).Decode(&fetched); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	fetchedSteps, _ := fetched["steps"].([]any)
	if len(fetchedSteps) < 2 {
		t.Fatalf("GET: want 2 steps, got %d", len(fetchedSteps))
	}
	fetchedStep2, _ := fetchedSteps[1].(map[string]any)
	fetchedInputs, _ := fetchedStep2["inputs"].([]any)
	if len(fetchedInputs) == 0 {
		t.Fatalf("GET: expected inputs on step[1], got none")
	}
	fetchedInp0, _ := fetchedInputs[0].(map[string]any)
	fetchedFromStep, _ := fetchedInp0["from_step"].(string)
	if fetchedFromStep != "step1" {
		t.Errorf("GET: expected from_step=step1, got %q", fetchedFromStep)
	}
}
