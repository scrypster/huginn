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
	"strconv"
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
	// Phase 5: optional `{"inputs": {...}}` body lets manual runs seed the
	// run scratchpad so the first step can reference {{run.scratch.KEY}}
	// without a predecessor step. An empty/missing body is the legacy
	// no-inputs trigger and stays backwards-compatible.
	inputs := readManualRunInputs(r)
	if err := sched.TriggerWorkflowWithInputs(r.Context(), target, inputs); err != nil {
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

// readManualRunInputs decodes the optional `{"inputs": {...}}` body of a
// manual workflow trigger. Missing/empty body returns nil. Body that fails to
// decode also returns nil — manual triggers should never reject on a
// malformed body, since the simplest UI form may submit no body at all.
//
// Inputs are coerced to strings: scalars render verbatim, complex values
// re-marshal to compact JSON so downstream `{{run.scratch.K}}` substitutions
// see something well-formed.
func readManualRunInputs(r *http.Request) map[string]string {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	var body struct {
		Inputs map[string]any `json:"inputs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Inputs) == 0 {
		return nil
	}
	out := make(map[string]string, len(body.Inputs))
	for k, v := range body.Inputs {
		switch val := v.(type) {
		case string:
			out[k] = val
		case nil:
			out[k] = ""
		default:
			b, err := json.Marshal(val)
			if err == nil {
				out[k] = string(b)
			}
		}
	}
	return out
}

// handleTriggerWebhook is a minimal external trigger that lets a third party
// kick a workflow with a JSON payload. The body is dropped into the run
// scratchpad as `{{run.scratch.payload}}` (full JSON) plus per-key entries.
// In production, gate this endpoint behind an auth middleware (the workflow
// id alone is not a secret); the current wiring relies on the existing
// rate-limit + auth middleware applied to /api/v1/* routes.
//
//	POST /api/v1/workflows/{id}/webhook  body: any JSON
func (s *Server) handleTriggerWebhook(w http.ResponseWriter, r *http.Request) {
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

	inputs := map[string]string{}
	if r.Body != nil && r.ContentLength != 0 {
		var raw any
		if err := json.NewDecoder(r.Body).Decode(&raw); err == nil {
			b, _ := json.Marshal(raw)
			inputs["payload"] = string(b)
			// If the payload is a flat object, also expose its top-level keys
			// so simple workflows can reference them as {{run.scratch.foo}}
			// without an unmarshal step.
			if obj, ok := raw.(map[string]any); ok {
				for k, v := range obj {
					switch val := v.(type) {
					case string:
						inputs[k] = val
					case nil:
						inputs[k] = ""
					default:
						vb, err := json.Marshal(val)
						if err == nil {
							inputs[k] = string(vb)
						}
					}
				}
			}
		}
	}

	if err := sched.TriggerWorkflowWithInputs(r.Context(), target, inputs); err != nil {
		if errors.Is(err, scheduler.ErrWorkflowAlreadyRunning) {
			jsonError(w, 409, "workflow is already running")
			return
		}
		if errors.Is(err, scheduler.ErrConcurrencyLimitReached) {
			w.Header().Set("Retry-After", "60")
			jsonError(w, http.StatusServiceUnavailable, "max concurrent workflows capacity reached; retry after 60s")
			return
		}
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, map[string]any{"status": "triggered", "workflow_id": id})
}

// ─────────────────────────────────────────────────────────────────────────────
// Phase 6 — run analytics: replay, fork, diff
// ─────────────────────────────────────────────────────────────────────────────

// handleReplayWorkflowRun re-runs a prior run with HIGH FIDELITY. It loads the
// stored WorkflowSnapshot + TriggerInputs and triggers a fresh run against
// the snapshotted definition (NOT the current YAML). This makes replays
// deterministic across YAML edits — exactly the behaviour incident-response
// debugging expects.
//
//	POST /api/v1/workflows/{id}/runs/{run_id}/replay   (no body)
func (s *Server) handleReplayWorkflowRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	runID := r.PathValue("run_id")

	s.mu.Lock()
	store := s.workflowRunStore
	sched := s.sched
	s.mu.Unlock()
	if store == nil {
		jsonError(w, 503, "run store not configured")
		return
	}
	// Look up the run BEFORE the scheduler check so a missing run id
	// returns 404 rather than masking with a generic 503.
	prior, err := store.Get(id, runID)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	if prior == nil {
		jsonError(w, 404, "run not found")
		return
	}
	if sched == nil {
		jsonError(w, 503, "scheduler not configured")
		return
	}
	if prior.WorkflowSnapshot == nil || prior.WorkflowSnapshot.ID == "" {
		// Older runs (created before Phase 6) don't carry a snapshot. Fall
		// back to the live definition with a header so the UI can warn.
		w.Header().Set("X-Replay-Source", "live-definition")
		dir := filepath.Join(s.huginnDir, "workflows")
		workflows, err := scheduler.LoadWorkflows(dir)
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		var live *scheduler.Workflow
		for _, wf := range workflows {
			if wf.ID == id {
				live = wf
				break
			}
		}
		if live == nil {
			jsonError(w, 404, "workflow not found and no snapshot available")
			return
		}
		if err := triggerWithChainGuard(r, sched, live, prior.TriggerInputs); err != nil {
			handleTriggerError(w, err)
			return
		}
		jsonOK(w, map[string]any{
			"status":            "triggered",
			"replayed_run_id":   runID,
			"used_snapshot":     false,
			"used_input_count":  len(prior.TriggerInputs),
		})
		return
	}
	// Snapshot path — clone so the runner can mutate safely.
	snap, err := scheduler.CloneWorkflowOrError(prior.WorkflowSnapshot)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	w.Header().Set("X-Replay-Source", "snapshot")
	if err := triggerWithChainGuard(r, sched, snap, prior.TriggerInputs); err != nil {
		handleTriggerError(w, err)
		return
	}
	jsonOK(w, map[string]any{
		"status":           "triggered",
		"replayed_run_id":  runID,
		"used_snapshot":    true,
		"used_input_count": len(prior.TriggerInputs),
	})
}

// handleForkWorkflowRun starts a fresh run that uses the prior run's trigger
// inputs as a baseline, with optional per-key overrides supplied in the body
// `{"inputs": {...}, "use_live_definition": false}`. By default forks run
// against the snapshot (deterministic). Setting use_live_definition=true
// runs against the current YAML definition — useful for "iterate then
// re-run from yesterday's inputs" workflows.
//
//	POST /api/v1/workflows/{id}/runs/{run_id}/fork
func (s *Server) handleForkWorkflowRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	runID := r.PathValue("run_id")

	s.mu.Lock()
	store := s.workflowRunStore
	sched := s.sched
	s.mu.Unlock()
	if store == nil {
		jsonError(w, 503, "run store not configured")
		return
	}
	prior, err := store.Get(id, runID)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	if prior == nil {
		jsonError(w, 404, "run not found")
		return
	}
	if sched == nil {
		jsonError(w, 503, "scheduler not configured")
		return
	}

	var body struct {
		Inputs            map[string]any `json:"inputs"`
		UseLiveDefinition bool           `json:"use_live_definition"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	overrides := coerceInputsToStringMap(body.Inputs)
	merged := scheduler.MergeForkInputs(prior.TriggerInputs, overrides)

	// Pick the workflow definition: snapshot (default) or live YAML.
	var target *scheduler.Workflow
	source := "snapshot"
	if body.UseLiveDefinition || prior.WorkflowSnapshot == nil || prior.WorkflowSnapshot.ID == "" {
		dir := filepath.Join(s.huginnDir, "workflows")
		workflows, lErr := scheduler.LoadWorkflows(dir)
		if lErr != nil {
			jsonError(w, 500, lErr.Error())
			return
		}
		for _, wf := range workflows {
			if wf.ID == id {
				target = wf
				break
			}
		}
		if target == nil {
			jsonError(w, 404, "workflow not found and no snapshot available")
			return
		}
		source = "live-definition"
	} else {
		clone, cErr := scheduler.CloneWorkflowOrError(prior.WorkflowSnapshot)
		if cErr != nil {
			jsonError(w, 500, cErr.Error())
			return
		}
		target = clone
	}
	w.Header().Set("X-Fork-Source", source)

	if err := triggerWithChainGuard(r, sched, target, merged); err != nil {
		handleTriggerError(w, err)
		return
	}
	jsonOK(w, map[string]any{
		"status":              "triggered",
		"forked_run_id":       runID,
		"source":              source,
		"used_input_count":    len(merged),
		"override_input_count": len(overrides),
	})
}

// handleDiffWorkflowRuns returns a structured side-by-side diff of two runs.
// Both runs MUST belong to the same workflow ID — cross-workflow diffs are a
// future feature (the WorkflowChanged flag in the diff payload already
// signals "across versions" of the same workflow).
//
//	GET /api/v1/workflows/{id}/runs/{run_id}/diff/{other_run_id}
func (s *Server) handleDiffWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	leftID := r.PathValue("run_id")
	rightID := r.PathValue("other_run_id")

	s.mu.Lock()
	store := s.workflowRunStore
	s.mu.Unlock()
	if store == nil {
		jsonError(w, 503, "run store not configured")
		return
	}
	left, err := store.Get(id, leftID)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	if left == nil {
		jsonError(w, 404, "left run not found")
		return
	}
	right, err := store.Get(id, rightID)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	if right == nil {
		jsonError(w, 404, "right run not found")
		return
	}
	jsonOK(w, scheduler.DiffRuns(left, right))
}

// coerceInputsToStringMap normalises a JSON-decoded inputs object into the
// runner-friendly map[string]string. Nil and empty maps return nil.
// Mirrors the logic in readManualRunInputs but reusable from fork.
func coerceInputsToStringMap(in map[string]any) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		switch val := v.(type) {
		case string:
			out[k] = val
		case nil:
			out[k] = ""
		default:
			b, err := json.Marshal(val)
			if err == nil {
				out[k] = string(b)
			}
		}
	}
	return out
}

