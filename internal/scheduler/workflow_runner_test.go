package scheduler

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/notification"
)

// mockNotifStore is a minimal in-memory implementation of notification.StoreInterface
// used by tests that need to inspect stored notifications.
type mockNotifStore struct {
	puts []*notification.Notification
}

func (m *mockNotifStore) Put(n *notification.Notification) error {
	// Store a shallow copy so callers cannot mutate what we recorded.
	cp := *n
	m.puts = append(m.puts, &cp)
	return nil
}
func (m *mockNotifStore) Get(id string) (*notification.Notification, error) { return nil, nil }
func (m *mockNotifStore) Transition(id string, s notification.Status) error  { return nil }
func (m *mockNotifStore) ListPending() ([]*notification.Notification, error)  { return nil, nil }
func (m *mockNotifStore) ListByRoutine(id string) ([]*notification.Notification, error) {
	return nil, nil
}
func (m *mockNotifStore) ListByWorkflow(id string) ([]*notification.Notification, error) {
	return nil, nil
}
func (m *mockNotifStore) PendingCount() (int, error)    { return 0, nil }
func (m *mockNotifStore) ExpireRun(id string) error     { return nil }

func newTestRunStore() *WorkflowRunStore {
	dir, err := os.MkdirTemp("", "huginn-test-*")
	if err != nil {
		panic(err)
	}
	return NewWorkflowRunStore(dir)
}

func TestMakeWorkflowRunner_InlineStep_Success(t *testing.T) {
	store := NewWorkflowRunStore(t.TempDir())
	agentCalled := 0
	agentFn := func(ctx context.Context, opts RunOptions) (string, error) {
		agentCalled++
		return `{"summary": "all good"}` + "\nDetails here.", nil
	}
	wfRunner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-inline-1",
		Name: "Inline Test WF",
		Steps: []WorkflowStep{
			{Name: "step-one", Agent: "TestAgent", Prompt: "do the thing", Position: 0},
		},
	}
	if err := wfRunner(context.Background(), w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentCalled != 1 {
		t.Errorf("want agentFn called 1 time, got %d", agentCalled)
	}
	runs, _ := store.List("wf-inline-1", 10)
	if len(runs) == 0 {
		t.Fatal("expected run to be stored")
	}
	if runs[0].Status != WorkflowRunStatusComplete {
		t.Errorf("status want complete, got %s", runs[0].Status)
	}
}

func TestMakeWorkflowRunner_InlineStep_Failure_Stop(t *testing.T) {
	store := NewWorkflowRunStore(t.TempDir())
	calls := 0
	agentFn := func(ctx context.Context, opts RunOptions) (string, error) {
		calls++
		return "", errors.New("agent failed")
	}
	wfRunner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-inline-fail",
		Name: "Fail WF",
		Steps: []WorkflowStep{
			{Name: "step-a", Agent: "A", Prompt: "do A", Position: 0, OnFailure: "stop"},
			{Name: "step-b", Agent: "B", Prompt: "do B", Position: 1},
		},
	}
	_ = wfRunner(context.Background(), w)
	if calls != 1 {
		t.Errorf("want 1 agent call (stopped at failure), got %d", calls)
	}
	runs, _ := store.List("wf-inline-fail", 10)
	if len(runs) == 0 || runs[0].Status != WorkflowRunStatusFailed {
		t.Errorf("expected failed run, got %+v", runs)
	}
}

func TestMakeWorkflowRunner_InlineStep_Failure_Continue(t *testing.T) {
	store := NewWorkflowRunStore(t.TempDir())
	calls := 0
	agentFn := func(ctx context.Context, opts RunOptions) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("step A failed")
		}
		return `{"summary": "ok"}`, nil
	}
	wfRunner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-continue",
		Name: "Continue WF",
		Steps: []WorkflowStep{
			{Name: "step-a", Agent: "A", Prompt: "do A", Position: 0, OnFailure: "continue"},
			{Name: "step-b", Agent: "B", Prompt: "do B", Position: 1},
		},
	}
	_ = wfRunner(context.Background(), w)
	if calls != 2 {
		t.Errorf("want 2 agent calls (continue on failure), got %d", calls)
	}
}

