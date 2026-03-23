// internal/server/handlers_workflows.go
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/notification"
	"github.com/scrypster/huginn/internal/scheduler"
)

// validateWorkflow checks semantic constraints on a Workflow before it is
// persisted. It returns a non-nil error with a human-readable message when
// any constraint is violated.
//
// Validated rules:
//  1. A step's Inputs[].FromStep must reference a step Name that exists in the
//     same workflow.  A dangling reference would cause a silent nil-output at
//     runtime (the referenced step is never found, so the variable is empty).
//  2. Any NotificationDelivery with Type == "space" must have a non-empty
//     SpaceID.  An empty SpaceID reaches the delivery layer and produces a
//     confusing "missing space_id" error at runtime instead of at save time.
//  3. Step field constraints (max_retries, retry_delay, timeout) via step.Validate().
//     All step errors are accumulated and returned together for a single fix cycle.
//  4. If Schedule is non-empty, cron syntax is validated immediately so invalid
//     expressions are rejected before reaching the scheduler.
func validateWorkflow(wf *scheduler.Workflow) error {
	// Build a set of step names for O(1) lookup and detect duplicates.
	stepNames := make(map[string]struct{}, len(wf.Steps))
	for _, step := range wf.Steps {
		if step.Name == "" {
			continue
		}
		if _, exists := stepNames[step.Name]; exists {
			// Duplicate step names make from_step resolution ambiguous: the
			// runner always uses the first completed step with that name, so a
			// later step with the same name is silently ignored. Reject early.
			return fmt.Errorf("duplicate step name %q: step names must be unique within a workflow", step.Name)
		}
		stepNames[step.Name] = struct{}{}
	}

	for i, step := range wf.Steps {
		// Rule 1: dangling from_step references and self-references.
		for j, inp := range step.Inputs {
			if inp.FromStep == "" {
				continue
			}
			// Self-reference: a step cannot read its own output because it has
			// not yet run when its inputs are resolved. Reject clearly.
			if step.Name != "" && inp.FromStep == step.Name {
				return fmt.Errorf(
					"step[%d].inputs[%d].from_step %q references the step itself; a step cannot consume its own output",
					i+1, j+1, inp.FromStep,
				)
			}
			if _, ok := stepNames[inp.FromStep]; !ok {
				return fmt.Errorf(
					"step[%d].inputs[%d].from_step %q does not match any step name in this workflow",
					i+1, j+1, inp.FromStep,
				)
			}
		}

		// Rule 2: space delivery with empty space_id.
		// Rule 3: webhook URLs validated at save time (syntactic check; DNS resolved at delivery).
		if step.Notify != nil {
			for k, d := range step.Notify.DeliverTo {
				if d.Type == "space" && d.SpaceID == "" {
					return fmt.Errorf(
						"step[%d].notify.deliver_to[%d]: type \"space\" requires a non-empty space_id",
						i+1, k+1,
					)
				}
				if d.Type == "webhook" && d.To != "" {
					if err := scheduler.ValidateWebhookURLSyntax(d.To); err != nil {
						return fmt.Errorf("step[%d].notify.deliver_to[%d]: %w", i+1, k+1, err)
					}
				}
			}
		}
	}

	// Rule 4: per-step field validation (max_retries, retry_delay, timeout).
	// Collect ALL step errors so the caller can fix them in one round-trip.
	var stepErrs []string
	for i := range wf.Steps {
		if err := wf.Steps[i].Validate(); err != nil {
			name := wf.Steps[i].Name
			if name == "" {
				name = fmt.Sprintf("step[%d]", i+1)
			}
			stepErrs = append(stepErrs, fmt.Sprintf("step %q: %s", name, err.Error()))
		}
	}
	if len(stepErrs) > 0 {
		return fmt.Errorf("workflow validation failed:\n%s", strings.Join(stepErrs, "\n"))
	}

	// Rule 5: cron schedule syntax validation.
	if wf.Schedule != "" {
		if err := scheduler.ValidateCronSchedule(wf.Schedule); err != nil {
			return fmt.Errorf("invalid schedule: %w", err)
		}
	}

	// Rules 2 and 3 also apply to the workflow-level notification config.
	for k, d := range wf.Notification.DeliverTo {
		if d.Type == "space" && d.SpaceID == "" {
			return fmt.Errorf(
				"notification.deliver_to[%d]: type \"space\" requires a non-empty space_id",
				k,
			)
		}
		if d.Type == "webhook" && d.To != "" {
			if err := scheduler.ValidateWebhookURLSyntax(d.To); err != nil {
				return fmt.Errorf("notification.deliver_to[%d]: %w", k, err)
			}
		}
	}

	// Rule 3: cycle detection using DFS on the from_step dependency graph.
	// Build adjacency: step name → set of steps it depends on (from_step).
	deps := make(map[string][]string, len(wf.Steps))
	for _, step := range wf.Steps {
		deps[step.Name] = nil
		for _, inp := range step.Inputs {
			if inp.FromStep != "" {
				deps[step.Name] = append(deps[step.Name], inp.FromStep)
			}
		}
	}
	// DFS to find back-edges (cycles).
	const (
		unvisited = 0
		inStack   = 1
		done      = 2
	)
	state := make(map[string]int, len(deps))
	var dfs func(name string) error
	dfs = func(name string) error {
		state[name] = inStack
		for _, dep := range deps[name] {
			if state[dep] == inStack {
				return fmt.Errorf("circular dependency detected involving step %q", dep)
			}
			if state[dep] == unvisited {
				if err := dfs(dep); err != nil {
					return err
				}
			}
		}
		state[name] = done
		return nil
	}
	for name := range deps {
		if state[name] == unvisited {
			if err := dfs(name); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateWorkflowAgentsAndConnections checks that every step in the workflow
// references a known agent name and known connection IDs. It is a best-effort
// check: if the agent loader or connection store are unavailable the check is
// skipped (graceful degradation).
//
// Agent name comparison is case-insensitive. Connection ID comparison is exact.
func (s *Server) validateWorkflowAgentsAndConnections(wf *scheduler.Workflow) error {
	// Build known agent set (case-insensitive).
	knownAgents := make(map[string]struct{})
	if s.agentLoader != nil {
		if cfg, err := s.agentLoader(); err == nil && cfg != nil {
			for _, a := range cfg.Agents {
				knownAgents[strings.ToLower(a.Name)] = struct{}{}
			}
		}
	}

	// Build known connection ID set.
	knownConnIDs := make(map[string]struct{})
	if s.connStore != nil {
		if conns, err := s.connStore.List(); err == nil {
			for _, c := range conns {
				knownConnIDs[c.ID] = struct{}{}
			}
		}
	}

	for i, step := range wf.Steps {
		// Check agent name if we have agent data.
		if len(knownAgents) > 0 && step.Agent != "" {
			if _, ok := knownAgents[strings.ToLower(step.Agent)]; !ok {
				return fmt.Errorf("step[%d] references unknown agent %q", i+1, step.Agent)
			}
		}
		// Check connection IDs if we have connection data.
		if len(knownConnIDs) > 0 {
			for alias, connID := range step.Connections {
				_ = alias
				if _, ok := knownConnIDs[connID]; !ok {
					return fmt.Errorf("step[%d] references unknown connection ID %q", i+1, connID)
				}
			}
		}
	}
	return nil
}

func (s *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	dir := filepath.Join(s.huginnDir, "workflows")
	workflows, err := scheduler.LoadWorkflows(dir)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	result := make([]any, len(workflows))
	for i, wf := range workflows {
		result[i] = wf
	}
	jsonOK(w, result)
}

func (s *Server) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	dir := filepath.Join(s.huginnDir, "workflows")
	workflows, err := scheduler.LoadWorkflows(dir)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	for _, wf := range workflows {
		if wf.ID == id {
			jsonOK(w, wf)
			return
		}
	}
	jsonError(w, 404, "workflow not found")
}

func (s *Server) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	var wf scheduler.Workflow
	if err := json.NewDecoder(r.Body).Decode(&wf); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if err := validateWorkflow(&wf); err != nil {
		jsonError(w, 422, "invalid workflow: "+err.Error())
		return
	}
	if err := s.validateWorkflowAgentsAndConnections(&wf); err != nil {
		jsonError(w, 422, "invalid workflow: "+err.Error())
		return
	}
	// Clamp timeout_minutes to the server-enforced safe range [0, 1440].
	wf.TimeoutMinutes = scheduler.ValidateWorkflowTimeout(wf.TimeoutMinutes)
	if wf.ID == "" {
		wf.ID = notification.NewID()
	}
	now := time.Now().UTC()
	wf.CreatedAt = now
	wf.UpdatedAt = now
	dir := filepath.Join(s.huginnDir, "workflows")
	if err := os.MkdirAll(dir, 0755); err != nil {
		jsonError(w, 500, "create workflows dir: "+err.Error())
		return
	}
	if err := scheduler.SaveWorkflow(dir, &wf); err != nil {
		jsonError(w, 500, "save workflow: "+err.Error())
		return
	}
	s.mu.Lock()
	sched := s.sched
	s.mu.Unlock()
	if sched != nil && wf.Enabled {
		if err := sched.RegisterWorkflow(&wf); err != nil {
			// Compensating rollback: remove the file we just wrote to keep
			// disk and scheduler state consistent.
			if wf.FilePath != "" {
				_ = os.Remove(wf.FilePath)
			}
			jsonError(w, 500, "register workflow: "+err.Error())
			return
		}
	}
	jsonOK(w, wf)
}

func (s *Server) handleUpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var updates scheduler.Workflow
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if err := validateWorkflow(&updates); err != nil {
		jsonError(w, 422, "invalid workflow: "+err.Error())
		return
	}
	if err := s.validateWorkflowAgentsAndConnections(&updates); err != nil {
		jsonError(w, 422, "invalid workflow: "+err.Error())
		return
	}
	dir := filepath.Join(s.huginnDir, "workflows")
	workflows, err := scheduler.LoadWorkflows(dir)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	var target *scheduler.Workflow
	for _, wf := range workflows {
		if wf.ID == id {
			target = wf
			break
		}
	}
	if target == nil {
		jsonError(w, 404, "workflow not found")
		return
	}
	// Optimistic concurrency: if the client supplied a non-zero version and it
	// doesn't match the stored version, the payload is stale → 409 Conflict.
	if updates.Version != 0 && updates.Version != target.Version {
		jsonError(w, http.StatusConflict, fmt.Sprintf(
			"workflow version conflict: stored=%d, submitted=%d",
			target.Version, updates.Version,
		))
		return
	}
	updates.ID = id
	updates.FilePath = target.FilePath
	updates.CreatedAt = target.CreatedAt
	updates.UpdatedAt = time.Now().UTC()
	// Carry forward the current version so SaveWorkflow increments from it.
	updates.Version = target.Version
	// Clamp timeout_minutes to the server-enforced safe range [0, 1440].
	updates.TimeoutMinutes = scheduler.ValidateWorkflowTimeout(updates.TimeoutMinutes)
	if err := scheduler.SaveWorkflow(dir, &updates); err != nil {
		jsonError(w, 500, "save workflow: "+err.Error())
		return
	}
	s.mu.Lock()
	sched := s.sched
	s.mu.Unlock()
	if sched != nil {
		sched.RemoveWorkflow(id)
		if updates.Enabled {
			if err := sched.RegisterWorkflow(&updates); err != nil {
				// Scheduler registration failed — attempt compensating rollback to
				// restore the previous workflow on disk and re-register it.
				// TODO: replace with atomic save-then-register (temp file + rename) to
				// eliminate the divergence window entirely.
				rollbackMsg := "register workflow: " + err.Error()
				if rerr := scheduler.SaveWorkflow(dir, target); rerr != nil {
					slog.Error("workflow update: rollback save failed; workflow may be in inconsistent state",
						"id", id, "save_err", rerr, "register_err", err)
					jsonError(w, 500, rollbackMsg+"; rollback also failed — manual scheduler restart may be required")
					return
				}
				if target.Enabled {
					if rerr := sched.RegisterWorkflow(target); rerr != nil {
						slog.Error("workflow update: rollback re-register failed; workflow may be in inconsistent state",
							"id", id, "register_err", rerr)
					}
				}
				jsonError(w, 500, rollbackMsg)
				return
			}
		}
	}
	jsonOK(w, updates)
}