// triggerWithChainGuard wraps the scheduler trigger in the same error
// translation we use for /run and /webhook so replay/fork return 409 / 503
// for concurrency rather than a generic 500.
func triggerWithChainGuard(r *http.Request, sched *scheduler.Scheduler, wf *scheduler.Workflow, inputs map[string]string) error {
	return sched.TriggerWorkflowWithInputs(r.Context(), wf, inputs)
}

// handleTriggerError translates scheduler trigger errors into the same HTTP
// codes used by /run and /webhook so replay/fork callers see consistent
// responses.
func handleTriggerError(w http.ResponseWriter, err error) {
	if errors.Is(err, scheduler.ErrWorkflowAlreadyRunning) {
		jsonError(w, 409, "workflow is already running")
		return
	}
	if errors.Is(err, scheduler.ErrConcurrencyLimitReached) {
		w.Header().Set("Retry-After", "60")
		jsonError(w, http.StatusServiceUnavailable, "max concurrent workflows capacity reached; retry after 60s")
		return
	}
	jsonError(w, 500, err.Error())
}

// handleCronPreview returns the next-N upcoming run times for a cron
// expression so the workflow editor UI can render a "next runs" preview while
// the user types. The endpoint is intentionally side-effect-free and
// idempotent; no scheduling state is mutated.
//
//	GET /api/v1/workflows/cron-preview?expr=...&count=5
//
// Response: {"expr": "...", "next_runs": ["2026-04-26T18:30:00Z", ...]}
// Errors:
//   - 400 when expr is missing or syntactically invalid (parser surfaces the
//     details; the body wraps them in {"error": "..."}).
func (s *Server) handleCronPreview(w http.ResponseWriter, r *http.Request) {
	expr := strings.TrimSpace(r.URL.Query().Get("expr"))
	if expr == "" {
		jsonError(w, 400, "expr query parameter is required")
		return
	}
	count := 5
	if c := r.URL.Query().Get("count"); c != "" {
		if parsed, err := strconv.Atoi(c); err == nil {
			count = parsed
		}
	}
	runs, err := scheduler.CronPreview(expr, count, time.Now().UTC())
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	out := struct {
		Expr     string      `json:"expr"`
		NextRuns []time.Time `json:"next_runs"`
	}{Expr: expr, NextRuns: runs}
	jsonOK(w, out)
}