func TestMakeWorkflowRunner_LegacySlug_Fails(t *testing.T) {
	store := NewWorkflowRunStore(t.TempDir())
	agentFn := func(ctx context.Context, opts RunOptions) (string, error) {
		return "", nil
	}
	wfRunner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-legacy",
		Name: "Legacy WF",
		Steps: []WorkflowStep{
			{Routine: "old-routine-slug", Position: 0},
		},
	}
	_ = wfRunner(context.Background(), w)
	runs, _ := store.List("wf-legacy", 10)
	if len(runs) == 0 {
		t.Fatal("expected run stored")
	}
	if runs[0].Steps[0].Status != "failed" {
		t.Errorf("expected legacy slug step to fail, got %s", runs[0].Steps[0].Status)
	}
}

func TestMakeWorkflowRunner_NoPromptNoSlug_Fails(t *testing.T) {
	store := NewWorkflowRunStore(t.TempDir())
	agentFn := func(ctx context.Context, opts RunOptions) (string, error) { return "", nil }
	wfRunner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-empty-step",
		Name: "Empty Step WF",
		Steps: []WorkflowStep{
			{Position: 0}, // no Routine or Prompt
		},
	}
	_ = wfRunner(context.Background(), w)
	runs, _ := store.List("wf-empty-step", 10)
	if len(runs) == 0 || len(runs[0].Steps) == 0 {
		t.Fatal("expected run with step result")
	}
	if runs[0].Steps[0].Status != "failed" {
		t.Errorf("expected empty step to fail, got %s", runs[0].Steps[0].Status)
	}
}

func TestMakeWorkflowRunner_VarSubstitution(t *testing.T) {
	store := NewWorkflowRunStore(t.TempDir())
	var capturedPrompt string
	agentFn := func(ctx context.Context, opts RunOptions) (string, error) {
		capturedPrompt = opts.Prompt
		return `{"summary": "done"}`, nil
	}
	wfRunner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-vars",
		Name: "Vars WF",
		Steps: []WorkflowStep{
			{
				Name:     "step-vars",
				Agent:    "A",
				Prompt:   "check {{REPO}} for issues",
				Vars:     map[string]string{"REPO": "huginn"},
				Position: 0,
			},
		},
	}
	_ = wfRunner(context.Background(), w)
	if capturedPrompt == "" {
		t.Fatal("prompt not captured")
	}
	if !contains(capturedPrompt, "huginn") {
		t.Errorf("expected 'huginn' in prompt, got: %s", capturedPrompt)
	}
}

func TestResolveInlineVars(t *testing.T) {
	result := resolveInlineVars("Hello {{NAME}}, today is {{DATE}}", map[string]string{
		"NAME": "World",
		"DATE": "Monday",
	})
	if result != "Hello World, today is Monday" {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestWorkflowRunner_PrevOutput(t *testing.T) {
	var capturedPrompt string
	callCount := 0
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		callCount++
		if callCount == 2 {
			capturedPrompt = opts.Prompt
		}
		return `{"summary":"ok"}` + "\ndetail", nil
	}
	store := newTestRunStore()
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-prev",
		Name: "prev test",
		Steps: []WorkflowStep{
			{Position: 1, Name: "step-one", Prompt: "do step one"},
			{Position: 2, Name: "step-two", Prompt: "use this: {{prev.output}}"},
		},
	}
	if err := runner(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(capturedPrompt, `{"summary":"ok"}`) {
		t.Errorf("expected prev output in prompt, got: %s", capturedPrompt)
	}
}

func TestWorkflowRunner_NamedInputs(t *testing.T) {
	var capturedPrompt string
	callCount := 0
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		callCount++
		if callCount == 2 {
			capturedPrompt = opts.Prompt
		}
		return `{"summary":"analysis done"}` + "\nfull detail", nil
	}
	store := newTestRunStore()
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-inputs",
		Name: "inputs test",
		Steps: []WorkflowStep{
			{Position: 1, Name: "triage", Prompt: "triage the issue"},
			{Position: 2, Name: "analyze", Prompt: "based on triage: {{inputs.triage_result}}", Inputs: []StepInput{{FromStep: "triage", As: "triage_result"}}},
		},
	}
	if err := runner(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(capturedPrompt, "analysis done") {
		t.Errorf("expected triage output in prompt, got: %s", capturedPrompt)
	}
}

