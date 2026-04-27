// internal/scheduler/workflow_runner.go
package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/scrypster/huginn/internal/notification"
)


// errUnresolvedPlaceholder is the prefix for step errors caused by template
// placeholders that remain after variable resolution.
const errUnresolvedPlaceholder = "step failed: unresolved template placeholders"

// unresolvedPlaceholderRe matches any {{...}} token that was not substituted.
var unresolvedPlaceholderRe = regexp.MustCompile(`\{\{[^}]+\}\}`)

const maxStepOutputBytes = 64 * 1024 // 64 KB

// RunOptions carries the parameters for a single agent run.
type RunOptions struct {
	RoutineID   string
	RunID       string
	AgentName   string
	Prompt      string
	Workspace   string
	MaxTokens   int
	// Connections maps provider name → connection account label for
	// pre-authorised credentials. Injected into the agent's context so the
	// agent picks the right account when calling integration tools.
	Connections map[string]string
	// WorkflowID + StepName + StepPosition let an AgentFunc emit live
	// streaming events (e.g. token deltas) tagged with the step they belong to.
	// Optional — populated by the runner; safe to leave empty in tests.
	WorkflowID   string
	StepName     string
	StepPosition int
	// OnToken, when non-nil, is invoked for every model-emitted token chunk
	// during step execution. AgentFuncs that support streaming SHOULD call it
	// so the live UI panel can show progressive output. Nil = silent (legacy
	// behaviour). The callback MUST be cheap and non-blocking — the runner
	// sends WS events from inside it.
	OnToken func(token string)

	// ModelOverride (Phase 7) is the per-step model id the runner wants the
	// backend to use INSTEAD of the agent's configured Model. Empty string
	// means "use the agent default". AgentFuncs that honour this MUST NOT
	// mutate agents.AgentDef — the override is request-scoped only.
	ModelOverride string
}

// AgentFunc executes an agent run headlessly and returns the raw output.
// In production this wraps agent.Orchestrator; in tests it's stubbed.
type AgentFunc func(ctx context.Context, opts RunOptions) (output string, err error)

// BroadcastFunc is called after storing a notification to push WS events.
// Nil means no broadcast (used in tests or pre-wiring).
type BroadcastFunc func(n *notification.Notification, pendingCount int)

// NotifyFunc is called after storing a notification to emit a system tray alert.
// Nil means no tray notification.
type NotifyFunc func(title, message string)

// WorkflowRunner executes a Workflow, running steps in order.
type WorkflowRunner func(ctx context.Context, w *Workflow) error

// WorkflowBroadcastFunc pushes WS events for workflow lifecycle.
type WorkflowBroadcastFunc func(eventType string, payload map[string]any)

// SpaceDeliveryFunc posts a notification summary to an internal Huginn Space.
// Nil means no space delivery.
type SpaceDeliveryFunc func(spaceID, summary, detail string) error

// AgentDMDeliveryFunc posts a notification as a DM authored by `agentName` to
// the recipient `user`. The binding (typically wired in main.go) is
// responsible for resolving (or creating) the DM space, persisting the
// message with agent authorship, and broadcasting the appropriate WS event.
// Nil means agent_dm deliveries are silently skipped (no panic, no error).
type AgentDMDeliveryFunc func(agentName, user, summary, detail string) error

// DeliveryFailureFunc is called on the workflow goroutine when a delivery attempt fails.
// It MUST be non-blocking (channel send, no I/O). redactedTarget has PII stripped:
// webhook → scheme+host, email → *@domain.
type DeliveryFailureFunc func(workflowID, runID, deliveryType, redactedTarget, errMsg string)

// redactTarget strips PII from delivery targets before broadcasting over WS.
// Webhook URLs are redacted to scheme+host. Email addresses to *@domain.
func redactTarget(raw, deliveryType string) string {
	switch deliveryType {
	case "webhook":
		u, err := url.Parse(raw)
		if err != nil {
			return "(redacted)"
		}
		return u.Scheme + "://" + u.Host
	case "email":
		at := strings.LastIndex(raw, "@")
		if at < 0 {
			return "(redacted)"
		}
		return "*@" + raw[at+1:]
	default:
		return "(redacted)"
	}
}

