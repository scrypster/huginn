// Package scheduler — run analytics helpers (Phase 6).
//
// Replay and fork both need a complete, deterministic copy of the workflow
// that ran, plus the trigger inputs that started it. Run diff needs a stable
// way to align two runs of the same (or similar) workflow step-by-step.
// Everything in this file is pure: no I/O, no goroutines, no globals — so
// the HTTP layer can compose it freely.
package scheduler

import (
	"encoding/json"
	"fmt"
	"strings"
)

// cloneWorkflow returns a deep copy of w via JSON round-trip. Returns nil if
// the input is nil. The Workflow struct is JSON-tagged end-to-end so this is
// the simplest correct deep-copy that also strips runtime-only fields like
// FilePath (yaml:"-" json:"file_path"). On marshal/unmarshal failure we fall
// back to a shallow copy so the runner never panics — the snapshot becomes
// "best-effort" rather than mandatory.
func cloneWorkflow(w *Workflow) *Workflow {
	if w == nil {
		return nil
	}
	b, err := json.Marshal(w)
	if err != nil {
		shallow := *w
		return &shallow
	}
	var out Workflow
	if err := json.Unmarshal(b, &out); err != nil {
		shallow := *w
		return &shallow
	}
	return &out
}

// StepDiff is one row of a structured run diff. Status, Output, Error and
// LatencyMs are reported as left/right pairs so the UI can render a side-by-
// side comparison without re-deriving alignment.
//
// Position pairs by step Position (1-indexed). When a run is shorter than
// the other the missing side is left blank; the OnlyIn field signals which
// side has the row.
type StepDiff struct {
	Position    int    `json:"position"`
	Slug        string `json:"slug"`
	OnlyIn      string `json:"only_in,omitempty"` // "left" | "right" | ""
	StatusLeft  string `json:"status_left,omitempty"`
	StatusRight string `json:"status_right,omitempty"`
	OutputLeft  string `json:"output_left,omitempty"`
	OutputRight string `json:"output_right,omitempty"`
	ErrorLeft   string `json:"error_left,omitempty"`
	ErrorRight  string `json:"error_right,omitempty"`
	LatencyLeft int64  `json:"latency_ms_left,omitempty"`
	LatencyRight int64 `json:"latency_ms_right,omitempty"`
	// Changed is the union: any field differs between left and right.
	// Included so the frontend can highlight rows in O(1).
	Changed bool `json:"changed,omitempty"`
}

// RunDiff is the full diff envelope for two runs of (typically) the same
// workflow. WorkflowChanged signals that the snapshotted workflow definitions
// differ — the user is comparing runs across versions, which is supported
// but flagged so the UI can render an "across versions" badge.
type RunDiff struct {
	LeftRunID        string     `json:"left_run_id"`
	RightRunID       string     `json:"right_run_id"`
	LeftStatus       string     `json:"left_status,omitempty"`
	RightStatus      string     `json:"right_status,omitempty"`
	WorkflowChanged  bool       `json:"workflow_changed,omitempty"`
	StatusChanged    bool       `json:"status_changed,omitempty"`
	Steps            []StepDiff `json:"steps"`
	StepsChangedCount int       `json:"steps_changed_count"`
}