// TestMakeWorkflowRunner_StepOutputTruncated verifies that agent output larger
// than maxStepOutputBytes is capped and the run still completes successfully.
func TestMakeWorkflowRunner_StepOutputTruncated(t *testing.T) {
	store := NewWorkflowRunStore(t.TempDir())
	// Build a string that exceeds 64 KB.
	bigOutput := strings.Repeat("x", maxStepOutputBytes+1024)
	agentFn := func(ctx context.Context, opts RunOptions) (string, error) {
		return bigOutput, nil
	}
	wfRunner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-truncate",
		Name: "Truncate WF",
		Steps: []WorkflowStep{
			{Name: "big-step", Agent: "A", Prompt: "produce big output", Position: 0},
		},
	}
	if err := wfRunner(context.Background(), w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	runs, _ := store.List("wf-truncate", 10)
	if len(runs) == 0 {
		t.Fatal("expected run to be stored")
	}
	run := runs[0]
	if run.Status != WorkflowRunStatusComplete {
		t.Errorf("want complete, got %s", run.Status)
	}
	if len(run.Steps) == 0 {
		t.Fatal("expected at least one step result")
	}
	stepOutput := run.Steps[0].Output
	// Allow maxStepOutputBytes + len("\n[output truncated]") overhead.
	const suffix = "\n[output truncated]"
	maxAllowed := maxStepOutputBytes + len(suffix)
	if len(stepOutput) > maxAllowed {
		t.Errorf("step output not capped: got %d bytes, max allowed %d", len(stepOutput), maxAllowed)
	}
	if !strings.HasSuffix(stepOutput, suffix) {
		t.Errorf("expected truncated output to end with %q, got suffix: %q", suffix, stepOutput[len(stepOutput)-len(suffix):])
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsRune(s, sub))
}