// MakeWorkflowRunner builds a WorkflowRunner that:
//  1. Executes steps in Position order, calling agentFn for each inline step.
//  2. Respects per-step on_failure: stop|continue.
//  3. Appends the completed WorkflowRun to runStore.
//  4. Broadcasts WS events via broadcast (may be nil).
//
// huginnDir is used for writing dead-letter records when webhook/email delivery fails;
// pass "" to disable dead-letter writes (failures are still captured in the notification store).
// deliverers is the registry for external notification channels (webhook, email); nil disables them.
func MakeWorkflowRunner(
	runStore WorkflowRunStoreInterface,
	agentFn AgentFunc,
	notifStore notification.StoreInterface,
	broadcast WorkflowBroadcastFunc,
	notifyFn NotifyFunc,
	spaceDeliveryFn SpaceDeliveryFunc,
	huginnDir string,
	deliverers *DelivererRegistry,
	onDeliveryFailure DeliveryFailureFunc,
	opts ...RunnerOption,
) WorkflowRunner {
	cfg := defaultRunnerConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return func(ctx context.Context, w *Workflow) error {
		// Sort steps by Position ascending.
		steps := make([]WorkflowStep, len(w.Steps))
		copy(steps, w.Steps)
		sort.Slice(steps, func(i, j int) bool { return steps[i].Position < steps[j].Position })
		// Phase 8: apply workflow-level retry defaults BEFORE running so
		// every step picks up the inherited values. This mutates the local
		// `steps` slice only — the on-disk YAML is untouched.
		ApplyRetryDefaults(steps, w.Retry)

		stepOutputs := map[string]string{}
		// Run scratchpad: a per-run key/value store accessible to all steps via
		// {{run.scratch.KEY}} placeholders. Populated by the SetScratch tool
		// (Phase 2) so an agent in step N can stash a value that step M reads.
		// Lives only for the duration of this run; never persisted.
		runScratch := map[string]string{}
		// Phase 5: seed scratch from trigger-supplied inputs so the very first
		// step can reference {{run.scratch.KEY}} without a predecessor step.
		// Used by manual runs (POST /run with body) and webhook triggers.
		for k, v := range initialInputs(ctx) {
			runScratch[k] = v
		}
		var prevOutput string
		var anyStepFailed bool

		run := &WorkflowRun{
			ID:         fmt.Sprintf("wf-%s-%d", w.ID, time.Now().UnixMilli()),
			WorkflowID: w.ID,
			Status:     WorkflowRunStatusRunning,
			StartedAt:  time.Now().UTC(),
		}
		// Phase 6 (run analytics): record the trigger inputs and a workflow
		// snapshot so the run can be replayed or forked with high fidelity.
		// We snapshot the FULL definition (steps + chains + notifications)
		// because future edits to the YAML file shouldn't change "what
		// already ran". The snapshot is best-effort — a copy via JSON
		// round-trip — so cycles or unmarshallable fields would fail loudly
		// at storage time, but Workflow itself is JSON-tagged end-to-end.
		if inputs := initialInputs(ctx); len(inputs) > 0 {
			cp := make(map[string]string, len(inputs))
			for k, v := range inputs {
				cp[k] = v
			}
			run.TriggerInputs = cp
		}
		run.WorkflowSnapshot = cloneWorkflow(w)

		if broadcast != nil {
			broadcast("workflow_started", map[string]any{
				"workflow_id":   w.ID,
				"workflow_name": w.Name,
				"run_id":        run.ID,
			})
		}

		var aborted bool
		var cancelled bool // true only when CancelWorkflow was called explicitly by a user
		for _, step := range steps {
			// Check for context cancellation before starting each step.
			// This handles server shutdown, timeout, or explicit user cancellation.
			select {
			case <-ctx.Done():
				cause := context.Cause(ctx)
				if errors.Is(cause, errUserCancelled) {
					cancelled = true
					slog.Info("scheduler: workflow run cancelled by user",
						"workflow_id", w.ID, "run_id", run.ID)
				} else {
					slog.Warn("scheduler: workflow run cancelled by context",
						"workflow_id", w.ID, "run_id", run.ID, "cause", cause)
				}
				aborted = true
			default:
			}
			if aborted {
				break
			}

			// Phase 8: conditional execution. Resolve {{...}} placeholders in
			// the When expression first, then evaluate truthiness. Skipping
			// emits a workflow_skipped WS event and persists the step record
			// with Status="skipped"; downstream steps still execute.
			if strings.TrimSpace(step.When) != "" {
				resolvedWhen := resolveInlineVars(step.When, step.Vars)
				resolvedWhen = resolveRuntimeVars(resolvedWhen, step.Inputs, stepOutputs, prevOutput, runScratch)
				if !EvaluateWhen(resolvedWhen) {
					skippedName := step.Name
					if skippedName == "" {
						skippedName = fmt.Sprintf("step-%d", step.Position)
					}
					stepStartedAt := time.Now().UTC()
					stepCompletedAt := stepStartedAt
					skippedResult := WorkflowStepResult{
						Position:     step.Position,
						Slug:         skippedName,
						Status:       "skipped",
						StartedAt:    stepStartedAt,
						CompletedAt:  &stepCompletedAt,
						SkipReason:   "when_false",
						WhenResolved: resolvedWhen,
					}
					run.Steps = append(run.Steps, skippedResult)
					stepOutputs[skippedName] = "" // skipped → empty so downstream {{inputs.x}} stays well-defined
					if broadcast != nil {
						broadcast("workflow_skipped", map[string]any{
							"workflow_id":   w.ID,
							"run_id":        run.ID,
							"position":      step.Position,
							"slug":          skippedName,
							"reason":        "when_false",
							"when_resolved": resolvedWhen,
						})
					}
					continue
				}
			}

			var stepResult WorkflowStepResult

			if step.Routine != "" {
				// Legacy slug path: not supported in unified model.
				stepResult = WorkflowStepResult{
					Position: step.Position,
					Slug:     step.Routine,
					Status:   "failed",
					Error:    "legacy routine slug references not supported; please update to inline steps",
				}
				anyStepFailed = true
				run.Steps = append(run.Steps, stepResult)
				if broadcast != nil {
					broadcast("workflow_step_complete", map[string]any{
						"workflow_id": w.ID, "run_id": run.ID,
						"position": step.Position, "slug": step.Routine,
						"status": "failed", "error": stepResult.Error,
					})
				}
				if step.EffectiveOnFailure() == "stop" {
					aborted = true
					break
				}
				continue
			}

			// Phase 8: sub-workflow step. The parent passes its current
			// scratchpad as the child's initialInputs so {{run.scratch.KEY}}
			// references survive across the call boundary. The child's
			// last-step output becomes this step's output, which then flows
			// to the next sibling via {{prev.output}}.
			if strings.TrimSpace(step.SubWorkflow) != "" {
				subID := strings.TrimSpace(step.SubWorkflow)
				subName := step.Name
				if subName == "" {
					subName = fmt.Sprintf("step-%d", step.Position)
				}
				stepStartedAt := time.Now().UTC()
				if cfg.subWorkflow == nil {
					stepCompletedAt := time.Now().UTC()
					stepResult = WorkflowStepResult{
						Position:    step.Position,
						Slug:        subName,
						Status:      "failed",
						Error:       "sub_workflow support not configured",
						StartedAt:   stepStartedAt,
						CompletedAt: &stepCompletedAt,
					}
					anyStepFailed = true
					run.Steps = append(run.Steps, stepResult)
					if broadcast != nil {
						broadcast("workflow_step_complete", map[string]any{
							"workflow_id": w.ID, "run_id": run.ID,
							"position": step.Position, "slug": subName,
							"status": "failed", "error": stepResult.Error,
						})
					}
					if step.EffectiveOnFailure() == "stop" {
						aborted = true
						break
					}
					continue
				}
				// Snapshot scratch so a child mutation doesn't race the parent map.
				childInputs := make(map[string]string, len(runScratch))
				for k, v := range runScratch {
					childInputs[k] = v
				}
				if broadcast != nil {
					broadcast("workflow_step_started", map[string]any{
						"workflow_id": w.ID, "run_id": run.ID,
						"position": step.Position, "slug": subName,
						"sub_workflow": subID,
					})
				}
				output, subErr := cfg.subWorkflow(ctx, subID, childInputs)
				stepCompletedAt := time.Now().UTC()
				if subErr != nil {
					stepResult = WorkflowStepResult{
						Position:    step.Position,
						Slug:        subName,
						Status:      "failed",
						Error:       fmt.Sprintf("sub_workflow %s: %v", subID, subErr),
						StartedAt:   stepStartedAt,
						CompletedAt: &stepCompletedAt,
					}
					anyStepFailed = true
					run.Steps = append(run.Steps, stepResult)
					if broadcast != nil {
						broadcast("workflow_step_complete", map[string]any{
							"workflow_id": w.ID, "run_id": run.ID,
							"position": step.Position, "slug": subName,
							"status": "failed", "error": stepResult.Error,
						})
					}
					if step.EffectiveOnFailure() == "stop" {
						aborted = true
						break
					}
					continue
				}
				stepResult = WorkflowStepResult{
					Position:    step.Position,
					Slug:        subName,
					Status:      "success",
					Output:      output,
					StartedAt:   stepStartedAt,
					CompletedAt: &stepCompletedAt,
				}
				stepOutputs[subName] = output
				prevOutput = output
				run.Steps = append(run.Steps, stepResult)
				if broadcast != nil {
					broadcast("workflow_step_complete", map[string]any{
						"workflow_id": w.ID, "run_id": run.ID,
						"position": step.Position, "slug": subName,
						"status": "success",
					})
				}
				continue
			}

			if step.Prompt != "" {
				// Inline step path.
				resolvedPrompt := resolveInlineVars(step.Prompt, step.Vars)
				resolvedPrompt = resolveRuntimeVars(resolvedPrompt, step.Inputs, stepOutputs, prevOutput, runScratch)
				// Detect any placeholders that were not resolved. Sending a prompt
				// with literal "{{inputs.result}}" tokens to the agent produces
				// confusing, garbage-in/garbage-out behaviour. Fail the step
				// immediately with a clear diagnostic instead.
				if remaining := unresolvedPlaceholderRe.FindAllString(resolvedPrompt, -1); len(remaining) > 0 {
					placeholderList := strings.Join(remaining, ", ")
					errMsg := fmt.Sprintf("%s: %s", errUnresolvedPlaceholder, placeholderList)
					slog.Error("scheduler: step has unresolved placeholders; failing step",
						"workflow_id", w.ID, "step", step.Name, "placeholders", placeholderList)
					earlyName := step.Name
					if earlyName == "" {
						earlyName = fmt.Sprintf("step-%d", step.Position)
					}
					stepResult = WorkflowStepResult{
						Position: step.Position,
						Slug:     earlyName,
						Status:   "failed",
						Error:    errMsg,
					}
					anyStepFailed = true
					run.Steps = append(run.Steps, stepResult)
					if broadcast != nil {
						broadcast("workflow_step_complete", map[string]any{
							"workflow_id": w.ID, "run_id": run.ID,
							"position": step.Position, "slug": earlyName,
							"status": "failed", "error": errMsg,
						})
					}
					if step.EffectiveOnFailure() == "stop" {
						aborted = true
						break
					}
					continue
				}

				// Add JSON summary instruction.
				jsonInstruction := `Respond with a JSON block first on its own line: {"summary": "<one line, max 120 chars>"}` + "\nThen your full analysis."
				fullPrompt := resolvedPrompt + "\n\n" + jsonInstruction
				stepName := step.Name
				if stepName == "" {
					// NOTE: Inputs.FromStep references steps by name. A step with no
					// name is keyed in stepOutputs as "step-N". Any other step whose
					// Inputs.FromStep value does not match that generated key will
					// silently fail to resolve — resolveRuntimeVars will warn about it.
					stepName = fmt.Sprintf("step-%d", step.Position)
				}
				runID := fmt.Sprintf("wf-%s-step-%d-%d", w.ID, step.Position, time.Now().UnixMilli())

				// Token streaming (Phase 1.3). Throttle emission so a long
				// model output doesn't flood the WS bus: flush whenever the
				// pending buffer reaches 256 chars OR 100ms have elapsed since
				// the last flush. The runner pre-builds the closure so each
				// step gets its own buffer + last-flush timestamp.
				var (
					tokBufMu     sync.Mutex
					tokBuf       strings.Builder
					tokLastFlush = time.Now()
				)
				flushTokens := func(force bool) {
					tokBufMu.Lock()
					if tokBuf.Len() == 0 {
						tokBufMu.Unlock()
						return
					}
					if !force && tokBuf.Len() < 256 && time.Since(tokLastFlush) < 100*time.Millisecond {
						tokBufMu.Unlock()
						return
					}
					chunk := tokBuf.String()
					tokBuf.Reset()
					tokLastFlush = time.Now()
					tokBufMu.Unlock()
					if broadcast != nil {
						broadcast("workflow_step_token", map[string]any{
							"workflow_id":   w.ID,
							"run_id":        runID,
							"step_name":     stepName,
							"step_position": step.Position,
							"token":         chunk,
						})
					}
				}
				onToken := func(t string) {
					tokBufMu.Lock()
					tokBuf.WriteString(t)
					tokBufMu.Unlock()
					flushTokens(false)
				}

				opts := RunOptions{
					RunID:         runID,
					AgentName:     step.Agent,
					Prompt:        fullPrompt,
					Connections:   step.Connections,
					WorkflowID:    w.ID,
					StepName:      stepName,
					StepPosition:  step.Position,
					OnToken:       onToken,
					ModelOverride: step.ModelOverride,
				}
				// Apply per-step timeout if set. The step context is always a child
			// of the workflow context so the workflow-level deadline still wins.
			stepCtx := ctx
			stepHadOwnTimeout := false
			if d := step.TimeoutDuration(); d > 0 {
				var stepCancel context.CancelFunc
				stepCtx, stepCancel = context.WithTimeout(ctx, d)
				defer stepCancel()
				stepHadOwnTimeout = true
			}
			// Plumb a scratchpad writer onto the step context. Tools (e.g.
			// future set_scratch PromptTool) read this via ScratchSetter and
			// can mutate the live runScratch map. Mutex-free is safe because
			// linear workflows execute one step at a time; when fan-out lands
			// (Phase 8) this needs a sync.Map or per-call mutex.
			scratchSetter := func(k, v string) error {
				runScratch[k] = v
				return nil
			}
			stepCtx = WithScratchSetter(stepCtx, scratchSetter)
			stepStartedAt := time.Now().UTC()
			output, agentErr := executeStepWithRetry(stepCtx, agentFn, opts, step)
			stepCompletedAt := time.Now().UTC()
			stepLatencyMs := stepCompletedAt.Sub(stepStartedAt).Milliseconds()
			// Final flush: emit any tokens still buffered when the agent
			// finished so the live UI sees the complete output.
			flushTokens(true)
			// Annotate the error so operators know which deadline fired.
			if agentErr != nil && stepHadOwnTimeout && errors.Is(agentErr, context.DeadlineExceeded) {
				agentErr = fmt.Errorf("step %q timed out after %s: %w", step.Name, step.TimeoutDuration(), agentErr)
			}
				if agentErr == nil && len(output) > maxStepOutputBytes {
					slog.Warn("workflow step output truncated", "step", stepName, "original_bytes", len(output), "cap_bytes", maxStepOutputBytes)
					output = output[:maxStepOutputBytes] + "\n[output truncated]"
				}
				sessionID := runID
				stepResult = WorkflowStepResult{
					Position:    step.Position,
					Slug:        stepName,
					SessionID:   sessionID,
					StartedAt:   stepStartedAt,
					CompletedAt: &stepCompletedAt,
					LatencyMs:   stepLatencyMs,
				}
				if agentErr != nil {
					stepResult.Status = "failed"
					stepResult.Error = agentErr.Error()
					anyStepFailed = true
					// NOTE: When a step fails, stepOutputs and prevOutput are NOT
					// updated. The next step's {{prev.output}} will resolve to the
					// output of the most recent SUCCESSFUL step (or "" if no prior
					// step succeeded). This is intentional: a failed step has no
					// output to propagate.
				} else {
					stepResult.Status = "success"
					stepOutputs[stepName] = output
					stepResult.Output = output
					prevOutput = output
				}

				if step.Notify != nil {
					stepSucceeded := agentErr == nil
					if (stepSucceeded && step.Notify.OnSuccess) || (!stepSucceeded && step.Notify.OnFailure) {
						sev := parseSeverity(w.Notification.Severity)
						pos := step.Position
						// Phase 2: prefer the agent's own one-line {"summary":"..."}
						// JSON block (which the runner explicitly instructs the agent
						// to emit) for the notification headline. Falls back to the
						// generic "[wf] Step N: name" when parseOutput can't find one.
						headline := fmt.Sprintf("[%s] Step %d: %s", w.Name, step.Position, stepName)
						body := output
						if stepSucceeded {
							if sum, det := parseOutput(output); sum != "" && sum != "(no output)" {
								headline = fmt.Sprintf("[%s] %s", w.Name, sum)
								// Use the agent's own narrative as the body when present;
								// otherwise keep the full output so operators see context.
								if det != "" {
									body = det
								}
							}
						}
						n := &notification.Notification{
							ID:           notification.NewID(),
							RunID:        runID,
							WorkflowID:   w.ID,
							Summary:      headline,
							Severity:     sev,
							Status:       notification.StatusPending,
							StepPosition: &pos,
							StepName:     stepName,
							CreatedAt:    time.Now().UTC(),
							UpdatedAt:    time.Now().UTC(),
						}
						if agentErr != nil {
							n.Detail = agentErr.Error()
						} else {
							// For a successful step, populate Detail with the agent output.
							// An empty output (e.g. agent returned nothing) results in an
							// empty Detail string — handled gracefully downstream: space
							// delivery receives "" which is valid, and inbox is always written.
							n.Detail = body
						}
						deliveries := dispatchNotification(n, step.Notify.DeliverTo, notifStore, spaceDeliveryFn, cfg.agentDM, step.Agent, huginnDir, deliverers, onDeliveryFailure)
						n.Deliveries = deliveries
						if notifStore != nil {
							if putErr := notifStore.Put(n); putErr != nil {
								slog.Warn("scheduler: failed to store step notification",
									"workflow_id", w.ID, "step", stepName, "err", putErr)
							}
						}
					}
				}

				run.Steps = append(run.Steps, stepResult)

				if broadcast != nil {
					broadcast("workflow_step_complete", map[string]any{
						"workflow_id": w.ID, "run_id": run.ID,
						"position": step.Position, "slug": stepName,
						"status": stepResult.Status, "session_id": sessionID,
						"latency_ms": stepLatencyMs,
					})
				}

				if agentErr != nil && step.EffectiveOnFailure() == "stop" {
					aborted = true
					break
				}
				continue
			}

			// Neither Routine slug nor Prompt: fail the step.
			stepResult = WorkflowStepResult{
				Position: step.Position,
				Slug:     fmt.Sprintf("step-%d", step.Position),
				Status:   "failed",
				Error:    fmt.Sprintf("error: step at position %d has neither routine slug nor inline prompt", step.Position),
			}
			anyStepFailed = true
			run.Steps = append(run.Steps, stepResult)
			if broadcast != nil {
				broadcast("workflow_step_complete", map[string]any{
					"workflow_id": w.ID, "run_id": run.ID,
					"position": step.Position,
					"status":   "failed", "error": stepResult.Error,
				})
			}
			if step.EffectiveOnFailure() == "stop" {
				aborted = true
				break
			}
		}

		now := time.Now().UTC()
		run.CompletedAt = &now
		if cancelled {
			// Explicit user cancellation — distinct from a step-failure abort.
			run.Status = WorkflowRunStatusCancelled
		} else if aborted {
			run.Status = WorkflowRunStatusFailed
		} else if anyStepFailed {
			// At least one step failed with on_failure: continue, so all steps
			// were attempted but the run is not fully successful. Use "partial"
			// rather than "complete" to signal the mixed outcome to the caller.
			run.Status = WorkflowRunStatusPartial
		} else {
			run.Status = WorkflowRunStatusComplete
		}

		// Phase 4/observability: emit terminal metrics once the status is
		// final. We deliberately do this BEFORE persistence and notification
		// so a slow notification path can't skew the latency histogram.
		if cfg.metrics != nil {
			cfg.metrics.Record("huginn.workflow.run.total", 1, "status:"+string(run.Status), "workflow_id:"+w.ID)
			latencyMs := float64(now.Sub(run.StartedAt).Milliseconds())
			if latencyMs >= 0 {
				cfg.metrics.Histogram("huginn.workflow.run.latency_ms", latencyMs, "status:"+string(run.Status), "workflow_id:"+w.ID)
			}
			for _, sr := range run.Steps {
				cfg.metrics.Record("huginn.workflow.step.total", 1, "status:"+sr.Status, "workflow_id:"+w.ID)
				// Skipped steps have no meaningful latency; exclude them from
				// the histogram so percentiles describe real work.
				if sr.Status == "skipped" {
					continue
				}
				if sr.LatencyMs > 0 {
					cfg.metrics.Histogram("huginn.workflow.step.latency_ms", float64(sr.LatencyMs), "status:"+sr.Status, "workflow_id:"+w.ID)
				}
			}
		}

		// Workflow-level notification.
		// "succeeded" means all steps passed (complete). Partial and failed
		// both count as non-success for notification routing purposes.
		wfSucceeded := run.Status == WorkflowRunStatusComplete
		if notifStore != nil && ((wfSucceeded && w.Notification.OnSuccess) || (!wfSucceeded && w.Notification.OnFailure)) {
			sev := parseSeverity(w.Notification.Severity)
			summary := fmt.Sprintf("[%s] %s", w.Name, string(run.Status))
			detail := buildWorkflowRunDetail(run)
			n := &notification.Notification{
				ID:            notification.NewID(),
				RunID:         run.ID,
				WorkflowID:    w.ID,
				WorkflowRunID: run.ID,
				Summary:       summary,
				Detail:        detail,
				Severity:      sev,
				Status:        notification.StatusPending,
				CreatedAt:     time.Now().UTC(),
				UpdatedAt:     time.Now().UTC(),
			}
			// Workflow-level notifications have no single agent author — use ""
			// so the AgentDMDeliveryFunc binding can decide a default
			// (e.g. the workflow's lead agent).
			deliveries := dispatchNotification(n, w.Notification.DeliverTo, notifStore, spaceDeliveryFn, cfg.agentDM, "", huginnDir, deliverers, onDeliveryFailure)
			n.Deliveries = deliveries
			// Persist the run record FIRST — it is the authoritative historical record.
			// On a crash between these two writes the run still exists and the user
			// can inspect it; the missing notification is recoverable. The prior
			// ordering (notification first) left orphaned inbox entries with no
			// corresponding run — the harder inconsistency to diagnose.
			if err := runStore.Append(w.ID, run); err != nil {
				slog.Error("scheduler: failed to persist workflow run",
					"workflow_id", w.ID, "run_id", run.ID, "err", err)
			}
			if putErr := notifStore.Put(n); putErr != nil {
				slog.Warn("scheduler: failed to store workflow notification",
					"workflow_id", w.ID, "run_id", run.ID, "err", putErr)
			}
		} else {
			// No notification configured — persist the run unconditionally.
			if err := runStore.Append(w.ID, run); err != nil {
				slog.Error("scheduler: failed to persist workflow run",
					"workflow_id", w.ID, "run_id", run.ID, "err", err)
			}
		}

		eventType := "workflow_complete"
		if cancelled {
			// Explicit user cancellation — emit a dedicated event type so the
			// frontend can transition from "cancelling" to "cancelled".
			eventType = "workflow_cancelled"
		} else if aborted {
			// aborted means a step failed with on_failure: stop — the run is "failed".
			eventType = "workflow_failed"
		} else if run.Status == WorkflowRunStatusPartial {
			// partial: all steps attempted but at least one failed with on_failure: continue.
			eventType = "workflow_partial"
		}
		if broadcast != nil {
			broadcast(eventType, map[string]any{
				"workflow_id": w.ID, "run_id": run.ID, "status": string(run.Status),
			})
		}
		if notifyFn != nil && !cancelled && (aborted || run.Status == WorkflowRunStatusPartial) {
			msg := "Workflow failed — check Inbox"
			if run.Status == WorkflowRunStatusPartial {
				msg = "Workflow completed with failures — check Inbox"
			}
			notifyFn(w.Name, msg)
		}

		// Phase 5: workflow chaining. The hook decides (based on parent.Chain
		// and run.Status) whether to trigger a downstream workflow. The hook
		// runs synchronously after persistence + WS broadcast so the
		// downstream watcher sees the parent transition first; the trigger
		// call itself dispatches to a goroutine inside the scheduler.
		if cfg.chainTrigger != nil && !cancelled {
			cfg.chainTrigger(w, run)
		}

		return nil
	}
}

