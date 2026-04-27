package scheduler

import (
	"context"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// JSON field access in template resolution (Phase 2)
//
// These tests pin the contract for `{{prev.output.field}}` and
// `{{inputs.alias.field}}` placeholders so future refactors can't silently
// regress to literal-string-only behaviour.
// ─────────────────────────────────────────────────────────────────────────────

func TestResolveRuntimeVars_PrevOutputJSONField(t *testing.T) {
	t.Parallel()
	prev := `{"summary":"hello world","count":42,"ok":true}`
	got := resolveRuntimeVars("S: {{prev.output.summary}} | C: {{prev.output.count}} | OK: {{prev.output.ok}}",
		nil, map[string]string{}, prev, nil)
	want := "S: hello world | C: 42 | OK: true"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestResolveRuntimeVars_InputsAliasJSONField(t *testing.T) {
	t.Parallel()
	inputs := []StepInput{{FromStep: "step-a", As: "doc"}}
	outputs := map[string]string{
		"step-a": `{"title":"my report","author":{"name":"Mary"}}`,
	}
	got := resolveRuntimeVars("Title: {{inputs.doc.title}}; Author: {{inputs.doc.author.name}}",
		inputs, outputs, "", nil)
	if got != "Title: my report; Author: Mary" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveRuntimeVars_InputsAliasJSONField_NestedObjectRendersAsJSON(t *testing.T) {
	t.Parallel()
	inputs := []StepInput{{FromStep: "step-a", As: "doc"}}
	outputs := map[string]string{
		"step-a": `{"meta":{"k":"v","n":1}}`,
	}
	got := resolveRuntimeVars("Meta: {{inputs.doc.meta}}", inputs, outputs, "", nil)
	// Object values re-marshal as compact JSON so the next step still gets
	// machine-readable input.
	if !strings.Contains(got, `"k":"v"`) || !strings.Contains(got, `"n":1`) {
		t.Fatalf("expected compact JSON object, got %q", got)
	}
}

func TestResolveRuntimeVars_PrevOutputJSONField_FallsThroughForRaw(t *testing.T) {
	t.Parallel()
	// {{prev.output}} (no field) should still get the raw payload.
	prev := `{"x":1}`
	got := resolveRuntimeVars("RAW={{prev.output}} F={{prev.output.x}}",
		nil, map[string]string{}, prev, nil)
	if got != `RAW={"x":1} F=1` {
		t.Fatalf("got %q", got)
	}
}

func TestResolveRuntimeVars_PrevOutputJSONField_BadJSONLeavesPlaceholder(t *testing.T) {
	t.Parallel()
	got := resolveRuntimeVars("Got {{prev.output.field}}",
		nil, map[string]string{}, "not-json-at-all", nil)
	// The placeholder MUST be left in place so the runner's
	// unresolved-placeholder guard fails the step instead of silently feeding
	// garbage to the next agent.
	if !strings.Contains(got, "{{prev.output.field}}") {
		t.Fatalf("expected placeholder retained on bad JSON, got %q", got)
	}
}

func TestResolveRuntimeVars_PrevOutputJSONField_MissingPathLeavesPlaceholder(t *testing.T) {
	t.Parallel()
	prev := `{"a":1}`
	got := resolveRuntimeVars("Got {{prev.output.b.c}}",
		nil, map[string]string{}, prev, nil)
	if !strings.Contains(got, "{{prev.output.b.c}}") {
		t.Fatalf("expected placeholder retained on missing path, got %q", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Run scratchpad (Phase 2)
//
// Verifies that {{run.scratch.KEY}} substitutions work and that the
// ScratchSetter context plumbing round-trips through the runner so an
// agentFn can write into the live scratchpad and have a later step read it.
// ─────────────────────────────────────────────────────────────────────────────

func TestResolveRuntimeVars_RunScratchSubstitution(t *testing.T) {
	t.Parallel()
	scratch := map[string]string{"feature_flag": "v2", "count": "7"}
	got := resolveRuntimeVars("flag={{run.scratch.feature_flag}} c={{run.scratch.count}}",
		nil, map[string]string{}, "", scratch)
	if got != "flag=v2 c=7" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveRuntimeVars_RunScratch_MissingKeyLeavesPlaceholder(t *testing.T) {
	t.Parallel()
	got := resolveRuntimeVars("k={{run.scratch.missing}}",
		nil, map[string]string{}, "", map[string]string{"present": "yes"})
	if !strings.Contains(got, "{{run.scratch.missing}}") {
		t.Fatalf("expected missing key to leave placeholder, got %q", got)
	}
}

// TestWorkflowRunner_ScratchSetter_RoundTrip verifies an agent that writes via
// ScratchSetter in step 1 sees its value reflected in step 2's prompt via
// {{run.scratch.K}}. This is the end-to-end Phase 2 contract for the
// run-scratchpad: write-once-read-many across linear steps.
func TestWorkflowRunner_ScratchSetter_RoundTrip(t *testing.T) {
	t.Parallel()
	store := &mockRunStore{}

	var step2Prompt string
	agentFn := func(ctx context.Context, opts RunOptions) (string, error) {
		switch opts.StepName {
		case "writer":
			setter := ScratchSetter(ctx)
			if setter == nil {
				t.Fatal("expected scratch setter in step 1 ctx, got nil")
			}
			if err := setter("flag", "enabled"); err != nil {
				t.Fatalf("setter: %v", err)
			}
			return "ok", nil
		case "reader":
			step2Prompt = opts.Prompt
			return "done", nil
		}
		return "", nil
	}

	wf := &Workflow{
		ID:   "wf-scratch",
		Name: "scratch",
		Steps: []WorkflowStep{
			{Position: 1, Name: "writer", Agent: "a", Prompt: "go"},
			{Position: 2, Name: "reader", Agent: "a", Prompt: "flag is {{run.scratch.flag}}"},
		},
	}

	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	if err := runner(context.Background(), wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
	if !strings.Contains(step2Prompt, "flag is enabled") {
		t.Fatalf("step 2 saw prompt %q, want substring 'flag is enabled'", step2Prompt)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// parseOutput-driven notification headlines (Phase 2)
// ─────────────────────────────────────────────────────────────────────────────

func TestParseOutput_PrefersJSONSummary(t *testing.T) {
	t.Parallel()
	in := `{"summary":"All clear"}` + "\nDetails: looked at 12 PRs, none flagged."
	gotSum, gotDet := parseOutput(in)
	if gotSum != "All clear" {
		t.Errorf("summary = %q, want %q", gotSum, "All clear")
	}
	if !strings.Contains(gotDet, "12 PRs") {
		t.Errorf("detail missing expected substring; got %q", gotDet)
	}
}

func TestParseOutput_FallsBackToFirstLine(t *testing.T) {
	t.Parallel()
	in := "Looked at things\nFound nothing"
	gotSum, gotDet := parseOutput(in)
	if gotSum != "Looked at things" {
		t.Errorf("summary = %q", gotSum)
	}
	if gotDet != "Found nothing" {
		t.Errorf("detail = %q", gotDet)
	}
}