func containsRune(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// resolveRuntimeVars hardening tests
// ---------------------------------------------------------------------------

// TestResolveRuntimeVars_EmptyAs verifies that an input entry with an empty As
// field is silently skipped and does NOT replace "{{inputs.}}" in the prompt.
func TestResolveRuntimeVars_EmptyAs(t *testing.T) {
	inputs := []StepInput{
		{FromStep: "step-one", As: ""},
	}
	stepOutputs := map[string]string{"step-one": "output-value"}
	prompt := "do something {{inputs.}} here"

	result := resolveRuntimeVars(prompt, inputs, stepOutputs, "", nil)

	// The placeholder must be left intact because As was empty.
	if result != prompt {
		t.Errorf("expected prompt unchanged, got: %q", result)
	}
}

// TestResolveRuntimeVars_MissingStep verifies that an input whose FromStep does
// not exist in stepOutputs leaves the {{inputs.alias}} placeholder unreplaced.
func TestResolveRuntimeVars_MissingStep(t *testing.T) {
	inputs := []StepInput{
		{FromStep: "nonexistent-step", As: "result"},
	}
	stepOutputs := map[string]string{"step-one": "something"}
	prompt := "based on: {{inputs.result}}"

	result := resolveRuntimeVars(prompt, inputs, stepOutputs, "", nil)

	if result != prompt {
		t.Errorf("expected placeholder left unreplaced, got: %q", result)
	}
}

// TestResolveRuntimeVars_EmptyFromStep verifies that an input entry with an
// empty FromStep is skipped and the placeholder remains in the prompt.
func TestResolveRuntimeVars_EmptyFromStep(t *testing.T) {
	inputs := []StepInput{
		{FromStep: "", As: "alias"},
	}
	stepOutputs := map[string]string{}
	prompt := "use {{inputs.alias}} here"

	result := resolveRuntimeVars(prompt, inputs, stepOutputs, "", nil)

	if result != prompt {
		t.Errorf("expected placeholder left unreplaced, got: %q", result)
	}
}

// TestResolveRuntimeVars_FirstStepPrevOutput verifies that {{prev.output}} on the
// very first step (prevOutput == "") is replaced with an empty string, not left as
// a literal placeholder.
func TestResolveRuntimeVars_FirstStepPrevOutput(t *testing.T) {
	prompt := "use this: {{prev.output}} end"
	result := resolveRuntimeVars(prompt, nil, map[string]string{}, "", nil)

	want := "use this:  end"
	if result != want {
		t.Errorf("want %q, got %q", want, result)
	}
}

// ---------------------------------------------------------------------------
// buildWorkflowRunDetail tests
// ---------------------------------------------------------------------------

// TestBuildWorkflowRunDetail_EmptySteps ensures the function returns a
// non-empty, human-readable string when there are no step results.
func TestBuildWorkflowRunDetail_EmptySteps(t *testing.T) {
	run := &WorkflowRun{ID: "run-abc-123", Steps: nil}
	detail := buildWorkflowRunDetail(run)

	if detail == "" {
		t.Fatal("expected non-empty detail for run with no steps")
	}
	if !strings.Contains(detail, "run-abc-123") {
		t.Errorf("expected run ID in detail, got: %s", detail)
	}
	if !strings.Contains(detail, "no steps") {
		t.Errorf("expected 'no steps' note in detail, got: %s", detail)
	}
}

// TestBuildWorkflowRunDetail_SkippedStatus verifies that a step with status
// "skipped" renders with the "~" icon rather than the success icon.
func TestBuildWorkflowRunDetail_SkippedStatus(t *testing.T) {
	run := &WorkflowRun{
		ID: "run-skip",
		Steps: []WorkflowStepResult{
			{Position: 0, Slug: "step-a", Status: "skipped"},
		},
	}
	detail := buildWorkflowRunDetail(run)
	if !strings.Contains(detail, "~") {
		t.Errorf("expected '~' icon for skipped step, got: %s", detail)
	}
}

// TestBuildWorkflowRunDetail_SuccessAndFailedIcons ensures ✓ and ✗ are used
// for "success" and "failed" step statuses respectively.
func TestBuildWorkflowRunDetail_SuccessAndFailedIcons(t *testing.T) {
	run := &WorkflowRun{
		ID: "run-icons",
		Steps: []WorkflowStepResult{
			{Position: 0, Slug: "ok", Status: "success"},
			{Position: 1, Slug: "bad", Status: "failed", Error: "boom"},
		},
	}
	detail := buildWorkflowRunDetail(run)
	if !strings.Contains(detail, "✓") {
		t.Errorf("expected ✓ for success step, got: %s", detail)
	}
	if !strings.Contains(detail, "✗") {
		t.Errorf("expected ✗ for failed step, got: %s", detail)
	}
	if !strings.Contains(detail, "boom") {
		t.Errorf("expected error text in detail, got: %s", detail)
	}
}

// ---------------------------------------------------------------------------
// dispatchNotification tests
// ---------------------------------------------------------------------------

// TestDispatchNotification_NilTargets verifies that even with a nil targets
// slice the inbox delivery record is always included.
func TestDispatchNotification_NilTargets(t *testing.T) {
	n := &notification.Notification{
		ID:      "notif-1",
		Summary: "test",
	}
	records := dispatchNotification(n, nil, nil, nil, nil, "", "", nil, nil, nil, "")

	if len(records) != 1 {
		t.Fatalf("expected 1 record (inbox), got %d", len(records))
	}
	if records[0].Type != "inbox" {
		t.Errorf("expected inbox record, got type=%q", records[0].Type)
	}
	if records[0].Status != "sent" {
		t.Errorf("expected status=sent, got %q", records[0].Status)
	}
}

// TestDispatchNotification_SpaceDeliveryError verifies that a failing
// spaceDeliveryFn records status="failed" and does not panic.
func TestDispatchNotification_SpaceDeliveryError(t *testing.T) {
	n := &notification.Notification{ID: "notif-2", Summary: "hi"}
	targets := []NotificationDelivery{{Type: "space", SpaceID: "space-99"}}
	spaceErr := errors.New("space unavailable")
	records := dispatchNotification(n, targets, nil, func(spaceID, summary, detail string) error {
		return spaceErr
	}, nil, "", "", nil, nil, nil, "")

	if len(records) != 2 {
		t.Fatalf("expected 2 records (inbox + space), got %d", len(records))
	}
	spaceRec := records[1]
	if spaceRec.Status != "failed" {
		t.Errorf("expected space record status=failed, got %q", spaceRec.Status)
	}
	if spaceRec.Error != spaceErr.Error() {
		t.Errorf("expected error text %q, got %q", spaceErr.Error(), spaceRec.Error)
	}
}

// ---------------------------------------------------------------------------
// Notification ordering test: Deliveries must be set before Put
// ---------------------------------------------------------------------------

// TestWorkflowRunner_NotificationOrderCorrect verifies that when a
// workflow-level notification is persisted, n.Deliveries is already populated
// (i.e. dispatchNotification was called before notifStore.Put).
func TestWorkflowRunner_NotificationOrderCorrect(t *testing.T) {
	store := NewWorkflowRunStore(t.TempDir())
	notifStore := &mockNotifStore{}

	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		return `{"summary":"done"}` + "\ndetail", nil
	}

	runner := MakeWorkflowRunner(store, agentFn, notifStore, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-notif-order",
		Name: "NotifOrder",
		Steps: []WorkflowStep{
			{Position: 0, Name: "step-one", Agent: "A", Prompt: "do it"},
		},
		Notification: WorkflowNotificationConfig{
			OnSuccess: true,
			Severity:  "info",
		},
	}

	if err := runner(context.Background(), w); err != nil {
		t.Fatalf("runner error: %v", err)
	}

	if len(notifStore.puts) == 0 {
		t.Fatal("expected at least one notification to be Put")
	}
	for i, n := range notifStore.puts {
		if len(n.Deliveries) == 0 {
			t.Errorf("notification[%d] was Put with empty Deliveries — order bug", i)
		}
	}
}

