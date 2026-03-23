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
	// Connections maps provider name → connection ID for pre-authorised credentials.
	// Injected into the agent's tool context at run time.
	Connections map[string]string
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
) WorkflowRunner {
	return func(ctx context.Context, w *Workflow) error {
		// Sort steps by Position ascending.
		steps := make([]WorkflowStep, len(w.Steps))
		copy(steps, w.Steps)
		sort.Slice(steps, func(i, j int) bool { return steps[i].Position < steps[j].Position })

		stepOutputs := map[string]string{}
		var prevOutput string
		var anyStepFailed bool

		run := &WorkflowRun{
			ID:         fmt.Sprintf("wf-%s-%d", w.ID, time.Now().UnixMilli()),
			WorkflowID: w.ID,
			Status:     WorkflowRunStatusRunning,
			StartedAt:  time.Now().UTC(),
		}

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

			if step.Prompt != "" {
				// Inline step path.
				resolvedPrompt := resolveInlineVars(step.Prompt, step.Vars)
				resolvedPrompt = resolveRuntimeVars(resolvedPrompt, step.Inputs, stepOutputs, prevOutput)
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
				opts := RunOptions{
					RunID:       runID,
					AgentName:   step.Agent,
					Prompt:      fullPrompt,
					Connections: step.Connections,
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
			output, agentErr := executeStepWithRetry(stepCtx, agentFn, opts, step)
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
					Position:  step.Position,
					Slug:      stepName,
					SessionID: sessionID,
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
						n := &notification.Notification{
							ID:           notification.NewID(),
							RunID:        runID,
							WorkflowID:   w.ID,
							Summary:      fmt.Sprintf("[%s] Step %d: %s", w.Name, step.Position, stepName),
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
							n.Detail = output
						}
						deliveries := dispatchNotification(n, step.Notify.DeliverTo, notifStore, spaceDeliveryFn, huginnDir, deliverers, onDeliveryFailure)
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
			deliveries := dispatchNotification(n, w.Notification.DeliverTo, notifStore, spaceDeliveryFn, huginnDir, deliverers, onDeliveryFailure)
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
// then named inputs ({{inputs.alias}}), then {{prev.output}}.
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
func resolveRuntimeVars(prompt string, inputs []StepInput, stepOutputs map[string]string, prevOutput string) string {
	result := prompt
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
	return result
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
// to any configured targets (space, webhook, email). Returns the delivery records.
// huginnDir is used for dead-letter JSONL on external delivery failure; pass "" to skip.
// deliverers may be nil, in which case webhook/email targets are logged and skipped.
func dispatchNotification(
	n *notification.Notification,
	targets []NotificationDelivery,
	notifStore notification.StoreInterface,
	spaceDeliveryFn SpaceDeliveryFunc,
	huginnDir string,
	deliverers *DelivererRegistry,
	onDeliveryFailure DeliveryFailureFunc,
) []notification.DeliveryRecord {
	// Inbox is always implicit.
	records := []notification.DeliveryRecord{
		{Type: "inbox", Target: "inbox", Status: "sent", SentAt: time.Now().UTC()},
	}
	for _, t := range targets {
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
