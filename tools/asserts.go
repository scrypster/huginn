//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RunResult mirrors internal/headless.RunResult for JSON parsing.
type RunResult struct {
	Mode            string           `json:"mode"`
	Root            string           `json:"root"`
	ReposFound      []string         `json:"reposFound"`
	IndexDuration   string           `json:"indexDuration"`
	FilesScanned    int              `json:"filesScanned"`
	FilesSkipped    int              `json:"filesSkipped"`
	RadarDuration   string           `json:"radarDuration"`
	BFSNodesVisited int              `json:"bfsNodesVisited"`
	TopFindings     []FindingSummary `json:"topFindings"`
	BannersEmitted  int              `json:"bannersEmitted"`
	Errors          []string         `json:"errors,omitempty"`
}

// FindingSummary mirrors internal/headless.FindingSummary.
type FindingSummary struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Title    string   `json:"title"`
	Severity string   `json:"severity"`
	Score    float64  `json:"score"`
	Files    []string `json:"files"`
}

// AssertResult tracks pass/fail counts for a scenario.
type AssertResult struct {
	ScenarioName string
	Passed       int
	Failed       int
	Failures     []string
}

func (r *AssertResult) assert(name string, cond bool, format string, args ...interface{}) {
	if cond {
		r.Passed++
		fmt.Printf("  [PASS] %s\n", name)
	} else {
		r.Failed++
		msg := fmt.Sprintf(format, args...)
		r.Failures = append(r.Failures, fmt.Sprintf("%s: %s", name, msg))
		fmt.Printf("  [FAIL] %s: %s\n", name, msg)
	}
}

func (r *AssertResult) summary() {
	total := r.Passed + r.Failed
	fmt.Printf("\nScenario %s: %d/%d passed", r.ScenarioName, r.Passed, total)
	if r.Failed == 0 {
		fmt.Println(" ✓")
	} else {
		fmt.Printf(" — %d FAILURES\n", r.Failed)
		for _, f := range r.Failures {
			fmt.Printf("    - %s\n", f)
		}
	}
}

func parseRunResult(jsonOutput string) (*RunResult, error) {
	// Find the JSON object: look for `{"` to skip Pebble WAL log lines
	// that contain bare `{(...)` entries.
	start := strings.Index(jsonOutput, `{"`)
	if start < 0 {
		return nil, fmt.Errorf("no JSON found in output (len=%d)", len(jsonOutput))
	}
	var result RunResult
	if err := json.Unmarshal([]byte(jsonOutput[start:]), &result); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	return &result, nil
}

func hasFindingWithSeverity(result *RunResult, severity string) bool {
	for _, f := range result.TopFindings {
		if strings.EqualFold(f.Severity, severity) {
			return true
		}
	}
	return false
}

func hasFindingType(result *RunResult, findingType string) bool {
	for _, f := range result.TopFindings {
		if f.Type == findingType {
			return true
		}
	}
	return false
}

func hasFindingWithFile(result *RunResult, fileSubstr string) bool {
	for _, f := range result.TopFindings {
		for _, file := range f.Files {
			if strings.Contains(file, fileSubstr) {
				return true
			}
		}
	}
	return false
}

func maxSeverityScore(result *RunResult) float64 {
	var max float64
	for _, f := range result.TopFindings {
		if f.Score > max {
			max = f.Score
		}
	}
	return max
}