// ---------------------------------------------------------------------------
// Round 2 hardening: multi-step failure propagation
// ---------------------------------------------------------------------------

// TestMakeWorkflowRunner_ContinueOnFailure_StatusPartial verifies that when a
// step fails with on_failure: continue and the remaining steps all succeed,
// the run gets WorkflowRunStatusPartial (not "complete"). This distinguishes a
// clean run from one where some steps were skipped/failed.
func TestMakeWorkflowRunner_ContinueOnFailure_StatusPartial(t *testing.T) {
	store := NewWorkflowRunStore(t.TempDir())
	call := 0
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		call++
		if call == 1 {
			return "", errors.New("step one failed")
		}
		return `{"summary":"ok"}`, nil
	}
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-partial",
		Name: "Partial WF",
		Steps: []WorkflowStep{
			{Name: "step-a", Agent: "A", Prompt: "do A", Position: 0, OnFailure: "continue"},
			{Name: "step-b", Agent: "B", Prompt: "do B", Position: 1},
		},
	}
	if err := runner(context.Background(), w); err != nil {
		t.Fatalf("unexpected runner error: %v", err)
	}
	runs, _ := store.List("wf-partial", 10)
	if len(runs) == 0 {
		t.Fatal("expected run stored")
	}
	if runs[0].Status != WorkflowRunStatusPartial {
		t.Errorf("want status partial, got %s", runs[0].Status)
	}
	// Both steps must have been attempted.
	if len(runs[0].Steps) != 2 {
		t.Errorf("want 2 step results, got %d", len(runs[0].Steps))
	}
}