// handleListDeliveryQueue returns actionable (failed) delivery queue entries.
//
//	GET /api/v1/delivery-queue
func (s *Server) handleListDeliveryQueue(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	q := s.deliveryQueue
	s.mu.Unlock()
	if q == nil {
		jsonOK(w, []any{})
		return
	}
	entries, err := q.ListActionable(100)
	if err != nil {
		jsonError(w, 500, "list delivery queue: "+err.Error())
		return
	}
	jsonOK(w, entries)
}

// handleDeliveryQueueBadge returns the badge count for the nav indicator.
//
//	GET /api/v1/delivery-queue/badge
func (s *Server) handleDeliveryQueueBadge(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	q := s.deliveryQueue
	s.mu.Unlock()
	if q == nil {
		jsonOK(w, map[string]int{"count": 0})
		return
	}
	count, err := q.BadgeCount()
	if err != nil {
		jsonError(w, 500, "badge count: "+err.Error())
		return
	}
	jsonOK(w, map[string]int{"count": count})
}

// handleRetryDeliveryQueueEntry forces immediate retry of a queue entry.
//
//	POST /api/v1/delivery-queue/{id}/retry
func (s *Server) handleRetryDeliveryQueueEntry(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	q := s.deliveryQueue
	s.mu.Unlock()
	if q == nil {
		jsonError(w, 503, "delivery queue not configured")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, 400, "missing id")
		return
	}
	if err := q.ForceRetry(r.Context(), id); err != nil {
		jsonError(w, 404, "retry failed: "+err.Error())
		return
	}
	jsonOK(w, map[string]string{"status": "retrying", "id": id})
}

