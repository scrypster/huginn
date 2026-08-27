package scheduler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// MaxWorkflowFileBytes is the largest workflow drop file we will parse.
// Bigger files are skipped (file-drop) or rejected (HTTP) so a huge dump
// cannot take down list/load.
const MaxWorkflowFileBytes int64 = 1 << 20 // 1 MiB

// IsWorkflowFilename reports whether name is a workflow drop file
// (*.yaml, *.yml, or *.json). MJ asked for JSON or YAML.
func IsWorkflowFilename(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".yaml" || ext == ".yml" || ext == ".json"
}

func slugFromFilename(path string) string {
	base := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(base))
	return strings.TrimSuffix(base, ext)
}

// LoadWorkflows reads all *.yaml / *.yml / *.json files in dir and parses
// them as Workflows. Returns empty slice (not nil) if dir is missing or
// empty. Skips corrupt, oversized, or unreadable files (never 500s the dir).
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
		if e.Type()&os.ModeSymlink != 0 {
			continue // never follow a drop-dir symlink
		}
		name := e.Name()
		if !IsWorkflowFilename(name) {
			continue
		}
		if !ValidWorkflowID(slugFromFilename(name)) {
			continue
		}
		path := filepath.Join(dir, name)
		w, err := loadWorkflowFile(path)
		if err != nil {
			continue // skip corrupt / huge
		}
		workflows = append(workflows, w)
	}
	if workflows == nil {
		return []*Workflow{}, nil
	}
	return workflows, nil
}

func loadWorkflowFile(path string) (*Workflow, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("scheduler: stat workflow %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("scheduler: skip non-regular workflow %s", path)
	}
	if info.Size() > MaxWorkflowFileBytes {
		return nil, fmt.Errorf("scheduler: workflow %s exceeds %d byte limit", path, MaxWorkflowFileBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("scheduler: read workflow %s: %w", path, err)
	}
	return ParseWorkflow(data, path)
}

// ParseWorkflow unmarshals YAML or JSON (chosen by path extension) into a
// Workflow and runs per-step Validate. Agents and HTTP handlers use this
// so a bad drop file fails closed with a clear error instead of a 500.
func ParseWorkflow(data []byte, path string) (*Workflow, error) {
	if int64(len(data)) > MaxWorkflowFileBytes {
		return nil, fmt.Errorf("scheduler: workflow exceeds %d byte limit", MaxWorkflowFileBytes)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("scheduler: empty workflow file %s", path)
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return nil, fmt.Errorf("scheduler: binary workflow file %s", path)
	}
	var w Workflow
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".json" {
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, fmt.Errorf("scheduler: parse workflow json %s: %w", path, err)
		}
	} else {
		if err := yaml.Unmarshal(data, &w); err != nil {
			return nil, fmt.Errorf("scheduler: parse workflow %s: %w", path, err)
		}
	}
	w.FilePath = path
	if w.Slug == "" {
		w.Slug = slugFromFilename(path)
	}
	if strings.TrimSpace(w.ID) != "" && !ValidWorkflowID(w.ID) {
		return nil, fmt.Errorf("scheduler: invalid workflow id %q", w.ID)
	}
	if strings.TrimSpace(w.ID) == "" && strings.TrimSpace(w.Name) == "" && len(w.Steps) == 0 {
		return nil, fmt.Errorf("scheduler: workflow %s has no id, name, or steps", path)
	}
	for i := range w.Steps {
		if err := w.Steps[i].Validate(); err != nil {
			return nil, fmt.Errorf("scheduler: workflow %s step %d: %w", path, i, err)
		}
	}
	return &w, nil
}

// ValidWorkflowID is a drop-dir basename (no path, no "..").
// API create and SaveWorkflow reject anything else so an id of
// "../escape" cannot write outside ~/.huginn/workflows.
func ValidWorkflowID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || id == "." || id == ".." || len(id) > 120 {
		return false
	}
	if strings.ContainsAny(id, `/\`) {
		return false
	}
	for _, r := range id {
		if r > 127 {
			return false
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func workflowDest(dir, id string) (string, error) {
	if !ValidWorkflowID(id) {
		return "", fmt.Errorf("scheduler: invalid workflow id %q", id)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(absDir, id+".yaml")
	rel, err := filepath.Rel(absDir, dest)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("scheduler: workflow path escapes drop dir")
	}
	return dest, nil
}

// SaveWorkflow writes a Workflow to its FilePath or creates {id}.yaml in dir.
// It increments w.Version before writing so callers always receive the new version.
func SaveWorkflow(dir string, w *Workflow) error {
	if w.FilePath == "" {
		dest, err := workflowDest(dir, w.ID)
		if err != nil {
			return err
		}
		w.FilePath = dest
	} else {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		absFile, err := filepath.Abs(w.FilePath)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(absDir, absFile)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return fmt.Errorf("scheduler: workflow path escapes drop dir")
		}
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

// WriteWorkflowTemp marshals w to a ".tmp" file alongside its FilePath,
// incrementing w.Version. Returns the temp path. The caller must either
// os.Rename(tmpPath, w.FilePath) to commit or os.Remove(tmpPath) to abort.
// On any error, w.Version is rolled back.
func WriteWorkflowTemp(dir string, w *Workflow) (string, error) {
	if w.FilePath == "" {
		dest, err := workflowDest(dir, w.ID)
		if err != nil {
			return "", err
		}
		w.FilePath = dest
	}
	w.Version++
	data, err := yaml.Marshal(w)
	if err != nil {
		w.Version--
		return "", fmt.Errorf("scheduler: marshal workflow: %w", err)
	}
	tmp := w.FilePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		w.Version--
		return "", fmt.Errorf("scheduler: write workflow: %w", err)
	}
	return tmp, nil
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

// WorkflowsDir returns ~/.huginn/workflows (or huginnHome/workflows).
// Agents write JSON or YAML here; the watcher registers them.
func WorkflowsDir(huginnHome string) string {
	return filepath.Join(huginnHome, "workflows")
}