// TestMakeWorkflowRunner_AllSuccess_StatusComplete confirms that when every
// step succeeds the run status remains WorkflowRunStatusComplete (the partial
// path must not trigger on a clean run).
func TestMakeWorkflowRunner_AllSuccess_StatusComplete(t *testing.T) {
	store := NewWorkflowRunStore(t.TempDir())
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		return `{"summary":"ok"}`, nil
	}
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-all-ok",
		Name: "All OK WF",
		Steps: []WorkflowStep{
			{Name: "s1", Agent: "A", Prompt: "do A", Position: 0},
			{Name: "s2", Agent: "B", Prompt: "do B", Position: 1},
		},
	}
	_ = runner(context.Background(), w)
	runs, _ := store.List("wf-all-ok", 10)
	if len(runs) == 0 || runs[0].Status != WorkflowRunStatusComplete {
		t.Errorf("want complete, got %+v", runs)
	}
}

// TestMakeWorkflowRunner_PrevOutputSkipsFailedStep verifies that when a step
// fails with on_failure: continue the next step's {{prev.output}} resolves to
// the output of the step BEFORE the failed one (not the failed step's error).
func TestMakeWorkflowRunner_PrevOutputSkipsFailedStep(t *testing.T) {
	var capturedPrompt string
	call := 0
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		call++
		switch call {
		case 1:
			return "output-from-step-1", nil
		case 2:
			return "", errors.New("step 2 bombed")
		case 3:
			capturedPrompt = opts.Prompt
			return `{"summary":"ok"}`, nil
		}
		return "", nil
	}
	store := NewWorkflowRunStore(t.TempDir())
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-prev-skip",
		Name: "PrevSkip WF",
		Steps: []WorkflowStep{
			{Name: "s1", Agent: "A", Prompt: "step 1", Position: 0},
			{Name: "s2", Agent: "B", Prompt: "step 2", Position: 1, OnFailure: "continue"},
			{Name: "s3", Agent: "C", Prompt: "result: {{prev.output}}", Position: 2},
		},
	}
	_ = runner(context.Background(), w)
	if capturedPrompt == "" {
		t.Fatal("step 3 was not called")
	}
	if !strings.Contains(capturedPrompt, "output-from-step-1") {
		t.Errorf("{{prev.output}} should resolve to step-1 output, got prompt: %s", capturedPrompt)
	}
}

// ---------------------------------------------------------------------------
// Round 2 hardening: context cancellation
// ---------------------------------------------------------------------------

// TestMakeWorkflowRunner_ContextCancelledMidRun verifies that when the context
// is cancelled before steps begin the runner short-circuits and marks the run
// as failed rather than executing all steps.
func TestMakeWorkflowRunner_ContextCancelledMidRun(t *testing.T) {
	store := NewWorkflowRunStore(t.TempDir())
	calls := 0
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		calls++
		return `{"summary":"ok"}`, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel before running.
	cancel()

	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-ctx-cancel",
		Name: "Cancel WF",
		Steps: []WorkflowStep{
			{Name: "s1", Agent: "A", Prompt: "step 1", Position: 0},
			{Name: "s2", Agent: "B", Prompt: "step 2", Position: 1},
		},
	}
	_ = runner(ctx, w)

	if calls != 0 {
		t.Errorf("want 0 agent calls on pre-cancelled context, got %d", calls)
	}
	runs, _ := store.List("wf-ctx-cancel", 10)
	if len(runs) == 0 {
		t.Fatal("expected run persisted even on cancellation")
	}
	if runs[0].Status != WorkflowRunStatusFailed {
		t.Errorf("want failed status on cancelled context, got %s", runs[0].Status)
	}
}

// ---------------------------------------------------------------------------
// Round 2 hardening: step notification Detail for success path
// ---------------------------------------------------------------------------

// TestDispatchNotification_StepSuccessDetail verifies that when a step succeeds
// the notification Detail is populated with the agent output (not empty string).
// A space delivery with empty Detail is valid, but the caller should still set
// Detail so the notification record is informative in the inbox.
func TestDispatchNotification_StepSuccessDetail(t *testing.T) {
	store := NewWorkflowRunStore(t.TempDir())
	notifStore := &mockNotifStore{}

	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		return "the agent output", nil
	}

	runner := MakeWorkflowRunner(store, agentFn, notifStore, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-step-detail",
		Name: "StepDetail WF",
		Steps: []WorkflowStep{
			{
				Position: 0,
				Name:     "step-one",
				Agent:    "A",
				Prompt:   "do it",
				Notify: &StepNotifyConfig{
					OnSuccess: true,
				},
			},
		},
	}

	if err := runner(context.Background(), w); err != nil {
		t.Fatalf("runner error: %v", err)
	}

	if len(notifStore.puts) == 0 {
		t.Fatal("expected notification to be Put")
	}
	n := notifStore.puts[0]
	if n.Detail == "" {
		t.Error("step success notification Detail must not be empty when agent returned output")
	}
	if n.Detail != "the agent output" {
		t.Errorf("step success notification Detail want %q, got %q", "the agent output", n.Detail)
	}
}