// resolveInlineVars substitutes {{KEY}} placeholders in prompt with values from vars.
func resolveInlineVars(prompt string, vars map[string]string) string {
	result := prompt
	for k, v := range vars {
		result = strings.ReplaceAll(result, "{{"+k+"}}", v)
	}
	return result
}

// resolveRuntimeVars substitutes runtime step outputs into the prompt.
// Resolution order: static vars first (already done by resolveInlineVars),
// then JSON field access on named inputs and prev (Phase 2), then plain
// {{inputs.alias}} / {{prev.output}}, then {{run.scratch.KEY}}.
//
// JSON field access semantics (Phase 2):
//
//	{{prev.output.field}}       → JSON decode prevOutput, look up "field"
//	{{inputs.alias.field.sub}}  → JSON decode inputs[alias], walk "field.sub"
//
// Decoded field values are rendered back to a string. Strings/numbers/bools
// render verbatim; objects/arrays re-marshal to compact JSON so downstream
// steps still get well-formed JSON they can re-parse. When the source isn't
// valid JSON or the path doesn't exist the placeholder is left intact and a
// warning is logged — the runner will then trip the unresolved-placeholder
// check and fail the step rather than feeding garbage to the agent.
//
// Edge cases:
//   - Entries where As == "" are skipped; substituting into "{{inputs.}}" is
//     never intentional and would silently corrupt the prompt.
//   - Entries where FromStep == "" are skipped with a warning; there is nothing
//     to look up.
//   - When FromStep references a step that has not yet run or does not exist the
//     placeholder is left unreplaced (no-op) and a warning is logged so the
//     operator can diagnose misconfigured workflows without silent data loss.
//   - On the first step prevOutput is "" so {{prev.output}} is replaced with an
//     empty string.  This is intentional: downstream steps should not receive a
//     literal "{{prev.output}}" token in their prompts.
//   - scratch may be nil; in that case all {{run.scratch.*}} placeholders are
//     left unresolved (which causes the step to fail per the unresolved-placeholder
//     guard, which is the desired behaviour — a missing key is a bug).
func resolveRuntimeVars(prompt string, inputs []StepInput, stepOutputs map[string]string, prevOutput string, scratch map[string]string) string {
	// Pre-process JSON field-access placeholders BEFORE the literal substitutions
	// below. That way a JSON output like '{"items":[1,2]}' with a literal
	// `{{prev.output}}` placeholder elsewhere in the prompt still gets the raw
	// JSON inserted, while `{{prev.output.items}}` resolves to the field.
	result := resolveJSONFieldAccess(prompt, inputs, stepOutputs, prevOutput)

	for _, inp := range inputs {
		if inp.As == "" {
			slog.Warn("scheduler: resolveRuntimeVars: skipping input with empty As field",
				"from_step", inp.FromStep)
			continue
		}
		if inp.FromStep == "" {
			slog.Warn("scheduler: resolveRuntimeVars: skipping input with empty FromStep field",
				"as", inp.As)
			continue
		}
		if out, ok := stepOutputs[inp.FromStep]; ok {
			result = strings.ReplaceAll(result, "{{inputs."+inp.As+"}}", out)
		} else {
			slog.Warn("scheduler: resolveRuntimeVars: FromStep not found in stepOutputs; placeholder left unreplaced",
				"from_step", inp.FromStep, "as", inp.As)
		}
	}
	result = strings.ReplaceAll(result, "{{prev.output}}", prevOutput)
	// Run scratchpad: {{run.scratch.KEY}} → scratch[KEY]. Missing keys are left
	// unresolved by design so the placeholder-guard fails the step.
	if len(scratch) > 0 {
		for k, v := range scratch {
			result = strings.ReplaceAll(result, "{{run.scratch."+k+"}}", v)
		}
	}
	return result
}

