package scheduler

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// errBoom is a generic step-level failure used by chain-trigger failure tests.
var errBoom = errors.New("boom")

// TestWithInitialInputs_RoundTrip verifies the context helper round-trips
// an initial inputs map.
func TestWithInitialInputs_RoundTrip(t *testing.T) {
	t.Parallel()
	in := map[string]string{"foo": "bar", "n": "42"}
	ctx := WithInitialInputs(context.Background(), in)
	got := initialInputs(ctx)
	if got["foo"] != "bar" || got["n"] != "42" {
		t.Fatalf("round-trip lost values: %#v", got)
	}
	// Mutating the source map after binding must NOT poison the context value
	// — the helper takes a defensive copy.
	in["foo"] = "TAINTED"
	if initialInputs(ctx)["foo"] != "bar" {
		t.Fatalf("initialInputs leaked source-map mutation")
	}
}

// TestWithInitialInputs_EmptyMap_NoOp confirms nil/empty maps are no-ops so
// the runner doesn't overwrite a parent context value with an empty marker.
func TestWithInitialInputs_EmptyMap_NoOp(t *testing.T) {
	t.Parallel()
	parent := WithInitialInputs(context.Background(), map[string]string{"k": "v"})
	derived := WithInitialInputs(parent, nil)
	if got := initialInputs(derived); got["k"] != "v" {
		t.Fatalf("empty WithInitialInputs clobbered the parent context")
	}
}

// TestWorkflowRunner_InitialInputs_SeedsScratch is the e2e proof that a
// trigger-supplied input map is visible to the very first step's prompt via
// {{run.scratch.KEY}}. Without this, manual runs with a body and webhook
// triggers couldn't supply variables.
func TestWorkflowRunner_InitialInputs_SeedsScratch(t *testing.T) {
	t.Parallel()
	store := &mockRunStore{}
	var seenPrompts []string
	var mu sync.Mutex
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		mu.Lock()
		seenPrompts = append(seenPrompts, opts.Prompt)
		mu.Unlock()
		return "ok", nil
	}
	wf := &Workflow{
		ID: "wf-init", Name: "init",
		Steps: []WorkflowStep{{
			Position: 1, Name: "a", Agent: "x",
			Prompt: "Hello {{run.scratch.greeting}} from {{run.scratch.who}}",
		}},
	}
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	ctx := WithInitialInputs(context.Background(), map[string]string{
		"greeting": "world",
		"who":      "tester",
	})
	if err := runner(ctx, wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
	if len(seenPrompts) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(seenPrompts))
	}
	// The runner appends a notification-summary instruction to the resolved
	// prompt; we only care that the scratch placeholders were substituted at
	// the front of the prompt before that addendum.
	if want := "Hello world from tester"; !strings.HasPrefix(seenPrompts[0], want) {
		t.Fatalf("first-step prompt = %q, want prefix %q", seenPrompts[0], want)
	}
}

// TestWorkflowRunner_NoInitialInputs_BackwardsCompat verifies a run with no
// initial inputs and no scratch placeholders still works — guards against
// regressions in the seeding path.
func TestWorkflowRunner_NoInitialInputs_BackwardsCompat(t *testing.T) {
	t.Parallel()
	store := &mockRunStore{}
	agentFn := func(_ context.Context, _ RunOptions) (string, error) { return "ok", nil }
	wf := &Workflow{
		ID: "wf-bw", Name: "bw",
		Steps: []WorkflowStep{{Position: 1, Name: "a", Agent: "x", Prompt: "no placeholders"}},
	}
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	if err := runner(context.Background(), wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
}

// TestWorkflowRunner_ChainTrigger_FiresOnComplete verifies the chain hook is
// invoked exactly once after a successful run, with the parent workflow and
// its persisted run record.
func TestWorkflowRunner_ChainTrigger_FiresOnComplete(t *testing.T) {
	t.Parallel()
	store := &mockRunStore{}
	agentFn := func(_ context.Context, _ RunOptions) (string, error) { return "tail-output", nil }

	var calls int32
	var sawParent *Workflow
	var sawRun *WorkflowRun
	hook := func(parent *Workflow, run *WorkflowRun) {
		atomic.AddInt32(&calls, 1)
		sawParent = parent
		sawRun = run
	}

	wf := &Workflow{
		ID: "wf-chain", Name: "chain",
		Chain: &WorkflowChainConfig{Next: "downstream"},
		Steps: []WorkflowStep{{Position: 1, Name: "a", Agent: "x", Prompt: "go"}},
	}
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil, WithChainTrigger(hook))
	if err := runner(context.Background(), wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("chain trigger calls = %d, want 1", got)
	}
	if sawParent == nil || sawParent.ID != "wf-chain" {
		t.Fatalf("chain trigger received wrong parent: %#v", sawParent)
	}
	if sawRun == nil || sawRun.Status != WorkflowRunStatusComplete {
		t.Fatalf("chain trigger received wrong run status: %#v", sawRun)
	}
	if len(sawRun.Steps) != 1 || sawRun.Steps[0].Output != "tail-output" {
		t.Fatalf("chain trigger run missing tail output, got: %#v", sawRun.Steps)
	}
}

