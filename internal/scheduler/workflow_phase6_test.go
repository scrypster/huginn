package scheduler

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestCloneWorkflow_DeepCopy verifies cloneWorkflow produces a fully
// independent copy — mutating the clone must not poison the original.
func TestCloneWorkflow_DeepCopy(t *testing.T) {
	t.Parallel()
	src := &Workflow{
		ID: "wf-orig", Name: "orig",
		Steps: []WorkflowStep{
			{Position: 1, Name: "a", Agent: "x", Prompt: "p"},
		},
		Tags: []string{"alpha"},
	}
	dst := cloneWorkflow(src)
	if dst == nil {
		t.Fatal("cloneWorkflow returned nil")
	}
	dst.Name = "MUTATED"
	dst.Steps[0].Name = "MUTATED"
	dst.Tags[0] = "MUTATED"
	if src.Name != "orig" {
		t.Errorf("clone leaked Name mutation back to source: %q", src.Name)
	}
	if src.Steps[0].Name != "a" {
		t.Errorf("clone leaked Steps[0].Name mutation: %q", src.Steps[0].Name)
	}
	if src.Tags[0] != "alpha" {
		t.Errorf("clone leaked Tags[0] mutation: %q", src.Tags[0])
	}
}

// TestCloneWorkflow_NilInput returns nil safely.
func TestCloneWorkflow_NilInput(t *testing.T) {
	t.Parallel()
	if cloneWorkflow(nil) != nil {
		t.Fatal("cloneWorkflow(nil) must return nil")
	}
}

// TestCloneWorkflowOrError_NilInput returns an error rather than nil so HTTP
// handlers can return 500 cleanly.
func TestCloneWorkflowOrError_NilInput(t *testing.T) {
	t.Parallel()
	out, err := CloneWorkflowOrError(nil)
	if err == nil {
		t.Fatal("expected error for nil input")
	}
	if out != nil {
		t.Fatal("expected nil output on error")
	}
}

// TestRunner_RecordsTriggerInputsAndSnapshot is the e2e proof that the runner
// captures the trigger inputs and a copy of the workflow on the persisted
// run record. Without this the replay/fork endpoints have nothing to act on.
func TestRunner_RecordsTriggerInputsAndSnapshot(t *testing.T) {
	t.Parallel()
	store := &mockRunStore{}
	agentFn := func(_ context.Context, _ RunOptions) (string, error) { return "ok", nil }
	wf := &Workflow{
		ID: "wf-snap", Name: "snap", Description: "v1",
		Steps: []WorkflowStep{{Position: 1, Name: "a", Agent: "x", Prompt: "p"}},
	}
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)

	ctx := WithInitialInputs(context.Background(), map[string]string{
		"foo": "bar",
		"n":   "42",
	})
	if err := runner(ctx, wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
	if len(store.runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(store.runs))
	}
	run := store.runs[0]
	if run.TriggerInputs["foo"] != "bar" || run.TriggerInputs["n"] != "42" {
		t.Errorf("TriggerInputs not recorded: %#v", run.TriggerInputs)
	}
	if run.WorkflowSnapshot == nil {
		t.Fatal("WorkflowSnapshot was not recorded")
	}
	if run.WorkflowSnapshot.ID != "wf-snap" || run.WorkflowSnapshot.Description != "v1" {
		t.Errorf("snapshot lost fields: %#v", run.WorkflowSnapshot)
	}
	// Mutating the runner's source workflow after the run must NOT affect
	// the persisted snapshot — proves the deep-copy semantics.
	wf.Description = "MUTATED"
	if run.WorkflowSnapshot.Description != "v1" {
		t.Errorf("snapshot is not isolated from source: %q", run.WorkflowSnapshot.Description)
	}
}

