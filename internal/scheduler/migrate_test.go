package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigrateRoutinesToWorkflows_ProducesInlineSteps verifies that the migration
// produces inline steps (Agent + Prompt set) rather than legacy Routine slug
// references that the runner rejects.
//
// Regression for the migration trap: prior versions wrote {Routine: slug}
// steps which workflow_runner.go rejected at runtime with
// "legacy routine slug references not supported".
func TestMigrateRoutinesToWorkflows_ProducesInlineSteps(t *testing.T) {
	tmpRoot := t.TempDir()
	routineDir := filepath.Join(tmpRoot, "routines")
	workflowDir := filepath.Join(tmpRoot, "workflows")
	if err := os.MkdirAll(routineDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write a representative legacy routine YAML.
	legacy := `id: r-001
name: PR Review
description: Reviews open PRs
enabled: true
trigger:
  mode: schedule
  cron: "0 9 * * 1-5"
agent: Chris
prompt: |
  Review open PRs targeting {{TARGET_BRANCH}}.
notification:
  severity: warning
vars:
  TARGET_BRANCH:
    default: main
connections:
  github: gh-default
`
	if err := os.WriteFile(filepath.Join(routineDir, "pr-review.yaml"), []byte(legacy), 0644); err != nil {
		t.Fatalf("write legacy routine: %v", err)
	}

	if err := MigrateRoutinesToWorkflows(routineDir, workflowDir); err != nil {
		t.Fatalf("MigrateRoutinesToWorkflows: %v", err)
	}

	// The routines dir should be renamed.
	if _, err := os.Stat(routineDir); !os.IsNotExist(err) {
		t.Errorf("expected routines dir to be renamed, got: %v", err)
	}
	if _, err := os.Stat(routineDir + ".bak"); err != nil {
		t.Errorf("expected routines.bak dir, got: %v", err)
	}

	// Load the migrated workflow.
	wfs, err := LoadWorkflows(workflowDir)
	if err != nil {
		t.Fatalf("LoadWorkflows: %v", err)
	}
	if len(wfs) != 1 {
		t.Fatalf("expected 1 migrated workflow, got %d", len(wfs))
	}
	w := wfs[0]

	if w.Name != "PR Review" {
		t.Errorf("Name = %q, want %q", w.Name, "PR Review")
	}
	if w.Schedule != "0 9 * * 1-5" {
		t.Errorf("Schedule = %q, want %q", w.Schedule, "0 9 * * 1-5")
	}
	if !w.Enabled {
		t.Error("Enabled = false, want true")
	}

	if len(w.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(w.Steps))
	}
	s := w.Steps[0]

	// THE BUG: previously this would be set to a slug and the rest empty.
	if s.Routine != "" {
		t.Errorf("step.Routine = %q, expected empty (legacy slug references are rejected at runtime)", s.Routine)
	}
	if s.Agent != "Chris" {
		t.Errorf("step.Agent = %q, want Chris", s.Agent)
	}
	if !strings.Contains(s.Prompt, "Review open PRs") {
		t.Errorf("step.Prompt missing routine prompt body: %q", s.Prompt)
	}
	if got := s.Vars["TARGET_BRANCH"]; got != "main" {
		t.Errorf("step.Vars[TARGET_BRANCH] = %q, want main", got)
	}
	if got := s.Connections["github"]; got != "gh-default" {
		t.Errorf("step.Connections[github] = %q, want gh-default", got)
	}

	// Most importantly: running the migrated workflow should NOT trip the
	// legacy-slug rejection in the runner.
	store := &mockRunStore{}
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		if opts.AgentName != "Chris" {
			t.Errorf("agentFn called with AgentName=%q, want Chris", opts.AgentName)
		}
		return "ok", nil
	}
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	if err := runner(context.Background(), w); err != nil {
		t.Fatalf("runner returned error: %v", err)
	}
	if len(store.runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(store.runs))
	}
	run := store.runs[0]
	if run.Status != WorkflowRunStatusComplete {
		t.Errorf("run.Status = %q, want %q (migrated workflow should run successfully)",
			run.Status, WorkflowRunStatusComplete)
	}
	if len(run.Steps) != 1 {
		t.Fatalf("expected 1 step result, got %d", len(run.Steps))
	}
	if run.Steps[0].Status != "success" {
		t.Errorf("step status = %q, want success", run.Steps[0].Status)
	}
}

// TestMigrateRoutinesToWorkflows_NoRoutineDir verifies the no-op case when
// the routines directory does not exist (already migrated or fresh install).
func TestMigrateRoutinesToWorkflows_NoRoutineDir(t *testing.T) {
	tmpRoot := t.TempDir()
	routineDir := filepath.Join(tmpRoot, "routines") // does not exist
	workflowDir := filepath.Join(tmpRoot, "workflows")

	if err := MigrateRoutinesToWorkflows(routineDir, workflowDir); err != nil {
		t.Fatalf("MigrateRoutinesToWorkflows: %v", err)
	}
}

