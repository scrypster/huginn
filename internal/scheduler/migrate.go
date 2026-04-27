// internal/scheduler/migrate.go
package scheduler

import (
	"fmt"
	"log/slog"
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
	ID           string `yaml:"id"`
	Slug         string `yaml:"slug,omitempty"`
	Name         string `yaml:"name"`
	Description  string `yaml:"description,omitempty"`
	Enabled      bool   `yaml:"enabled"`
	Trigger      struct {
		Mode string `yaml:"mode"`
		Cron string `yaml:"cron,omitempty"`
	} `yaml:"trigger"`
	Agent        string `yaml:"agent"`
	Prompt       string `yaml:"prompt"`
	Workspace    string `yaml:"workspace,omitempty"`
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
//
// The produced step is INLINE (Agent + Prompt + Vars + Connections) rather
// than a {Routine: slug} reference, because workflow_runner.go rejects legacy
// slug references as a hard error. See TestMigrateRoutinesToWorkflows_ProducesInlineSteps.
//
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
		now := time.Now().UTC()
		slug := r.Slug
		if slug == "" && r.Name != "" {
			slug = strings.ToLower(strings.ReplaceAll(r.Name, " ", "-"))
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
			Steps:     []WorkflowStep{routineToInlineStep(&r)},
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

// routineToInlineStep produces a self-contained WorkflowStep from a legacy
// routine. The Routine field is intentionally left empty so the runner does
// not reject it as a "legacy routine slug reference".
func routineToInlineStep(r *legacyRoutine) WorkflowStep {
	stepVars := make(map[string]string, len(r.Vars))
	for k, v := range r.Vars {
		if v.Default != "" {
			stepVars[k] = v.Default
		}
	}
	connections := make(map[string]string, len(r.Connections))
	for k, v := range r.Connections {
		connections[k] = v
	}
	return WorkflowStep{
		Agent:       r.Agent,
		Prompt:      r.Prompt,
		Vars:        stepVars,
		Connections: connections,
		Position:    0,
		OnFailure:   "stop",
	}
}

// RepairLegacyRoutineSteps walks workflowDir and rewrites any workflow whose
// step still has a non-empty Routine field (a leftover of a buggy prior
// migration) into an inline step by reading the original routine YAML out of
// routineBakDir.
//
// Returns the number of workflows that were repaired. Workflows whose
// referenced routine cannot be found in routineBakDir are left untouched
// (and a warning is logged); they will continue to fail at runtime, but at
// least the failure mode is unchanged.
//
// This is idempotent: workflows with no Routine field set on any step are
// not rewritten.
func RepairLegacyRoutineSteps(workflowDir, routineBakDir string) (int, error) {
	wfs, err := LoadWorkflows(workflowDir)
	if err != nil {
		return 0, err
	}
	repaired := 0
	for _, w := range wfs {
		needsRepair := false
		for _, s := range w.Steps {
			if s.Routine != "" && (s.Agent == "" || s.Prompt == "") {
				needsRepair = true
				break
			}
		}
		if !needsRepair {
			continue
		}
		newSteps := make([]WorkflowStep, 0, len(w.Steps))
		anyRepaired := false
		for _, s := range w.Steps {
			if s.Routine == "" || (s.Agent != "" && s.Prompt != "") {
				newSteps = append(newSteps, s)
				continue
			}
			r, err := findLegacyRoutineBySlug(routineBakDir, s.Routine)
			if err != nil || r == nil {
				slog.Warn("scheduler.migrate: cannot repair legacy routine step",
					"workflow", w.ID, "routine", s.Routine, "error", err)
				newSteps = append(newSteps, s)
				continue
			}
			inline := routineToInlineStep(r)
			inline.Position = s.Position
			if s.OnFailure != "" {
				inline.OnFailure = s.OnFailure
			}
			if s.MaxRetries != 0 {
				inline.MaxRetries = s.MaxRetries
			}
			if s.RetryDelay != "" {
				inline.RetryDelay = s.RetryDelay
			}
			if s.Timeout != "" {
				inline.Timeout = s.Timeout
			}
			if len(s.Vars) > 0 {
				if inline.Vars == nil {
					inline.Vars = map[string]string{}
				}
				for k, v := range s.Vars {
					inline.Vars[k] = v // step-level overrides win
				}
			}
			newSteps = append(newSteps, inline)
			anyRepaired = true
		}
		if !anyRepaired {
			continue
		}
		w.Steps = newSteps
		w.UpdatedAt = time.Now().UTC()
		if err := SaveWorkflow(workflowDir, w); err != nil {
			return repaired, fmt.Errorf("scheduler.migrate: save repaired workflow %s: %w", w.ID, err)
		}
		slog.Info("scheduler.migrate: repaired legacy routine step",
			"workflow_id", w.ID, "workflow_name", w.Name)
		repaired++
	}
	return repaired, nil
}

// findLegacyRoutineBySlug scans routineBakDir for a YAML file whose Slug or
// derived slug matches the given slug.
func findLegacyRoutineBySlug(routineBakDir, slug string) (*legacyRoutine, error) {
	if routineBakDir == "" {
		return nil, nil
	}
	if _, err := os.Stat(routineBakDir); err != nil {
		return nil, nil
	}
	entries, err := os.ReadDir(routineBakDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || (!strings.HasSuffix(e.Name(), ".yaml") && !strings.HasSuffix(e.Name(), ".yml")) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(routineBakDir, e.Name()))
		if err != nil {
			continue
		}
		var r legacyRoutine
		if err := yaml.Unmarshal(data, &r); err != nil {
			continue
		}
		candidate := r.Slug
		if candidate == "" && r.Name != "" {
			candidate = strings.ToLower(strings.ReplaceAll(r.Name, " ", "-"))
		}
		if candidate == slug {
			return &r, nil
		}
		// Also match on filename (without extension), since the bug used the
		// filename-derived slug.
		base := strings.TrimSuffix(strings.TrimSuffix(e.Name(), ".yaml"), ".yml")
		if base == slug {
			return &r, nil
		}
	}
	return nil, nil
}
