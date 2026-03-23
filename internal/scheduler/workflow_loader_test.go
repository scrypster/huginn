package scheduler

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func writeWorkflowYAML(t *testing.T, path string, data map[string]any) {
	t.Helper()
	b, _ := yaml.Marshal(data)
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadWorkflows_Empty(t *testing.T) {
	dir := t.TempDir()
	wfs, err := LoadWorkflows(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(wfs) != 0 {
		t.Errorf("want 0, got %d", len(wfs))
	}
}

func TestLoadWorkflows_MissingDir(t *testing.T) {
	wfs, err := LoadWorkflows("/nonexistent/dir/xyz")
	if err != nil {
		t.Fatal(err)
	}
	if len(wfs) != 0 {
		t.Errorf("want 0, got %d", len(wfs))
	}
}

func TestLoadWorkflows_ParsesSteps(t *testing.T) {
	dir := t.TempDir()
	writeWorkflowYAML(t, filepath.Join(dir, "morning-pipeline.yaml"), map[string]any{
		"id": "wf1", "name": "Morning Pipeline", "enabled": true,
		"schedule": "0 9 * * 1-5",
		"steps": []map[string]any{
			{"routine": "pr-review", "position": 10},
			{"routine": "build-health", "position": 20, "on_failure": "continue"},
		},
	})
	wfs, err := LoadWorkflows(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(wfs) != 1 {
		t.Fatalf("want 1, got %d", len(wfs))
	}
	if wfs[0].Slug != "morning-pipeline" {
		t.Errorf("slug want morning-pipeline, got %q", wfs[0].Slug)
	}
	if len(wfs[0].Steps) != 2 {
		t.Errorf("want 2 steps, got %d", len(wfs[0].Steps))
	}
	if wfs[0].Steps[1].EffectiveOnFailure() != "continue" {
		t.Errorf("step 2 on_failure want continue, got %q", wfs[0].Steps[1].OnFailure)
	}
}

func TestLoadWorkflows_SkipsCorrupt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("{invalid:"), 0644); err != nil {
		t.Fatal(err)
	}
	writeWorkflowYAML(t, filepath.Join(dir, "good.yaml"), map[string]any{
		"id": "wf2", "name": "Good", "enabled": true, "schedule": "0 9 * * *",
	})
	wfs, err := LoadWorkflows(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(wfs) != 1 {
		t.Errorf("want 1 (skipped corrupt), got %d", len(wfs))
	}
}

func TestSaveWorkflow_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	w := &Workflow{ID: "wf3", Name: "Test WF", Enabled: true, Schedule: "0 8 * * *"}
	if err := SaveWorkflow(dir, w); err != nil {
		t.Fatal(err)
	}
	if w.FilePath == "" {
		t.Error("FilePath should be set after save")
	}
	wfs, err := LoadWorkflows(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(wfs) != 1 {
		t.Fatalf("want 1, got %d", len(wfs))
	}
	if wfs[0].ID != "wf3" {
		t.Errorf("ID want wf3, got %q", wfs[0].ID)
	}
}

func TestDeleteWorkflow(t *testing.T) {
	dir := t.TempDir()
	w := &Workflow{ID: "wf4", Name: "To Delete", Enabled: false, Schedule: "0 8 * * *"}
	if err := SaveWorkflow(dir, w); err != nil {
		t.Fatal(err)
	}
	if err := DeleteWorkflow(w); err != nil {
		t.Fatal(err)
	}
	wfs, _ := LoadWorkflows(dir)
	if len(wfs) != 0 {
		t.Errorf("want 0 after delete, got %d", len(wfs))
	}
}