// TestMigrateRoutinesToWorkflows_PreservesIDAndSlug verifies that an existing
// routine ID and slug are preserved through migration so that any references
// (notification routine_id fields etc.) remain stable.
func TestMigrateRoutinesToWorkflows_PreservesIDAndSlug(t *testing.T) {
	tmpRoot := t.TempDir()
	routineDir := filepath.Join(tmpRoot, "routines")
	workflowDir := filepath.Join(tmpRoot, "workflows")
	if err := os.MkdirAll(routineDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacy := `id: r-stable-1
slug: pr-review
name: PR Review
enabled: false
trigger:
  mode: manual
agent: Chris
prompt: hello
`
	if err := os.WriteFile(filepath.Join(routineDir, "anything.yaml"), []byte(legacy), 0644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	if err := MigrateRoutinesToWorkflows(routineDir, workflowDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	wfs, err := LoadWorkflows(workflowDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(wfs) != 1 {
		t.Fatalf("expected 1 workflow, got %d", len(wfs))
	}
	if wfs[0].ID != "r-stable-1" {
		t.Errorf("ID = %q, want r-stable-1", wfs[0].ID)
	}
	if wfs[0].Slug != "pr-review" {
		t.Errorf("Slug = %q, want pr-review", wfs[0].Slug)
	}
}

// TestRepairLegacyRoutineSteps verifies that workflows already on disk with
// legacy {Routine: slug} steps (from a buggy prior migration) are rewritten
// inline by reading the original routine YAML out of routines.bak.
func TestRepairLegacyRoutineSteps(t *testing.T) {
	tmpRoot := t.TempDir()
	routineBak := filepath.Join(tmpRoot, "routines.bak")
	workflowDir := filepath.Join(tmpRoot, "workflows")
	if err := os.MkdirAll(routineBak, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write the original routine in routines.bak (where the buggy migrator
	// left it after renaming).
	origRoutine := `id: r-orig
slug: pr-review
name: PR Review
agent: Chris
prompt: |
  Review open PRs.
vars:
  TARGET:
    default: main
`
	if err := os.WriteFile(filepath.Join(routineBak, "pr-review.yaml"), []byte(origRoutine), 0644); err != nil {
		t.Fatalf("write routine.bak: %v", err)
	}

	// Write a buggy already-migrated workflow with a {Routine: slug} step.
	buggyWf := `id: r-orig
slug: pr-review
name: PR Review
enabled: true
schedule: ""
steps:
  - routine: pr-review
    position: 0
    on_failure: stop
`
	wfPath := filepath.Join(workflowDir, "r-orig.yaml")
	if err := os.WriteFile(wfPath, []byte(buggyWf), 0644); err != nil {
		t.Fatalf("write buggy wf: %v", err)
	}

	// Run the repair.
	repaired, err := RepairLegacyRoutineSteps(workflowDir, routineBak)
	if err != nil {
		t.Fatalf("RepairLegacyRoutineSteps: %v", err)
	}
	if repaired != 1 {
		t.Errorf("repaired count = %d, want 1", repaired)
	}

	// Reload and check.
	wfs, err := LoadWorkflows(workflowDir)
	if err != nil {
		t.Fatalf("LoadWorkflows: %v", err)
	}
	if len(wfs) != 1 {
		t.Fatalf("expected 1 wf, got %d", len(wfs))
	}
	step := wfs[0].Steps[0]
	if step.Routine != "" {
		t.Errorf("step.Routine = %q, expected to be cleared after repair", step.Routine)
	}
	if step.Agent != "Chris" {
		t.Errorf("step.Agent = %q, want Chris", step.Agent)
	}
	if !strings.Contains(step.Prompt, "Review open PRs") {
		t.Errorf("step.Prompt not populated: %q", step.Prompt)
	}
	if step.Vars["TARGET"] != "main" {
		t.Errorf("step.Vars[TARGET] = %q, want main", step.Vars["TARGET"])
	}
}

// TestRepairLegacyRoutineSteps_NoMatchingRoutine leaves the step untouched
// (and reports it) when the original routine YAML cannot be located.
func TestRepairLegacyRoutineSteps_NoMatchingRoutine(t *testing.T) {
	tmpRoot := t.TempDir()
	routineBak := filepath.Join(tmpRoot, "routines.bak")
	workflowDir := filepath.Join(tmpRoot, "workflows")
	_ = os.MkdirAll(routineBak, 0755)
	_ = os.MkdirAll(workflowDir, 0755)

	buggyWf := `id: r-orphan
slug: orphan
name: Orphan
enabled: false
schedule: ""
steps:
  - routine: missing-slug
    position: 0
    on_failure: stop
`
	if err := os.WriteFile(filepath.Join(workflowDir, "r-orphan.yaml"), []byte(buggyWf), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	repaired, err := RepairLegacyRoutineSteps(workflowDir, routineBak)
	if err != nil {
		t.Fatalf("RepairLegacyRoutineSteps: %v", err)
	}
	if repaired != 0 {
		t.Errorf("repaired = %d, want 0", repaired)
	}
}

// mockRunStore is a tiny test double for WorkflowRunStoreInterface used by
// migration round-trip tests.
type mockRunStore struct {
	runs []*WorkflowRun
}

func (m *mockRunStore) Append(_ string, run *WorkflowRun) error {
	m.runs = append(m.runs, run)
	return nil
}

func (m *mockRunStore) List(_ string, _ int) ([]*WorkflowRun, error) {
	return m.runs, nil
}

func (m *mockRunStore) Get(_, runID string) (*WorkflowRun, error) {
	for _, r := range m.runs {
		if r.ID == runID {
			return r, nil
		}
	}
	return nil, nil
}
