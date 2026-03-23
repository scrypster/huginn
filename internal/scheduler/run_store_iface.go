package scheduler

// WorkflowRunStoreInterface is the read/write contract for a workflow run store.
type WorkflowRunStoreInterface interface {
	Append(workflowID string, run *WorkflowRun) error
	List(workflowID string, n int) ([]*WorkflowRun, error)
	// Get returns a single WorkflowRun by workflow ID and run ID.
	// Returns (nil, nil) when the run does not exist.
	Get(workflowID, runID string) (*WorkflowRun, error)
}