// jsonPlaceholderRe matches {{prev.output.PATH}} and {{inputs.ALIAS.PATH}} where
// PATH is a dotted sequence of identifier characters. The capture groups are:
//
//	1: empty for prev, alias name for inputs
//	2: dotted path (no leading dot)
var jsonPlaceholderRe = regexp.MustCompile(`\{\{(?:prev\.output|inputs\.([A-Za-z_][A-Za-z0-9_]*))\.([A-Za-z_][A-Za-z0-9_.]*)\}\}`)

// resolveJSONFieldAccess walks every {{prev.output.PATH}} / {{inputs.ALIAS.PATH}}
// occurrence in prompt and replaces it with the dotted-path lookup result on the
// JSON-decoded source. Unknown sources / parse failures / missing paths leave the
// placeholder untouched so downstream guards can fail the step instead of
// silently producing garbled prompts.
func resolveJSONFieldAccess(prompt string, inputs []StepInput, stepOutputs map[string]string, prevOutput string) string {
	aliasToFromStep := make(map[string]string, len(inputs))
	for _, inp := range inputs {
		if inp.As != "" && inp.FromStep != "" {
			aliasToFromStep[inp.As] = inp.FromStep
		}
	}
	return jsonPlaceholderRe.ReplaceAllStringFunc(prompt, func(match string) string {
		groups := jsonPlaceholderRe.FindStringSubmatch(match)
		if len(groups) != 3 {
			return match
		}
		alias, path := groups[1], groups[2]
		var src string
		if alias == "" {
			src = prevOutput
		} else {
			fromStep, ok := aliasToFromStep[alias]
			if !ok {
				slog.Warn("scheduler: JSON field access: unknown alias", "alias", alias)
				return match
			}
			out, ok := stepOutputs[fromStep]
			if !ok {
				return match
			}
			src = out
		}
		val, ok := lookupJSONPath(src, path)
		if !ok {
			return match
		}
		return val
	})
}