func (s *Server) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	dir := filepath.Join(s.huginnDir, "workflows")
	workflows, err := scheduler.LoadWorkflows(dir)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	for _, wf := range workflows {
		if wf.ID == id {
			if err := scheduler.DeleteWorkflow(wf); err != nil {
				jsonError(w, 500, "delete workflow: "+err.Error())
				return
			}
			s.mu.Lock()
			sched := s.sched
			s.mu.Unlock()
			if sched != nil {
				sched.RemoveWorkflow(id)
			}
			jsonOK(w, map[string]string{"deleted": id})
			return
		}
	}
	jsonError(w, 404, "workflow not found")
}

func (s *Server) handleRunWorkflow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	dir := filepath.Join(s.huginnDir, "workflows")
	workflows, err := scheduler.LoadWorkflows(dir)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	var target *scheduler.Workflow
	for _, wf := range workflows {
		if wf.ID == id {
			target = wf
			break
		}
	}
	if target == nil {
		jsonError(w, 404, "workflow not found")
		return
	}
	s.mu.Lock()
	sched := s.sched
	s.mu.Unlock()
	if sched == nil {
		jsonError(w, 503, "scheduler not configured")
		return
	}
	if err := sched.TriggerWorkflow(r.Context(), target); err != nil {
		if errors.Is(err, scheduler.ErrWorkflowAlreadyRunning) {
			jsonError(w, 409, "workflow is already running")
			return
		}
		if errors.Is(err, scheduler.ErrConcurrencyLimitReached) {
			w.Header().Set("Retry-After", "60")
			jsonError(w, http.StatusServiceUnavailable, "max concurrent workflows capacity reached; retry after 60s")
			return
		}
		jsonError(w, 500, "trigger workflow: "+err.Error())
		return
	}
	jsonOK(w, map[string]string{"status": "triggered", "workflow_id": id})
}

