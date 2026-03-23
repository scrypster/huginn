// internal/server/templates.go
package server

import (
	"embed"
	"net/http"

	"gopkg.in/yaml.v3"

	"github.com/scrypster/huginn/internal/scheduler"
)

//go:embed workflows/*.yaml
var embeddedWorkflowTemplates embed.FS

// WorkflowTemplate is a built-in workflow template returned by GET /api/v1/workflows/templates.
type WorkflowTemplate struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Workflow    scheduler.Workflow `json:"workflow"`
}

// builtinWorkflowTemplates returns the embedded workflow templates.
func builtinWorkflowTemplates() []WorkflowTemplate {
	files := []struct {
		slug string
		path string
	}{
		{"daily-standup", "workflows/daily-standup.yaml"},
		{"code-review", "workflows/code-review.yaml"},
		{"weekly-summary", "workflows/weekly-summary.yaml"},
		{"health-check", "workflows/health-check.yaml"},
		{"morning-standup", "workflows/morning-standup.yaml"},
		{"stale-pr-check", "workflows/stale-pr-check.yaml"},
		{"dependency-updates", "workflows/dependency-updates.yaml"},
		{"build-health", "workflows/build-health.yaml"},
		{"branch-cleanup", "workflows/branch-cleanup.yaml"},
	}

	var templates []WorkflowTemplate
	for _, f := range files {
		data, err := embeddedWorkflowTemplates.ReadFile(f.path)
		if err != nil {
			continue
		}
		var w scheduler.Workflow
		if err := yaml.Unmarshal(data, &w); err != nil {
			continue
		}
		templates = append(templates, WorkflowTemplate{
			ID:          f.slug,
			Name:        w.Name,
			Description: w.Description,
			Workflow:    w,
		})
	}
	return templates
}

func (s *Server) handleListWorkflowTemplates(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, builtinWorkflowTemplates())
}