// lookupJSONPath JSON-decodes src and walks dotted path. Returns the rendered
// string and true on success. Strings/numbers/bools render verbatim; objects
// and arrays re-marshal to compact JSON so downstream steps still see
// structured data. Returns ("", false) on any decode/lookup error so the
// caller can leave the placeholder intact.
func lookupJSONPath(src, path string) (string, bool) {
	src = strings.TrimSpace(src)
	if src == "" {
		return "", false
	}
	var root any
	if err := json.Unmarshal([]byte(src), &root); err != nil {
		return "", false
	}
	cur := root
	for _, seg := range strings.Split(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		cur, ok = obj[seg]
		if !ok {
			return "", false
		}
	}
	switch v := cur.(type) {
	case string:
		return v, true
	case float64:
		// json.Unmarshal renders all numbers as float64 — render back without
		// trailing ".0" for integer-valued floats so prompts read naturally.
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v)), true
		}
		return fmt.Sprintf("%g", v), true
	case bool:
		if v {
			return "true", true
		}
		return "false", true
	case nil:
		return "null", true
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", false
		}
		return string(b), true
	}
}

// parseOutput extracts summary and detail from agent output.
// Expects the agent to emit a JSON block {"summary":"..."} on the first non-empty line.
// Falls back to truncating the first non-empty line as the summary.
func parseOutput(output string) (summary, detail string) {
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Try to parse JSON block.
		if strings.HasPrefix(line, "{") {
			var block struct {
				Summary string `json:"summary"`
			}
			if err := json.Unmarshal([]byte(line), &block); err == nil && block.Summary != "" {
				summary = truncate(block.Summary, 120)
				detail = strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
				return summary, detail
			}
		}
		// Fallback: use first non-empty line as summary, rest as detail.
		summary = truncate(line, 120)
		detail = strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
		return summary, detail
	}
	return "(no output)", output
}