// DiffRuns computes a structured side-by-side diff of two runs. The algorithm
// aligns steps by Position (1-indexed) — the same key the runner uses for
// step ordering. Missing-on-one-side rows are included with OnlyIn set so the
// UI can render them as additions/removals.
//
// Diff is intentionally simple: per-step exact equality on a small set of
// fields. Token-level text diffs live on the frontend.
func DiffRuns(left, right *WorkflowRun) RunDiff {
	out := RunDiff{
		LeftRunID:  safeRunID(left),
		RightRunID: safeRunID(right),
		Steps:      []StepDiff{},
	}
	if left == nil || right == nil {
		return out
	}
	out.LeftStatus = string(left.Status)
	out.RightStatus = string(right.Status)
	out.StatusChanged = left.Status != right.Status

	// WorkflowChanged: compare snapshots when both are present. JSON round-
	// trip stable order isn't guaranteed for maps with non-string keys but
	// Workflow uses only string-keyed maps + slices, so json.Marshal output
	// is stable enough for an equality check.
	if left.WorkflowSnapshot != nil && right.WorkflowSnapshot != nil {
		l, errL := json.Marshal(left.WorkflowSnapshot)
		r, errR := json.Marshal(right.WorkflowSnapshot)
		if errL == nil && errR == nil {
			out.WorkflowChanged = string(l) != string(r)
		}
	}

	leftByPos := map[int]WorkflowStepResult{}
	for _, s := range left.Steps {
		leftByPos[s.Position] = s
	}
	rightByPos := map[int]WorkflowStepResult{}
	for _, s := range right.Steps {
		rightByPos[s.Position] = s
	}
	positions := mergeSortedKeys(leftByPos, rightByPos)
	for _, pos := range positions {
		l, lok := leftByPos[pos]
		r, rok := rightByPos[pos]
		row := StepDiff{Position: pos}
		switch {
		case lok && rok:
			row.Slug = pickFirstNonEmpty(l.Slug, r.Slug)
			row.StatusLeft = l.Status
			row.StatusRight = r.Status
			row.OutputLeft = l.Output
			row.OutputRight = r.Output
			row.ErrorLeft = l.Error
			row.ErrorRight = r.Error
			row.LatencyLeft = l.LatencyMs
			row.LatencyRight = r.LatencyMs
			row.Changed = row.StatusLeft != row.StatusRight ||
				row.OutputLeft != row.OutputRight ||
				row.ErrorLeft != row.ErrorRight ||
				row.LatencyLeft != row.LatencyRight
		case lok:
			row.OnlyIn = "left"
			row.Slug = l.Slug
			row.StatusLeft = l.Status
			row.OutputLeft = l.Output
			row.ErrorLeft = l.Error
			row.LatencyLeft = l.LatencyMs
			row.Changed = true
		case rok:
			row.OnlyIn = "right"
			row.Slug = r.Slug
			row.StatusRight = r.Status
			row.OutputRight = r.Output
			row.ErrorRight = r.Error
			row.LatencyRight = r.LatencyMs
			row.Changed = true
		}
		if row.Changed {
			out.StepsChangedCount++
		}
		out.Steps = append(out.Steps, row)
	}
	return out
}

func safeRunID(r *WorkflowRun) string {
	if r == nil {
		return ""
	}
	return r.ID
}

func pickFirstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// mergeSortedKeys returns the union of integer keys across two maps, sorted
// ascending. It is used by DiffRuns to align steps by Position.
func mergeSortedKeys(a, b map[int]WorkflowStepResult) []int {
	seen := make(map[int]struct{}, len(a)+len(b))
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	out := make([]int, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	// Insertion sort — n is tiny (typical workflow has <20 steps).
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// MergeForkInputs merges base inputs (from the prior run) with overrides
// supplied by the fork caller. Overrides win on collision; nil/empty maps
// are treated as no-op so callers can pass nil safely.
//
// The returned map is always non-nil (possibly empty) — downstream code
// treats nil as "no inputs" and that distinction matters for tests.
func MergeForkInputs(base, override map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(override))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		out[k] = v
	}
	return out
}

// CloneWorkflowOrError is the public entry point for handlers that need a
// deep copy of a workflow snapshot (replay, fork). It surfaces errors
// instead of swallowing them so the HTTP layer can return 500 cleanly.
func CloneWorkflowOrError(w *Workflow) (*Workflow, error) {
	if w == nil {
		return nil, fmt.Errorf("clone workflow: nil input")
	}
	out := cloneWorkflow(w)
	if out == nil {
		return nil, fmt.Errorf("clone workflow: round-trip returned nil")
	}
	return out, nil
}
