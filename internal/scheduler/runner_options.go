// Package scheduler — variadic options for MakeWorkflowRunner (Phase 3+).
//
// MakeWorkflowRunner accumulated nine positional parameters before this file
// existed, which makes new optional capabilities (agent DM delivery, future
// audit hooks, OTel exporters) painful to add without breaking every caller.
// Going forward, NEW configuration MUST land as a RunnerOption rather than a
// new positional argument so the existing call sites stay stable.
package scheduler

import "context"

// runnerConfig collects optional runner dependencies that don't have natural
// defaults from the existing positional parameters. Defaults are intentionally
// nil — every consumer is treated as optional unless explicitly wired.
type runnerConfig struct {
	agentDM       AgentDMDeliveryFunc
	chainTrigger  ChainTriggerFunc
	subWorkflow   SubWorkflowFunc
	metrics       MetricsCollector
	deliveryQueue *DeliveryQueue
}

// MetricsCollector is the minimal contract the runner needs to emit
// observability metrics. It mirrors stats.Collector so callers can pass a
// stats.Registry-backed collector directly. Pass nil (the default) to skip
// metric emission entirely; the runner still emits structured logs.
type MetricsCollector interface {
	Record(metric string, value float64, tags ...string)
	Histogram(metric string, value float64, tags ...string)
}

// SubWorkflowFunc resolves a sub-workflow id to an output string. Phase 8
// uses it to support `step.sub_workflow: <id>`: the runner looks up the
// child workflow, runs it (with the parent's scratch as initial inputs),
// and uses the last-step output as this step's output. The runner is
// disk-agnostic — main.go owns workflow lookup and chooses how to invoke
// the child. Errors abort the parent step (treated like an agent error).
type SubWorkflowFunc func(ctx context.Context, id string, inputs map[string]string) (output string, err error)

// ChainTriggerFunc is invoked after a workflow run reaches a terminal status.
// The runner passes the parent workflow definition and its completed run; the
// implementation decides (based on parent.Chain + run.Status) whether to
// trigger a downstream workflow and how to seed its scratchpad. The runner
// itself stays disk-agnostic; main.go owns workflow lookup and triggering.
//
// Errors are logged by the implementation and never propagated — chain
// triggering must NEVER cause the upstream run to be reported as failed.
type ChainTriggerFunc func(parent *Workflow, run *WorkflowRun)

// defaultRunnerConfig returns the zero-value config used when no options are
// supplied. Kept as a function so future defaults stay grouped here rather
// than scattered as inline literals at every call site.
func defaultRunnerConfig() runnerConfig {
	return runnerConfig{}
}

// RunnerOption configures optional runner capabilities (agent DM delivery,
// future audit hooks, etc) without breaking the positional MakeWorkflowRunner
// signature. Apply via MakeWorkflowRunner(..., WithAgentDMDelivery(fn)).
type RunnerOption func(*runnerConfig)

// WithAgentDMDelivery wires the per-agent DM delivery callback. The runner
// invokes it for every NotificationDelivery whose Type is "agent_dm". The
// callback resolves the DM space between the agent and the configured user,
// persists the message with agent authorship, and broadcasts WS events.
// Pass nil to explicitly disable agent_dm delivery (the default).
func WithAgentDMDelivery(fn AgentDMDeliveryFunc) RunnerOption {
	return func(c *runnerConfig) {
		c.agentDM = fn
	}
}

// WithChainTrigger wires the workflow-chaining hook. The runner invokes the
// callback once per run, AFTER the terminal WS event has been broadcast and
// the run has been persisted, so a chained workflow that watches the same WS
// stream sees the parent transition first. Pass nil to disable chaining.
func WithChainTrigger(fn ChainTriggerFunc) RunnerOption {
	return func(c *runnerConfig) {
		c.chainTrigger = fn
	}
}

// WithMetricsCollector wires a stats collector for runner-level observability.
// The runner emits:
//
//   - counter `huginn.workflow.run.total` with status tag (started/complete/
//     failed/partial/cancelled) — one increment per terminal transition.
//   - counter `huginn.workflow.step.total` with status tag (success/failed/
//     skipped) — one increment per step result.
//   - histogram `huginn.workflow.run.latency_ms` — wall time for the entire
//     run, populated only on terminal transitions.
//   - histogram `huginn.workflow.step.latency_ms` — wall time for each step
//     that ran (skipped steps are excluded so percentiles remain meaningful).
//
// Pass nil to disable metric emission. Structured logs still fire either way.
func WithMetricsCollector(c MetricsCollector) RunnerOption {
	return func(cfg *runnerConfig) {
		cfg.metrics = c
	}
}

// WithSubWorkflow wires the sub-workflow resolver used by `step.sub_workflow`
// (Phase 8). The runner calls fn synchronously: when fn returns, the parent
// step continues with the child's output. fn is expected to handle workflow
// loading, triggering, and waiting; the runner has no opinion on whether the
// child blocks the goroutine or runs in a worker pool. Pass nil to leave
// sub-workflow support disabled — the parent step then errors with
// "sub_workflow support not configured".
func WithSubWorkflow(fn SubWorkflowFunc) RunnerOption {
	return func(c *runnerConfig) {
		c.subWorkflow = fn
	}
}

// WithDeliveryQueue wires the durable delivery queue. When set, failed
// webhook and email deliveries are enqueued for automatic retry instead of
// being written to the JSONL dead-letter file. Pass nil to disable (default).
func WithDeliveryQueue(q *DeliveryQueue) RunnerOption {
	return func(c *runnerConfig) {
		c.deliveryQueue = q
	}
}