// parseSeverity converts a YAML severity string to notification.Severity.
// Defaults to SeverityInfo for unknown values.
func parseSeverity(s string) notification.Severity {
	switch strings.ToLower(s) {
	case "warning":
		return notification.SeverityWarning
	case "urgent":
		return notification.SeverityUrgent
	default:
		return notification.SeverityInfo
	}
}

// truncate returns s truncated to max runes.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// dispatchNotification stores the notification in notifStore (inbox) and delivers
// to any configured targets (space, agent_dm, webhook, email). Returns the
// delivery records. huginnDir is used for dead-letter JSONL on external
// delivery failure; pass "" to skip. deliverers may be nil, in which case
// webhook/email targets are logged and skipped. agentDMFn may be nil, in
// which case agent_dm targets are skipped with a warning.
//
// stepAgent is the name of the agent that produced this notification — used as
// the default `From` author when a delivery target leaves Author empty.
func dispatchNotification(
	n *notification.Notification,
	targets []NotificationDelivery,
	notifStore notification.StoreInterface,
	spaceDeliveryFn SpaceDeliveryFunc,
	agentDMFn AgentDMDeliveryFunc,
	stepAgent string,
	huginnDir string,
	deliverers *DelivererRegistry,
	onDeliveryFailure DeliveryFailureFunc,
) []notification.DeliveryRecord {
	// Inbox is always implicit.
	records := []notification.DeliveryRecord{
		{Type: "inbox", Target: "inbox", Status: "sent", SentAt: time.Now().UTC()},
	}
	for _, t := range targets {
		// Resolve author: per-target From overrides step agent.
		author := t.From
		if author == "" {
			author = stepAgent
		}
		switch t.Type {
		case "space":
			if t.SpaceID == "" || spaceDeliveryFn == nil {
				continue
			}
			rec := notification.DeliveryRecord{
				Type:   "space",
				Target: t.SpaceID,
				SentAt: time.Now().UTC(),
			}
			if err := spaceDeliveryFn(t.SpaceID, n.Summary, n.Detail); err != nil {
				rec.Status = "failed"
				rec.Error = err.Error()
			} else {
				rec.Status = "sent"
			}
			records = append(records, rec)
		case "agent_dm":
			if agentDMFn == nil {
				slog.Warn("scheduler: agent_dm delivery skipped — agent DM binding not configured",
					"workflow_id", n.WorkflowID, "step", n.StepName)
				continue
			}
			if author == "" {
				slog.Warn("scheduler: agent_dm delivery skipped — no agent author resolved",
					"workflow_id", n.WorkflowID, "step", n.StepName)
				continue
			}
			rec := notification.DeliveryRecord{
				Type:   "agent_dm",
				Target: author + "→" + t.User,
				SentAt: time.Now().UTC(),
			}
			if err := agentDMFn(author, t.User, n.Summary, n.Detail); err != nil {
				rec.Status = "failed"
				rec.Error = err.Error()
			} else {
				rec.Status = "sent"
			}
			records = append(records, rec)
		case "inbox":
			// Inbox is always implicit (already recorded above); explicit entry is a no-op.
		case "webhook", "email":
			if deliverers == nil {
				slog.Warn("scheduler: deliverer registry not configured, skipping", "type", t.Type, "workflow_id", n.WorkflowID)
				continue
			}
			d := deliverers.get(t.Type)
			if d == nil {
				slog.Warn("scheduler: unknown delivery type", "type", t.Type, "workflow_id", n.WorkflowID)
				continue
			}
			// Delivery is synchronous within the workflow goroutine.
			// The 30-minute workflow timeout provides ample headroom for retries
			// (webhook: up to ~37s, email: up to ~40s worst-case with backoff).
			rec := d.Deliver(context.Background(), n, t)
			if rec.Status == "failed" && onDeliveryFailure != nil {
				// Non-blocking: onDeliveryFailure must not block (contract).
				// Target is redacted to avoid leaking PII/secrets over WS.
				onDeliveryFailure(n.WorkflowID, n.RunID, t.Type, redactTarget(t.To, t.Type), rec.Error)
			}
			records = append(records, rec)
			if rec.Status != "sent" && huginnDir != "" {
				WriteDeliveryFailure(huginnDir, n.WorkflowID, n.RunID, t.To, 3, rec.Error)
			}
		default:
			slog.Warn("scheduler: unrecognised delivery type", "type", t.Type, "workflow_id", n.WorkflowID)
		}
	}
	return records
}

