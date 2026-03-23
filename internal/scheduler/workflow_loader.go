package scheduler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadWorkflows reads all *.yaml files in dir and parses them as Workflows.
// Returns empty slice (not nil) if dir is missing or empty. Skips corrupt files.
func LoadWorkflows(dir string) ([]*Workflow, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*Workflow{}, nil
		}
		return nil, fmt.Errorf("scheduler: read workflows dir %s: %w", dir, err)
	}
	var workflows []*Workflow
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		path := filepath.Join(dir, name)
		w, err := loadWorkflowFile(path)
		if err != nil {
			continue // skip corrupt
		}
		workflows = append(workflows, w)
	}
	if workflows == nil {
		return []*Workflow{}, nil
	}
	return workflows, nil
}

func loadWorkflowFile(path string) (*Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("scheduler: read workflow %s: %w", path, err)
	}
	var w Workflow
	if err := yaml.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("scheduler: parse workflow %s: %w", path, err)
	}
	w.FilePath = path
	if w.Slug == "" {
		base := filepath.Base(path)
		w.Slug = strings.TrimSuffix(strings.TrimSuffix(base, ".yaml"), ".yml")
	}
	// Pre-parse per-step fields (e.g. retry_delay) at load time so the runner
	// never has to parse them at execution time.
	for i := range w.Steps {
		if err := w.Steps[i].Validate(); err != nil {
			return nil, fmt.Errorf("scheduler: workflow %s step %d: %w", path, i, err)
		}
	}
	return &w, nil
}

// SaveWorkflow writes a Workflow to its FilePath or creates {id}.yaml in dir.
// It increments w.Version before writing so callers always receive the new version.
func SaveWorkflow(dir string, w *Workflow) error {
	if w.FilePath == "" {
		w.FilePath = filepath.Join(dir, w.ID+".yaml")
	}
	w.Version++
	data, err := yaml.Marshal(w)
	if err != nil {
		w.Version-- // roll back on marshal failure
		return fmt.Errorf("scheduler: marshal workflow: %w", err)
	}
	tmp := w.FilePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		w.Version-- // roll back on write failure
		return fmt.Errorf("scheduler: write workflow: %w", err)
	}
	if err := os.Rename(tmp, w.FilePath); err != nil {
		w.Version-- // roll back on rename failure
		return err
	}
	return nil
}

// DeleteWorkflow removes the workflow's YAML file from disk.
func DeleteWorkflow(w *Workflow) error {
	if w.FilePath == "" {
		return nil
	}
	err := os.Remove(w.FilePath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