// handleCancelWorkflow cancels a running workflow by interrupting its context.
//
//	POST /api/v1/workflows/{id}/cancel
func (s *Server) handleCancelWorkflow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	sched := s.sched
	s.mu.Unlock()
	if sched == nil {
		jsonError(w, 503, "scheduler not configured")
		return
	}
	if !sched.CancelWorkflow(id) {
		jsonError(w, 404, "workflow is not currently running")
		return
	}
	jsonOK(w, map[string]string{"status": "cancelling", "workflow_id": id})
}

// handleRetryDeliveryFailure re-queues a failed webhook delivery.
// On success, appends a retry marker to the dead-letter log (preserving audit trail).
//
//	POST /api/v1/delivery-failures/retry
func (s *Server) handleRetryDeliveryFailure(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkflowID string `json:"workflow_id"`
		RunID      string `json:"run_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if body.WorkflowID == "" || body.RunID == "" {
		jsonError(w, 400, "workflow_id and run_id are required")
		return
	}

	// Find the matching failure record.
	records, err := scheduler.ReadDeliveryFailures(s.huginnDir, 100)
	if err != nil {
		jsonError(w, 500, "read delivery failures: "+err.Error())
		return
	}
	var target *scheduler.DeliveryFailureRecord
	for i := range records {
		if records[i].WorkflowID == body.WorkflowID && records[i].RunID == body.RunID {
			target = &records[i]
			break
		}
	}
	if target == nil {
		jsonError(w, 404, "delivery failure record not found (may have been retried already)")
		return
	}

	// Re-deliver via a fresh webhook deliverer instance.
	deliverer := scheduler.NewWebhookDeliverer()
	n := &notification.Notification{
		WorkflowID: body.WorkflowID,
		RunID:      body.RunID,
		Summary:    "Manual retry",
	}
	rec := deliverer.Deliver(r.Context(), n, scheduler.NotificationDelivery{Type: "webhook", To: target.URL})
	if rec.Status != "sent" {
		jsonError(w, 502, "retry failed: "+rec.Error)
		return
	}

	// Append retry marker (append-only; preserves audit trail).
	scheduler.MarkDeliveryFailureRetried(s.huginnDir, body.WorkflowID, body.RunID, target.URL)
	jsonOK(w, map[string]string{"status": "retried", "workflow_id": body.WorkflowID, "run_id": body.RunID})
}

// handleListDeliveryFailures returns the last 7 days of webhook delivery
// failures (dead-letter records), limited to 100 entries.
//
//	GET /api/v1/delivery-failures
func (s *Server) handleListDeliveryFailures(w http.ResponseWriter, r *http.Request) {
	records, err := scheduler.ReadDeliveryFailures(s.huginnDir, 100)
	if err != nil {
		jsonError(w, 500, "read delivery failures: "+err.Error())
		return
	}
	jsonOK(w, records)
}

func (s *Server) handleListWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	store := s.workflowRunStore
	s.mu.Unlock()
	if store == nil {
		jsonOK(w, []any{})
		return
	}
	runs, err := store.List(id, 50)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	result := make([]any, len(runs))
	for i, run := range runs {
		result[i] = run
	}
	jsonOK(w, result)
}

func (s *Server) handleGetWorkflowRun(w http.ResponseWriter, r *http.Request) {
	workflowID := r.PathValue("id")
	runID := r.PathValue("run_id")
	s.mu.Lock()
	store := s.workflowRunStore
	s.mu.Unlock()
	if store == nil {
		jsonError(w, 404, "run not found")
		return
	}
	run, err := store.Get(workflowID, runID)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	if run == nil {
		jsonError(w, 404, "run not found")
		return
	}
	jsonOK(w, run)
}