// buildWorkflowRunDetail builds a human-readable markdown summary of a workflow run.
// Icon legend: ✓ success, ✗ failed, ~ skipped, ? unknown status.
// When the run has no steps a note is emitted instead of an empty list.
func buildWorkflowRunDetail(run *WorkflowRun) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Run ID:** %s\n", run.ID))
	sb.WriteString(fmt.Sprintf("**Status:** %s\n\n", string(run.Status)))
	sb.WriteString("**Steps:**\n")
	if len(run.Steps) == 0 {
		sb.WriteString("_(no steps recorded)_\n")
		return sb.String()
	}
	for _, s := range run.Steps {
		var icon string
		switch s.Status {
		case "success":
			icon = "✓"
		case "failed":
			icon = "✗"
		case "skipped":
			icon = "~"
		default:
			icon = "?"
		}
		sb.WriteString(fmt.Sprintf("- %s Step %d (%s): %s\n", icon, s.Position, s.Slug, s.Status))
		if s.Error != "" {
			sb.WriteString(fmt.Sprintf("  Error: %s\n", s.Error))
		}
	}
	return sb.String()
}

// executeStepWithRetry calls agentFn once and retries up to step.MaxRetries
// additional times on failure, sleeping step.RetryDelayDuration() between
// attempts. Context cancellation during the sleep aborts early.
func executeStepWithRetry(ctx context.Context, agentFn AgentFunc, opts RunOptions, step WorkflowStep) (string, error) {
	var lastErr error
	maxAttempts := 1 + step.MaxRetries
	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Check context before each attempt.
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		output, err := agentFn(ctx, opts)
		if err == nil {
			return output, nil
		}
		lastErr = err
		// Sleep between retries (not after the final attempt).
		if attempt < maxAttempts-1 {
			delay := step.RetryDelayDuration()
			if delay > 0 {
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(delay):
				}
			}
		}
	}
	return "", lastErr
}
