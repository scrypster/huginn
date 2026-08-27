package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/scheduler"
)

// WriteWorkflowTool drops a workflow YAML or JSON file into
// ~/.huginn/workflows so the WorkflowsWatcher registers it.
// This is the granted path for agents to author pipelines
// (file-drop is the source of truth; no second API).
type WriteWorkflowTool struct {
	HuginnHome string
}

func (t *WriteWorkflowTool) Name() string { return "write_workflow" }

func (t *WriteWorkflowTool) Description() string {
	return "Create or replace a Huginn workflow by writing YAML or JSON into the workflows drop directory (~/.huginn/workflows). The file watcher registers it. Schema: id, name, enabled, schedule (empty = one-shot / Run Now), company_id (optional), steps[{name, agent, prompt, inputs:[{from_step, as}], on_failure, when, sub_workflow, model_override}]. Use {{prev.output}}, {{inputs.alias}}, {{run.scratch.KEY}} in prompts."
}

func (t *WriteWorkflowTool) Permission() PermissionLevel { return PermWrite }

func (t *WriteWorkflowTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "write_workflow",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"filename", "content"},
				Properties: map[string]backend.ToolProperty{
					"filename": {
						Type:        "string",
						Description: "Basename only, ending in .yaml, .yml, or .json (e.g. morning-standup.yaml). Written to ~/.huginn/workflows.",
					},
					"content": {
						Type:        "string",
						Description: "Full workflow document (YAML or JSON) matching the filename extension.",
					},
				},
			},
		},
	}
}

func (t *WriteWorkflowTool) Execute(_ context.Context, args map[string]any) ToolResult {
	filename, _ := args["filename"].(string)
	content, _ := args["content"].(string)
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return ToolResult{IsError: true, Error: "write_workflow: filename is required (e.g. my-pipeline.yaml)"}
	}
	if filepath.IsAbs(filename) || strings.ContainsAny(filename, `/\`) || filename == "." || filename == ".." || !safeWorkflowBasename(filename) {
		return ToolResult{IsError: true, Error: "write_workflow: filename must be a bare name like pipeline.yaml (no path)"}
	}
	if !scheduler.IsWorkflowFilename(filename) {
		return ToolResult{IsError: true, Error: "write_workflow: filename must end in .yaml, .yml, or .json"}
	}
	if int64(len(content)) > scheduler.MaxWorkflowFileBytes {
		return ToolResult{IsError: true, Error: fmt.Sprintf("write_workflow: content exceeds %d byte limit", scheduler.MaxWorkflowFileBytes)}
	}
	if strings.TrimSpace(content) == "" {
		return ToolResult{IsError: true, Error: "write_workflow: content is required"}
	}

	dir := scheduler.WorkflowsDir(t.HuginnHome)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return ToolResult{IsError: true, Error: "write_workflow: mkdir: " + err.Error()}
	}
	dest := filepath.Join(dir, filename)
	if _, err := scheduler.ParseWorkflow([]byte(content), dest); err != nil {
		return ToolResult{IsError: true, Error: "write_workflow: invalid workflow: " + err.Error()}
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return ToolResult{IsError: true, Error: "write_workflow: write: " + err.Error()}
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return ToolResult{IsError: true, Error: "write_workflow: commit: " + err.Error()}
	}
	return ToolResult{
		Output: fmt.Sprintf("wrote workflow %s (%d bytes). Drop dir: %s. Watcher will register it within a few seconds. One-shot: enabled=true and schedule=\"\". Repeating: set a cron schedule. Pass bits between steps with inputs.from_step / {{inputs.alias}} / {{prev.output}}.", filename, len(content), dir),
		Metadata: map[string]any{
			"path": dest,
			"dir":  dir,
		},
	}
}

func safeWorkflowBasename(name string) bool {
	if name == "" || len(name) > 120 {
		return false
	}
	for _, r := range name {
		if r > unicode.MaxASCII {
			return false
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

// RegisterWriteWorkflowTool grants agents the drop-dir writer.
func RegisterWriteWorkflowTool(reg *Registry, huginnHome string) {
	reg.Register(&WriteWorkflowTool{HuginnHome: huginnHome})
}
