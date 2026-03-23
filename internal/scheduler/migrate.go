// internal/scheduler/migrate.go
package scheduler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/notification"
	"gopkg.in/yaml.v3"
)

// legacyRoutine is a minimal struct used only during one-time migration of old routine YAML files.
// After migration, routineDir is renamed to routineDir+".bak" and this type is never used again.
type legacyRoutine struct {
	ID          string `yaml:"id"`
	Slug        string `yaml:"slug,omitempty"`
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Enabled     bool   `yaml:"enabled"`
	Trigger     struct {
		Mode string `yaml:"mode"`
		Cron string `yaml:"cron,omitempty"`
	} `yaml:"trigger"`
	Agent     string `yaml:"agent"`
	Prompt    string `yaml:"prompt"`
	Workspace string `yaml:"workspace,omitempty"`
	Notification struct {
		Severity string `yaml:"severity,omitempty"`
	} `yaml:"notification,omitempty"`
	Vars map[string]struct {
		Default string `yaml:"default,omitempty"`
	} `yaml:"vars,omitempty"`
	Connections map[string]string `yaml:"connections,omitempty"`
}

// MigrateRoutinesToWorkflows converts legacy routine YAMLs in routineDir into
// self-contained single-step workflow YAMLs in workflowDir.
// When done, routineDir is renamed to routineDir+".bak" (one-time migration).
// Returns nil if routineDir does not exist (already migrated or never had routines).
func MigrateRoutinesToWorkflows(routineDir, workflowDir string) error {
	if _, err := os.Stat(routineDir); os.IsNotExist(err) {
		return nil // already migrated or never had routines
	}
	entries, err := os.ReadDir(routineDir)
	if err != nil {
		return fmt.Errorf("migrate: read dir: %w", err)
	}
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		return fmt.Errorf("migrate: create workflows dir: %w", err)
	}
	migrated := 0
	for _, e := range entries {
		if e.IsDir() || (!strings.HasSuffix(e.Name(), ".yaml") && !strings.HasSuffix(e.Name(), ".yml")) {
			continue
		}
		path := filepath.Join(routineDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var r legacyRoutine
		if err := yaml.Unmarshal(data, &r); err != nil {
			continue
		}
		if r.ID == "" {
			r.ID = notification.NewID()
		}
		// Flatten var defaults for prompt substitution hints
		stepVars := make(map[string]string)
		for k, v := range r.Vars {
			if v.Default != "" {
				stepVars[k] = v.Default
			}
		}
		now := time.Now().UTC()
		slug := r.Slug
		if slug == "" && r.Name != "" {
			slug = strings.ToLower(strings.ReplaceAll(r.Name, " ", "-"))
		}
		step := WorkflowStep{
			Routine:   slug,
			Vars:      stepVars,
			Position:  0,
			OnFailure: "stop",
		}
		w := &Workflow{
			ID:          r.ID,
			Slug:        slug,
			Name:        r.Name,
			Description: r.Description,
			Enabled:     r.Enabled,
			Schedule:    r.Trigger.Cron,
			Notification: WorkflowNotificationConfig{
				OnFailure: true,
				Severity:  r.Notification.Severity,
			},
			Steps:     []WorkflowStep{step},
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := SaveWorkflow(workflowDir, w); err != nil {
			continue
		}
		migrated++
	}
	_ = migrated // suppress unused warning
	// Rename routineDir → routineDir.bak
	return os.Rename(routineDir, routineDir+".bak")
}