// ---------------------------------------------------------------------------
// SpaceDeliveryFunc success path
// ---------------------------------------------------------------------------

// TestDispatchNotification_SpaceDeliverySuccess verifies that a successful
// spaceDeliveryFn is called with the correct spaceID and records status="sent".
func TestDispatchNotification_SpaceDeliverySuccess(t *testing.T) {
	var called bool
	var calledSpace string
	n := &notification.Notification{ID: "notif-space-ok", Summary: "done"}
	targets := []NotificationDelivery{{Type: "space", SpaceID: "space-xyz"}}
	recs := dispatchNotification(n, targets, nil, func(spaceID, summary, detail string) error {
		called = true
		calledSpace = spaceID
		return nil
	}, nil, "", "", nil, nil, nil, "")
	if !called {
		t.Error("expected spaceDeliveryFn to be called")
	}
	if calledSpace != "space-xyz" {
		t.Errorf("expected space-xyz, got %q", calledSpace)
	}
	// recs[0] = inbox record, recs[1] = space record
	if len(recs) < 2 {
		t.Fatalf("expected at least 2 records, got %d", len(recs))
	}
	if recs[1].Status != "sent" {
		t.Errorf("expected space record status=sent, got %q", recs[1].Status)
	}
}

// ---------------------------------------------------------------------------
// Silent-swallow resilience tests
// ---------------------------------------------------------------------------

// failingNotifStore embeds mockNotifStore but returns an error from Put.
type failingNotifStore struct{ mockNotifStore }

func (f *failingNotifStore) Put(n *notification.Notification) error {
	return errors.New("disk full")
}

// TestMakeWorkflowRunner_NotifStorePutFailure_DoesNotPanic confirms the runner
// returns nil even when notifStore.Put silently fails (error is discarded).
func TestMakeWorkflowRunner_NotifStorePutFailure_DoesNotPanic(t *testing.T) {
	store := newTestRunStore()
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		return `{"summary":"ok"}`, nil
	}
	wfRunner := MakeWorkflowRunner(store, agentFn, &failingNotifStore{}, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-notif-fail",
		Name: "Test",
		Steps: []WorkflowStep{
			{Name: "s1", Agent: "A", Prompt: "p", Position: 0},
		},
	}
	if err := wfRunner(context.Background(), w); err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
}

// failingRunStore implements WorkflowRunStoreInterface with Append always failing.
type failingRunStore struct{}

func (f *failingRunStore) Append(workflowID string, run *WorkflowRun) error {
	return errors.New("io error")
}
func (f *failingRunStore) List(workflowID string, n int) ([]*WorkflowRun, error) {
	return nil, nil
}
func (f *failingRunStore) Get(workflowID, runID string) (*WorkflowRun, error) {
	return nil, nil
}

// TestMakeWorkflowRunner_RunStoreAppendFailure_ContinuesExecution confirms the
// runner returns nil even when runStore.Append fails (error is logged, not propagated).
func TestMakeWorkflowRunner_RunStoreAppendFailure_ContinuesExecution(t *testing.T) {
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		return `{"summary":"ok"}`, nil
	}
	wfRunner := MakeWorkflowRunner(&failingRunStore{}, agentFn, nil, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-run-fail",
		Name: "Test",
		Steps: []WorkflowStep{
			{Name: "s1", Agent: "A", Prompt: "p", Position: 0},
		},
	}
	if err := wfRunner(context.Background(), w); err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
}