// handleDismissDeliveryQueueEntry removes a failed entry from the queue.
//
//	DELETE /api/v1/delivery-queue/{id}
func (s *Server) handleDismissDeliveryQueueEntry(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	q := s.deliveryQueue
	s.mu.Unlock()
	if q == nil {
		jsonError(w, 503, "delivery queue not configured")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, 400, "missing id")
		return
	}
	if err := q.Dismiss(id); err != nil {
		jsonError(w, 500, "dismiss: "+err.Error())
		return
	}
	jsonOK(w, map[string]string{"status": "dismissed", "id": id})
}

// handleValidateWorkflow is a dry-run validation endpoint.
// It decodes the request body as a Workflow, runs all structural and
// cross-reference validation, and returns {"valid": true} on success.
// It does NOT persist the workflow or register any cron entry.
//
// POST /api/v1/workflows/validate
// Response 200: {"valid": true}
// Response 400: {"error": "invalid JSON: ..."}
// Response 422: {"error": "invalid workflow: ..."}
func (s *Server) handleValidateWorkflow(w http.ResponseWriter, r *http.Request) {
	var wf scheduler.Workflow
	if err := json.NewDecoder(r.Body).Decode(&wf); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := validateWorkflow(&wf); err != nil {
		jsonError(w, http.StatusUnprocessableEntity, "invalid workflow: "+err.Error())
		return
	}
	if err := s.validateWorkflowAgentsAndConnections(&wf); err != nil {
		jsonError(w, http.StatusUnprocessableEntity, "invalid workflow: "+err.Error())
		return
	}
	jsonOK(w, map[string]bool{"valid": true})
}