// TestRunner_NoInitialInputs_NilTriggerInputs verifies a run started without
// inputs does NOT carry an empty {} map on the persisted record. We always
// want nil-vs-non-nil to mean "no inputs supplied" so dashboards can render
// correctly.
func TestRunner_NoInitialInputs_NilTriggerInputs(t *testing.T) {
	t.Parallel()
	store := &mockRunStore{}
	agentFn := func(_ context.Context, _ RunOptions) (string, error) { return "ok", nil }
	wf := &Workflow{
		ID: "wf-empty", Name: "empty",
		Steps: []WorkflowStep{{Position: 1, Name: "a", Agent: "x", Prompt: "p"}},
	}
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	if err := runner(context.Background(), wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
	if got := store.runs[0].TriggerInputs; got != nil {
		t.Errorf("expected nil TriggerInputs for no-inputs run, got %#v", got)
	}
}

// TestDiffRuns_AlignsByPosition verifies the diff aligns step-by-step by
// Position even when slugs differ, and reports per-field changes.
func TestDiffRuns_AlignsByPosition(t *testing.T) {
	t.Parallel()
	left := &WorkflowRun{
		ID:     "L",
		Status: WorkflowRunStatusComplete,
		Steps: []WorkflowStepResult{
			{Position: 1, Slug: "fetch", Status: "complete", Output: "ok", LatencyMs: 100},
			{Position: 2, Slug: "summarize", Status: "complete", Output: "tldr", LatencyMs: 200},
		},
	}
	right := &WorkflowRun{
		ID:     "R",
		Status: WorkflowRunStatusFailed,
		Steps: []WorkflowStepResult{
			{Position: 1, Slug: "fetch", Status: "complete", Output: "ok-v2", LatencyMs: 110},
			{Position: 2, Slug: "summarize", Status: "failed", Error: "boom", LatencyMs: 50},
		},
	}
	d := DiffRuns(left, right)
	if d.LeftRunID != "L" || d.RightRunID != "R" {
		t.Errorf("ids: %+v", d)
	}
	if !d.StatusChanged {
		t.Error("expected StatusChanged=true")
	}
	if len(d.Steps) != 2 {
		t.Fatalf("expected 2 step rows, got %d", len(d.Steps))
	}
	if !d.Steps[0].Changed || d.Steps[0].OutputLeft != "ok" || d.Steps[0].OutputRight != "ok-v2" {
		t.Errorf("step1 mismatch: %+v", d.Steps[0])
	}
	if !d.Steps[1].Changed || d.Steps[1].StatusLeft != "complete" || d.Steps[1].StatusRight != "failed" {
		t.Errorf("step2 mismatch: %+v", d.Steps[1])
	}
	if d.StepsChangedCount != 2 {
		t.Errorf("StepsChangedCount = %d, want 2", d.StepsChangedCount)
	}
}

// TestDiffRuns_OnlyOnOneSide verifies a run that has more steps than the
// other surfaces them with OnlyIn set — important so the UI can render
// "added/removed" rows.
func TestDiffRuns_OnlyOnOneSide(t *testing.T) {
	t.Parallel()
	left := &WorkflowRun{
		ID: "L",
		Steps: []WorkflowStepResult{
			{Position: 1, Slug: "a", Status: "complete"},
			{Position: 2, Slug: "b", Status: "complete"},
		},
	}
	right := &WorkflowRun{
		ID: "R",
		Steps: []WorkflowStepResult{
			{Position: 1, Slug: "a", Status: "complete"},
		},
	}
	d := DiffRuns(left, right)
	if len(d.Steps) != 2 {
		t.Fatalf("expected 2 rows (union), got %d", len(d.Steps))
	}
	if d.Steps[0].Changed {
		t.Errorf("step1 (identical) should not be Changed: %+v", d.Steps[0])
	}
	if d.Steps[1].OnlyIn != "left" {
		t.Errorf("step2 OnlyIn = %q, want %q", d.Steps[1].OnlyIn, "left")
	}
}

// TestDiffRuns_WorkflowChanged compares the embedded snapshots and reports
// when the underlying workflow definition differs across runs.
func TestDiffRuns_WorkflowChanged(t *testing.T) {
	t.Parallel()
	w1 := &Workflow{ID: "x", Name: "v1"}
	w2 := &Workflow{ID: "x", Name: "v2"}
	left := &WorkflowRun{ID: "L", WorkflowSnapshot: w1}
	right := &WorkflowRun{ID: "R", WorkflowSnapshot: w2}
	d := DiffRuns(left, right)
	if !d.WorkflowChanged {
		t.Error("expected WorkflowChanged=true when snapshots differ")
	}

	// Same snapshot → not changed.
	right2 := &WorkflowRun{ID: "R", WorkflowSnapshot: w1}
	d2 := DiffRuns(left, right2)
	if d2.WorkflowChanged {
		t.Error("expected WorkflowChanged=false when snapshots match")
	}
}

// TestDiffRuns_NilInputs returns an empty diff — handlers should never panic.
func TestDiffRuns_NilInputs(t *testing.T) {
	t.Parallel()
	d := DiffRuns(nil, nil)
	if len(d.Steps) != 0 {
		t.Errorf("nil inputs: expected empty Steps, got %d", len(d.Steps))
	}
}

// TestMergeForkInputs_OverridesWin verifies override values replace base
// values and that nil inputs are tolerated.
func TestMergeForkInputs_OverridesWin(t *testing.T) {
	t.Parallel()
	base := map[string]string{"a": "1", "b": "2"}
	over := map[string]string{"b": "BB", "c": "3"}
	got := MergeForkInputs(base, over)
	want := map[string]string{"a": "1", "b": "BB", "c": "3"}
	if len(got) != 3 || got["a"] != "1" || got["b"] != "BB" || got["c"] != "3" {
		t.Errorf("merge mismatch: got %#v, want %#v", got, want)
	}
}

// TestMergeForkInputs_NilSafe validates nil-safety.
func TestMergeForkInputs_NilSafe(t *testing.T) {
	t.Parallel()
	if got := MergeForkInputs(nil, nil); got == nil {
		t.Fatal("MergeForkInputs(nil,nil) must return non-nil empty map")
	}
	if got := MergeForkInputs(nil, map[string]string{"a": "1"}); got["a"] != "1" {
		t.Errorf("nil base: %#v", got)
	}
	if got := MergeForkInputs(map[string]string{"a": "1"}, nil); got["a"] != "1" {
		t.Errorf("nil over: %#v", got)
	}
}

// TestSnapshotJSONIsRoundtrippable is a forward-compat guard: any future
// addition to Workflow that breaks JSON round-trip would silently corrupt
// snapshots. We marshal/unmarshal a populated definition and assert
// equality.
func TestSnapshotJSONIsRoundtrippable(t *testing.T) {
	t.Parallel()
	src := &Workflow{
		ID: "wf-rt", Name: "rt", Description: "desc", Enabled: true,
		Schedule: "*/5 * * * *", Tags: []string{"t1", "t2"},
		Steps: []WorkflowStep{{
			Position: 1, Name: "a", Agent: "x", Prompt: "p",
			Notify: &StepNotifyConfig{OnSuccess: true},
		}},
		Notification: WorkflowNotificationConfig{
			OnSuccess: true,
			DeliverTo: []NotificationDelivery{{Type: "inbox"}},
		},
		Chain: &WorkflowChainConfig{Next: "downstream", OnSuccess: true},
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
		Version:   1,
		TimeoutMinutes: 30,
	}
	b, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var dst Workflow
	if err := json.Unmarshal(b, &dst); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Deep-equal the marshalled form to guarantee no field was dropped.
	b2, _ := json.Marshal(&dst)
	if string(b) != string(b2) {
		t.Errorf("round-trip diff:\n got=%s\nwant=%s", string(b2), string(b))
	}
}
