package scheduler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WorkflowRunStore persists WorkflowRun records as append-only JSONL files.
// Each workflow gets its own file: {baseDir}/{workflowID}.jsonl.
type WorkflowRunStore struct {
	baseDir string
}

// NewWorkflowRunStore creates a store backed by baseDir.
func NewWorkflowRunStore(baseDir string) *WorkflowRunStore {
	return &WorkflowRunStore{baseDir: baseDir}
}

func (s *WorkflowRunStore) runPath(workflowID string) string {
	return filepath.Join(s.baseDir, workflowID+".jsonl")
}

// Append writes a single WorkflowRun as a JSON line.
func (s *WorkflowRunStore) Append(workflowID string, run *WorkflowRun) error {
	if err := os.MkdirAll(s.baseDir, 0755); err != nil {
		return fmt.Errorf("workflow run store: mkdir: %w", err)
	}
	line, err := json.Marshal(run)
	if err != nil {
		return fmt.Errorf("workflow run store: marshal: %w", err)
	}
	line = append(line, '\n')
	f, err := os.OpenFile(s.runPath(workflowID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("workflow run store: open: %w", err)
	}
	defer f.Close()
	_, err = f.Write(line)
	return err
}

// List returns the last n runs for workflowID, newest first.
// Returns nil, nil if no runs exist.
func (s *WorkflowRunStore) List(workflowID string, n int) ([]*WorkflowRun, error) {
	f, err := os.Open(s.runPath(workflowID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var all []*WorkflowRun
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var run WorkflowRun
		if json.Unmarshal([]byte(line), &run) == nil {
			all = append(all, &run)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("workflow run store: scan %s: %w", s.runPath(workflowID), err)
	}
	if n <= 0 || len(all) == 0 {
		return nil, nil
	}
	if len(all) <= n {
		reverseRuns(all)
		return all, nil
	}
	tail := all[len(all)-n:]
	reverseRuns(tail)
	return tail, nil
}

// Get returns a single WorkflowRun by workflow ID and run ID.
// Returns (nil, nil) when no matching run is found.
func (s *WorkflowRunStore) Get(workflowID, runID string) (*WorkflowRun, error) {
	f, err := os.Open(s.runPath(workflowID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var run WorkflowRun
		if json.Unmarshal([]byte(line), &run) == nil && run.ID == runID {
			return &run, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("workflow run store: scan %s: %w", s.runPath(workflowID), err)
	}
	return nil, nil
}

func reverseRuns(runs []*WorkflowRun) {
	for i, j := 0, len(runs)-1; i < j; i, j = i+1, j-1 {
		runs[i], runs[j] = runs[j], runs[i]
	}
}

var _ WorkflowRunStoreInterface = (*WorkflowRunStore)(nil)
