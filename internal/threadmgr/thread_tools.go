package threadmgr

import "github.com/scrypster/huginn/internal/backend"

// ErrFinish is panicked by ThreadTools.Finish() to break the LLM loop.
type ErrFinish struct {
	Summary FinishSummary
}

// ErrHelp is panicked by ThreadTools.RequestHelp() to break the LLM loop
// and block the thread awaiting human input.
type ErrHelp struct {
	Message string
}

// ThreadTools holds the control-flow tools available inside a thread's LLM loop.
// Not registered in tools.Registry — wired directly into SpawnThread.
type ThreadTools struct{}

// FinishSchema returns the backend.Tool schema for the finish() tool.
func (tt *ThreadTools) FinishSchema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "finish",
			Description: "Call this when you have completed the assigned task. Provide a summary of what was done.",
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"summary"},
				Properties: map[string]backend.ToolProperty{
					"summary": {
						Type:        "string",
						Description: "Human-readable narrative of what was accomplished",
					},
					"files_modified": {
						Type:        "array",
						Description: "List of file paths written or modified",
					},
					"key_decisions": {
						Type:        "array",
						Description: "Important architectural or implementation decisions made",
					},
					"artifacts": {
						Type:        "array",
						Description: "References to notable outputs (binaries, test results, etc.)",
					},
					"status": {
						Type:        "string",
						Description: "One of: completed | blocked | needs_review",
					},
				},
			},
		},
	}
}

// RequestHelpSchema returns the backend.Tool schema for the request_help() tool.
func (tt *ThreadTools) RequestHelpSchema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "request_help",
			Description: "Call this when you are blocked and need human input to continue.",
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"message"},
				Properties: map[string]backend.ToolProperty{
					"message": {
						Type:        "string",
						Description: "The question or blocker description to surface to the user",
					},
				},
			},
		},
	}
}

// Finish panics with *ErrFinish, breaking the LLM loop in SpawnThread.
func (tt *ThreadTools) Finish(args map[string]any) {
	summary, _ := args["summary"].(string)
	status, _ := args["status"].(string)
	if status == "" {
		status = "completed"
	}

	fs := FinishSummary{
		Summary: summary,
		Status:  status,
	}

	if raw, ok := args["files_modified"]; ok {
		if arr, ok := raw.([]any); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					fs.FilesModified = append(fs.FilesModified, s)
				}
			}
		}
	}
	if raw, ok := args["key_decisions"]; ok {
		if arr, ok := raw.([]any); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					fs.KeyDecisions = append(fs.KeyDecisions, s)
				}
			}
		}
	}
	if raw, ok := args["artifacts"]; ok {
		if arr, ok := raw.([]any); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					fs.Artifacts = append(fs.Artifacts, s)
				}
			}
		}
	}

	panic(&ErrFinish{Summary: fs})
}

// RequestHelp panics with *ErrHelp, blocking the thread and surfacing the
// message to the user via the WebSocket broadcast.
func (tt *ThreadTools) RequestHelp(args map[string]any) {
	msg, _ := args["message"].(string)
	panic(&ErrHelp{Message: msg})
}
