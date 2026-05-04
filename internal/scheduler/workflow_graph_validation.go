package scheduler

import "fmt"

// ValidateSubWorkflowCycles detects cycles in SubWorkflow cross-references
// across the full workflow registry. A cycle (e.g. A→B→A) would recurse
// indefinitely at runtime, so callers should reject the candidate before
// saving or registering it.
//
// wf is the candidate workflow (new or updated). all is the current registry.
// Dangling sub_workflow references are ignored here (validated elsewhere).
func ValidateSubWorkflowCycles(wf *Workflow, all []*Workflow) error {
	registry := make(map[string]*Workflow, len(all)+1)
	for _, w := range all {
		registry[w.ID] = w
	}
	registry[wf.ID] = wf // candidate overrides stale on-disk version

	const (
		unvisited = 0
		inStack   = 1
		done      = 2
	)
	state := make(map[string]int, len(registry))

	var dfs func(id string) error
	dfs = func(id string) error {
		state[id] = inStack
		w, ok := registry[id]
		if !ok {
			state[id] = done
			return nil // dangling ref: not a cycle edge
		}
		for _, step := range w.Steps {
			ref := step.SubWorkflow
			if ref == "" {
				continue
			}
			if state[ref] == inStack {
				return fmt.Errorf("circular sub_workflow reference: %q → %q creates a cycle", id, ref)
			}
			if state[ref] == unvisited {
				if err := dfs(ref); err != nil {
					return err
				}
			}
		}
		state[id] = done
		return nil
	}

	return dfs(wf.ID)
}