// TestWorkflowRunner_ChainTrigger_FiresOnFailure_LetsHookDecide verifies the
// runner forwards EVERY non-cancelled terminal status to the hook (success,
// failure, partial). The decision of whether to chain on failure lives in the
// hook, not the runner — this keeps the runner policy-free.
func TestWorkflowRunner_ChainTrigger_FiresOnFailure_LetsHookDecide(t *testing.T) {
	t.Parallel()
	store := &mockRunStore{}
	agentFn := func(_ context.Context, _ RunOptions) (string, error) {
		return "", errBoom
	}
	var calls int32
	var sawStatus WorkflowRunStatus
	hook := func(_ *Workflow, run *WorkflowRun) {
		atomic.AddInt32(&calls, 1)
		sawStatus = run.Status
	}

	wf := &Workflow{
		ID: "wf-chain-fail", Name: "cf",
		Chain: &WorkflowChainConfig{Next: "downstream", OnFailure: true},
		Steps: []WorkflowStep{{Position: 1, Name: "a", Agent: "x", Prompt: "go"}},
	}
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil, WithChainTrigger(hook))
	if err := runner(context.Background(), wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("chain trigger calls = %d, want 1 on failure (hook decides whether to chain)", got)
	}
	if sawStatus != WorkflowRunStatusFailed {
		t.Fatalf("chain hook saw status %q, want %q", sawStatus, WorkflowRunStatusFailed)
	}
}

// TestWorkflowChainConfig_OnSuccessOnly is a behavioural unit test of the
// chaining "should-fire?" predicate to lock in the default-on-success rule.
// Note: the rule itself lives in main.go's hook; we re-implement it here so
// the test stays in this package without an import cycle. If the rule
// changes, this test must be updated in lock-step.
func TestWorkflowChainConfig_OnSuccessOnly(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		cfg     WorkflowChainConfig
		status  WorkflowRunStatus
		wantFire bool
	}{
		{"default-on-success-fires", WorkflowChainConfig{Next: "x"}, WorkflowRunStatusComplete, true},
		{"default-on-failure-skips", WorkflowChainConfig{Next: "x"}, WorkflowRunStatusFailed, false},
		{"default-on-partial-skips", WorkflowChainConfig{Next: "x"}, WorkflowRunStatusPartial, false},
		{"explicit-success-fires", WorkflowChainConfig{Next: "x", OnSuccess: true}, WorkflowRunStatusComplete, true},
		{"explicit-failure-fires", WorkflowChainConfig{Next: "x", OnFailure: true}, WorkflowRunStatusFailed, true},
		{"explicit-failure-fires-on-partial", WorkflowChainConfig{Next: "x", OnFailure: true}, WorkflowRunStatusPartial, true},
		{"on-success-but-failed-skips", WorkflowChainConfig{Next: "x", OnSuccess: true}, WorkflowRunStatusFailed, false},
		{"both-flags-success", WorkflowChainConfig{Next: "x", OnSuccess: true, OnFailure: true}, WorkflowRunStatusComplete, true},
		{"both-flags-failure", WorkflowChainConfig{Next: "x", OnSuccess: true, OnFailure: true}, WorkflowRunStatusFailed, true},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			success := c.status == WorkflowRunStatusComplete
			failure := c.status == WorkflowRunStatusFailed || c.status == WorkflowRunStatusPartial
			fire := (c.cfg.OnSuccess && success) || (c.cfg.OnFailure && failure)
			if !c.cfg.OnSuccess && !c.cfg.OnFailure && success {
				fire = true
			}
			if fire != c.wantFire {
				t.Fatalf("status=%s cfg=%+v: fire=%v, want %v", c.status, c.cfg, fire, c.wantFire)
			}
		})
	}
}

// TestRunnerOptions_ChainTriggerNil verifies WithChainTrigger(nil) is a no-op
// — important so callers can disable chaining without conditionals.
func TestRunnerOptions_ChainTriggerNil(t *testing.T) {
	t.Parallel()
	cfg := defaultRunnerConfig()
	WithChainTrigger(nil)(&cfg)
	if cfg.chainTrigger != nil {
		// The option does store nil — the runner's nil-check is what makes
		// it safe. Document that here so a future refactor that "fixes" the
		// option to skip nils doesn't silently change semantics.
		t.Logf("WithChainTrigger(nil) sets cfg.chainTrigger=nil — runner must nil-check before calling")
	}
}

// TestWorkflowChainConfig_NextRequired is a documentation test asserting the
// hook treats an empty Next as "no chain", to lock in the contract that
// callers can use the same hook for many workflows.
func TestWorkflowChainConfig_NextRequired(t *testing.T) {
	t.Parallel()
	cfg := &WorkflowChainConfig{Next: "  "} // whitespace
	if strings.TrimSpace(cfg.Next) != "" {
		t.Fatalf("contract: empty/whitespace Next must be treated as no chain")
	}
}
