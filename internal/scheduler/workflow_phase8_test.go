package scheduler

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// recordingMetrics is an in-process MetricsCollector used by tests to assert
// the runner emits the expected counters/histograms. It is intentionally
// minimal — no concurrency support is needed because the runner is
// single-goroutine, and we capture a final snapshot via Snapshot().
type recordingMetrics struct {
	mu      sync.Mutex
	records []metricCall
	hists   []metricCall
}

type metricCall struct {
	Metric string
	Value  float64
	Tags   []string
}

func (r *recordingMetrics) Record(metric string, value float64, tags ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, metricCall{Metric: metric, Value: value, Tags: append([]string(nil), tags...)})
}

func (r *recordingMetrics) Histogram(metric string, value float64, tags ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hists = append(r.hists, metricCall{Metric: metric, Value: value, Tags: append([]string(nil), tags...)})
}

func (r *recordingMetrics) hasRecord(metric string, anyTag ...string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.records {
		if e.Metric != metric {
			continue
		}
		if len(anyTag) == 0 {
			return true
		}
		for _, want := range anyTag {
			for _, got := range e.Tags {
				if got == want {
					return true
				}
			}
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// EvaluateWhen
// ─────────────────────────────────────────────────────────────────────────────

func TestEvaluateWhen_Truthy(t *testing.T) {
	t.Parallel()
	cases := []string{
		"true", "TRUE", "1", "yes", "on", "anything", " literal ",
	}
	for _, c := range cases {
		if !EvaluateWhen(c) {
			t.Errorf("EvaluateWhen(%q) = false, want true", c)
		}
	}
}

func TestEvaluateWhen_Falsy(t *testing.T) {
	t.Parallel()
	cases := []string{
		"false", "FALSE", "False", "0", "no", "NO", "off",
	}
	for _, c := range cases {
		if EvaluateWhen(c) {
			t.Errorf("EvaluateWhen(%q) = true, want false", c)
		}
	}
}

func TestEvaluateWhen_EmptyMeansRun(t *testing.T) {
	t.Parallel()
	if !EvaluateWhen("") {
		t.Fatal("empty when must mean 'run' so steps without When still execute")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ApplyRetryDefaults
// ─────────────────────────────────────────────────────────────────────────────

func TestApplyRetryDefaults_FillsZeroValues(t *testing.T) {
	t.Parallel()
	steps := []WorkflowStep{
		{Position: 1, MaxRetries: 0, RetryDelay: ""},
		{Position: 2, MaxRetries: 5, RetryDelay: "10s"}, // already set: untouched
	}
	defaults := &WorkflowRetryConfig{MaxRetries: 3, Delay: "30s"}
	ApplyRetryDefaults(steps, defaults)
	if steps[0].MaxRetries != 3 || steps[0].RetryDelay != "30s" {
		t.Errorf("step 0 not filled: %#v", steps[0])
	}
	if steps[0].RetryDelayDuration().Seconds() != 30 {
		t.Errorf("step 0 retryDelayParsed = %v, want 30s", steps[0].RetryDelayDuration())
	}
	if steps[1].MaxRetries != 5 || steps[1].RetryDelay != "10s" {
		t.Errorf("step 1 should be untouched: %#v", steps[1])
	}
}

func TestApplyRetryDefaults_NilDefaults_NoOp(t *testing.T) {
	t.Parallel()
	steps := []WorkflowStep{{Position: 1}}
	ApplyRetryDefaults(steps, nil)
	if steps[0].MaxRetries != 0 || steps[0].RetryDelay != "" {
		t.Fatalf("nil defaults must not mutate steps: %#v", steps[0])
	}
}

func TestApplyRetryDefaults_EmptyDefaults_NoOp(t *testing.T) {
	t.Parallel()
	steps := []WorkflowStep{{Position: 1}}
	ApplyRetryDefaults(steps, &WorkflowRetryConfig{})
	if steps[0].MaxRetries != 0 || steps[0].RetryDelay != "" {
		t.Fatalf("zero-value defaults must not mutate steps: %#v", steps[0])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Runner: When evaluation
// ─────────────────────────────────────────────────────────────────────────────

func TestRunner_When_FalsyExpressionSkipsStep(t *testing.T) {
	t.Parallel()
	store := &mockRunStore{}

	var calledSteps []string
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		calledSteps = append(calledSteps, opts.StepName)
		return "ok", nil
	}

	wf := &Workflow{
		ID:   "wf-when",
		Name: "when",
		Steps: []WorkflowStep{
			{Position: 1, Name: "always", Agent: "a", Prompt: "p"},
			{Position: 2, Name: "skipped", Agent: "a", Prompt: "p", When: "false"},
			{Position: 3, Name: "tail", Agent: "a", Prompt: "p"},
		},
	}

	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	if err := runner(context.Background(), wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
	want := []string{"always", "tail"}
	if strings.Join(calledSteps, ",") != strings.Join(want, ",") {
		t.Fatalf("called steps = %v, want %v", calledSteps, want)
	}
	if len(store.runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(store.runs))
	}
	run := store.runs[0]
	if len(run.Steps) != 3 {
		t.Fatalf("expected 3 step results (1 skipped), got %d", len(run.Steps))
	}
	if run.Steps[1].Status != "skipped" {
		t.Fatalf("middle step status = %q, want skipped", run.Steps[1].Status)
	}
	if run.Steps[1].SkipReason != "when_false" {
		t.Fatalf("skipped step skip_reason = %q, want when_false", run.Steps[1].SkipReason)
	}
	if run.Steps[1].WhenResolved != "false" {
		t.Fatalf("skipped step when_resolved = %q, want false", run.Steps[1].WhenResolved)
	}
}

func TestRunner_When_ResolvesScratchBeforeEvaluating(t *testing.T) {
	t.Parallel()
	store := &mockRunStore{}

	var ranTail bool
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		if opts.StepName == "tail" {
			ranTail = true
		}
		return "ok", nil
	}

	wf := &Workflow{
		ID:   "wf-when-scratch",
		Name: "scratch-when",
		Steps: []WorkflowStep{
			{Position: 1, Name: "tail", Agent: "a", Prompt: "p", When: "{{run.scratch.allow}}"},
		},
	}

	// initialInputs ctx seeds scratch.
	ctx := WithInitialInputs(context.Background(), map[string]string{"allow": "false"})
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	if err := runner(ctx, wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
	if ranTail {
		t.Fatal("step ran with When=false; should have been skipped")
	}

	// Flip the flag → step runs.
	ranTail = false
	ctx = WithInitialInputs(context.Background(), map[string]string{"allow": "true"})
	if err := runner(ctx, wf); err != nil {
		t.Fatalf("runner 2: %v", err)
	}
	if !ranTail {
		t.Fatal("step did NOT run with When=true; expected execution")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Runner: SubWorkflow
// ─────────────────────────────────────────────────────────────────────────────

func TestRunner_SubWorkflow_NoResolverFailsStep(t *testing.T) {
	t.Parallel()
	store := &mockRunStore{}
	agentFn := func(_ context.Context, _ RunOptions) (string, error) { return "", nil }

	wf := &Workflow{
		ID:   "wf-sub-noresolver",
		Name: "sub",
		Steps: []WorkflowStep{
			{Position: 1, Name: "child", SubWorkflow: "child-wf"},
		},
	}

	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	if err := runner(context.Background(), wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
	if len(store.runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(store.runs))
	}
	if got := store.runs[0].Steps[0].Status; got != "failed" {
		t.Fatalf("step status = %q, want failed (no resolver configured)", got)
	}
	if !strings.Contains(store.runs[0].Steps[0].Error, "sub_workflow support not configured") {
		t.Fatalf("error message = %q, want substring 'sub_workflow support not configured'",
			store.runs[0].Steps[0].Error)
	}
}

func TestRunner_SubWorkflow_OutputFlowsToNextStep(t *testing.T) {
	t.Parallel()
	store := &mockRunStore{}

	var (
		mu             sync.Mutex
		seenInputs     map[string]string
		nextStepPrompt string
	)
	subFn := func(ctx context.Context, id string, inputs map[string]string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		seenInputs = inputs
		if id != "child" {
			return "", errors.New("unexpected id")
		}
		return "child-output", nil
	}
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		if opts.StepName == "after" {
			nextStepPrompt = opts.Prompt
		}
		return "tail-out", nil
	}

	wf := &Workflow{
		ID:   "wf-sub",
		Name: "sub",
		Steps: []WorkflowStep{
			{Position: 1, Name: "child-step", SubWorkflow: "child"},
			{Position: 2, Name: "after", Agent: "a", Prompt: "got: {{prev.output}}"},
		},
	}

	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil,
		WithSubWorkflow(subFn))
	ctx := WithInitialInputs(context.Background(), map[string]string{"key": "val"})
	if err := runner(ctx, wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
	if v, ok := seenInputs["key"]; !ok || v != "val" {
		t.Fatalf("child did not receive parent scratch: %#v", seenInputs)
	}
	if !strings.Contains(nextStepPrompt, "got: child-output") {
		t.Fatalf("next step prompt = %q, want substring 'got: child-output'", nextStepPrompt)
	}
}

func TestRunner_SubWorkflow_ErrorAbortsByDefault(t *testing.T) {
	t.Parallel()
	store := &mockRunStore{}

	subFn := func(_ context.Context, _ string, _ map[string]string) (string, error) {
		return "", errors.New("child blew up")
	}
	var ranTail bool
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		if opts.StepName == "tail" {
			ranTail = true
		}
		return "ok", nil
	}

	wf := &Workflow{
		ID:   "wf-sub-err",
		Name: "sub-err",
		Steps: []WorkflowStep{
			{Position: 1, Name: "child", SubWorkflow: "child"}, // default OnFailure: stop
			{Position: 2, Name: "tail", Agent: "a", Prompt: "p"},
		},
	}

	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil,
		WithSubWorkflow(subFn))
	if err := runner(context.Background(), wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
	if ranTail {
		t.Fatal("tail step ran after sub-workflow error with default on_failure: stop")
	}
}

func TestRunner_SubWorkflow_ContinueOnFailure(t *testing.T) {
	t.Parallel()
	store := &mockRunStore{}

	subFn := func(_ context.Context, _ string, _ map[string]string) (string, error) {
		return "", errors.New("nope")
	}
	var ranTail bool
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		if opts.StepName == "tail" {
			ranTail = true
		}
		return "ok", nil
	}

	wf := &Workflow{
		ID:   "wf-sub-cont",
		Name: "sub-cont",
		Steps: []WorkflowStep{
			{Position: 1, Name: "child", SubWorkflow: "child", OnFailure: "continue"},
			{Position: 2, Name: "tail", Agent: "a", Prompt: "p"},
		},
	}

	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil,
		WithSubWorkflow(subFn))
	if err := runner(context.Background(), wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
	if !ranTail {
		t.Fatal("tail step should have run with on_failure: continue")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Runner: workflow-level retry inheritance
// ─────────────────────────────────────────────────────────────────────────────

func TestRunner_WorkflowRetry_AppliesToStepsWithoutOverrides(t *testing.T) {
	t.Parallel()
	store := &mockRunStore{}

	calls := 0
	agentFn := func(_ context.Context, _ RunOptions) (string, error) {
		calls++
		if calls < 3 {
			return "", errors.New("transient")
		}
		return "ok", nil
	}

	wf := &Workflow{
		ID:    "wf-retry",
		Name:  "retry",
		Retry: &WorkflowRetryConfig{MaxRetries: 2}, // 1 initial + 2 retries = 3 total tries
		Steps: []WorkflowStep{
			{Position: 1, Name: "flaky", Agent: "a", Prompt: "p"},
		},
	}

	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	if err := runner(context.Background(), wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
	if calls != 3 {
		t.Fatalf("agent called %d times, want 3 (initial + 2 retries)", calls)
	}
	if got := store.runs[0].Steps[0].Status; got != "success" {
		t.Fatalf("step status = %q, want success after retries", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Runner: metrics emission
// ─────────────────────────────────────────────────────────────────────────────

func TestRunner_Metrics_EmitsTerminalCounters(t *testing.T) {
	t.Parallel()
	store := &mockRunStore{}
	metrics := &recordingMetrics{}

	agentFn := func(_ context.Context, _ RunOptions) (string, error) { return "ok", nil }

	wf := &Workflow{
		ID:   "wf-metrics",
		Name: "metrics",
		Steps: []WorkflowStep{
			{Position: 1, Name: "s1", Agent: "a", Prompt: "p"},
		},
	}
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil,
		WithMetricsCollector(metrics))
	if err := runner(context.Background(), wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
	if !metrics.hasRecord("huginn.workflow.run.total", "status:complete") {
		t.Fatalf("missing run.total metric with status:complete; got %#v", metrics.records)
	}
	if !metrics.hasRecord("huginn.workflow.step.total", "status:success") {
		t.Fatalf("missing step.total metric with status:success; got %#v", metrics.records)
	}
}

func TestRunner_Metrics_NilCollectorIsSafe(t *testing.T) {
	t.Parallel()
	store := &mockRunStore{}
	agentFn := func(_ context.Context, _ RunOptions) (string, error) { return "ok", nil }
	wf := &Workflow{ID: "wf-nilmetrics", Name: "nm",
		Steps: []WorkflowStep{{Position: 1, Name: "s", Agent: "a", Prompt: "p"}}}
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	if err := runner(context.Background(), wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
}

func TestRunner_WorkflowRetry_StepOverrideWins(t *testing.T) {
	t.Parallel()
	store := &mockRunStore{}

	calls := 0
	agentFn := func(_ context.Context, _ RunOptions) (string, error) {
		calls++
		return "", errors.New("permanent")
	}

	wf := &Workflow{
		ID:    "wf-retry-ovr",
		Name:  "retry-ovr",
		Retry: &WorkflowRetryConfig{MaxRetries: 5},
		Steps: []WorkflowStep{
			// step explicitly sets MaxRetries: 1 → 1 initial + 1 retry = 2 calls.
			{Position: 1, Name: "noisy", Agent: "a", Prompt: "p", MaxRetries: 1},
		},
	}

	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	if err := runner(context.Background(), wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
	if calls != 2 {
		t.Fatalf("agent called %d times, want 2 (step override of MaxRetries=1)", calls)
	}
}
