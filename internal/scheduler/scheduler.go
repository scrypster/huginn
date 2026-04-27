// internal/scheduler/scheduler.go
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/scrypster/huginn/internal/logger"
)

// ErrWorkflowAlreadyRunning is returned by TriggerWorkflow when the workflow
// is currently executing. The HTTP handler maps this to 409 Conflict.
var ErrWorkflowAlreadyRunning = errors.New("workflow already running")

// errUserCancelled is used as the cause when CancelWorkflow is called explicitly
// by a user request. The workflow runner checks context.Cause against this sentinel
// to distinguish user-initiated cancellation from server shutdown or timeout.
var errUserCancelled = errors.New("workflow cancelled by user")

// ErrConcurrencyLimitReached is returned by TriggerWorkflow (manual runs) when
// the global semaphore is full. The HTTP handler maps this to 503 Service
// Unavailable with a Retry-After header.
var ErrConcurrencyLimitReached = errors.New("scheduler: concurrency limit reached; try again later")

// maxConcurrentWorkflows caps the number of workflows that may execute in
// parallel. The scheduler uses a semaphore channel of this size.
const maxConcurrentWorkflows = 10

// Scheduler manages cron-based workflow schedules.
type Scheduler struct {
	cron             *cron.Cron
	mu               sync.Mutex
	workflowRunner   WorkflowRunner                   // nil if not configured
	workflowRunStore WorkflowRunStoreInterface          // optional; used by RunWorkflowSyncWithInputs (Phase 8)
	workflowRunning  map[string]bool                  // workflow IDs currently executing
	workflowEntries  map[string]cron.EntryID          // workflow ID → cron entry ID
	workflowCancels  map[string]context.CancelCauseFunc // workflow ID → cancel-cause func for running goroutine
	sem              chan struct{}                     // global concurrency semaphore
	broadcastFn      WorkflowBroadcastFunc            // may be nil; emits WS events for skipped/lifecycle events
	deliveryQueue    *DeliveryQueue                   // optional; started in Start() if set
	workflowsDir     string                           // optional; enables WorkflowsWatcher when non-empty
}

// New creates a Scheduler.
// Uses SecondOptional parser so both 5-field ("0 8 * * 1-5") and 6-field
// ("*/5 * * * * *") cron expressions are accepted without error.
func New() *Scheduler {
	parser := cron.NewParser(
		cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)
	return &Scheduler{
		cron:            cron.New(cron.WithParser(parser)),
		workflowRunning: make(map[string]bool),
		workflowEntries: make(map[string]cron.EntryID),
		workflowCancels: make(map[string]context.CancelCauseFunc),
		sem:             make(chan struct{}, maxConcurrentWorkflows),
	}
}

// cronParser is the shared parser for cron expressions. Mirrors the parser
// used by New() so ValidateCronSchedule accepts the same syntax.
var cronParser = cron.NewParser(
	cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// defaultWorkflowTimeout is the cap applied to all workflow runs when
// Workflow.TimeoutMinutes is 0 (not configured).
const defaultWorkflowTimeout = 30 * time.Minute

// maxWorkflowTimeoutMinutes is the server-enforced ceiling for TimeoutMinutes.
// Submitted values above this are clamped rather than rejected so that older
// clients that don't know about the field still work correctly.
const maxWorkflowTimeoutMinutes = 1440 // 24 hours

// ValidateWorkflowTimeout clamps timeout_minutes to [0, maxWorkflowTimeoutMinutes].
// 0 is preserved as "use default" and is not an error. Safe to call from
// HTTP handlers in other packages before persisting a workflow.
func ValidateWorkflowTimeout(minutes int) int {
	if minutes < 0 {
		return 0
	}
	if minutes > maxWorkflowTimeoutMinutes {
		return maxWorkflowTimeoutMinutes
	}
	return minutes
}

// workflowTimeout returns the effective run timeout for w.
// Falls back to defaultWorkflowTimeout when TimeoutMinutes is 0.
func workflowTimeout(w *Workflow) time.Duration {
	if w.TimeoutMinutes > 0 {
		return time.Duration(w.TimeoutMinutes) * time.Minute
	}
	return defaultWorkflowTimeout
}

// ValidateCronSchedule returns a non-nil error when schedule cannot be parsed
// by the Huginn cron parser. Use this in input validation before persisting a
// workflow so users get a clear error at save time rather than at run time.
func ValidateCronSchedule(schedule string) error {
	if schedule == "" {
		return nil // empty schedule = disabled workflow; not an error
	}
	if _, err := cronParser.Parse(schedule); err != nil {
		return fmt.Errorf("invalid cron schedule %q: %w", schedule, err)
	}
	return nil
}

// Start begins the cron loop. Non-blocking.
func (s *Scheduler) Start(ctx context.Context) {
	s.cron.Start()
	s.mu.Lock()
	q := s.deliveryQueue
	dir := s.workflowsDir
	s.mu.Unlock()
	if q != nil {
		q.StartWorker(ctx)
	}
	if dir != "" {
		watcher := NewWorkflowsWatcher(dir, s, nil)
		go watcher.Start(ctx)
	}
}

// Stop halts the cron loop, waiting for running jobs to finish.
func (s *Scheduler) Stop(ctx context.Context) {
	stopCtx := s.cron.Stop()
	select {
	case <-stopCtx.Done():
	case <-ctx.Done():
	}
}

// SetWorkflowRunner configures the runner used when workflows fire.
func (s *Scheduler) SetWorkflowRunner(wr WorkflowRunner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workflowRunner = wr
}

// SetWorkflowRunStore wires the run store used by RunWorkflowSyncWithInputs
// to identify the new run a sub-workflow appended. It is OK to leave this
// nil — sub-workflow steps will then return an empty output and the parent
// continues. The runner's own persistence path uses its own injected store
// regardless.
func (s *Scheduler) SetWorkflowRunStore(store WorkflowRunStoreInterface) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workflowRunStore = store
}

// SetBroadcastFunc wires the WS broadcast function used to emit workflow
// lifecycle events (e.g. workflow_skipped when the semaphore is full).
// Nil is valid and disables broadcasting.
func (s *Scheduler) SetBroadcastFunc(fn WorkflowBroadcastFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.broadcastFn = fn
}

// SetDeliveryQueue wires the durable delivery queue and starts its background
// worker when Start() is called.
func (s *Scheduler) SetDeliveryQueue(q *DeliveryQueue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deliveryQueue = q
}

// SetWorkflowsDir configures the directory the WorkflowsWatcher polls for
// workflow YAML file changes. Must be called before Start(). Empty string
// disables the watcher.
func (s *Scheduler) SetWorkflowsDir(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workflowsDir = dir
}

// RegisterWorkflow adds or replaces a workflow's cron schedule.
// Disabled workflows are skipped. Existing entries for the same ID are removed first.
func (s *Scheduler) RegisterWorkflow(w *Workflow) error {
	if !w.Enabled {
		return nil
	}
	if w.Schedule == "" {
		return fmt.Errorf("scheduler: workflow %q has empty schedule", w.ID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.workflowRunner == nil {
		return fmt.Errorf("scheduler: workflow runner not configured")
	}
	if id, ok := s.workflowEntries[w.ID]; ok {
		s.cron.Remove(id)
		delete(s.workflowEntries, w.ID)
	}
	ww := w
	wr := s.workflowRunner
	sem := s.sem
	entryID, err := s.cron.AddFunc(w.Schedule, func() {
		s.mu.Lock()
		if s.workflowRunning[ww.ID] {
			s.mu.Unlock()
			return
		}
		s.workflowRunning[ww.ID] = true
		broadcast := s.broadcastFn
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			delete(s.workflowRunning, ww.ID)
			delete(s.workflowCancels, ww.ID)
			s.mu.Unlock()
		}()
		// Acquire global concurrency semaphore (non-blocking: skip if full).
		// For scheduled (cron-triggered) runs, log a warning and emit a
		// workflow_skipped WS event so operators can observe the drop.
		select {
		case sem <- struct{}{}:
		default:
			logger.Warn("scheduler: concurrency limit reached, skipping scheduled workflow run",
				"workflow_id", ww.ID)
			if broadcast != nil {
				broadcast("workflow_skipped", map[string]any{
					"workflow_id": ww.ID,
					"reason":      "concurrency limit",
				})
			}
			return
		}
		defer func() { <-sem }()
		// Two-layer context: outer carries the cancel cause; inner adds timeout.
		// CancelWorkflow stores causeCancel so it can tag cancellations with
		// errUserCancelled. The runner checks context.Cause to distinguish
		// user-cancel from timeout/shutdown.
		baseCtx, causeCancel := context.WithCancelCause(context.Background())
		ctx, timeoutCancel := context.WithTimeout(baseCtx, workflowTimeout(ww))
		defer timeoutCancel()
		defer causeCancel(nil) // satisfy resource-release requirement
		// Store causeCancel so CancelWorkflow can interrupt this run.
		// Deferred cleanup above removes it when the goroutine exits.
		s.mu.Lock()
		s.workflowCancels[ww.ID] = causeCancel
		s.mu.Unlock()
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("scheduler: panic in workflow runner",
						"workflow_id", ww.ID, "panic", r)
				}
			}()
			if err := wr(ctx, ww); err != nil {
				logger.Error("scheduler: workflow run failed",
					"workflow_id", ww.ID, "err", err)
			}
		}()
	})
	if err != nil {
		return fmt.Errorf("scheduler: register workflow %q cron %q: %w", w.ID, w.Schedule, err)
	}
	s.workflowEntries[w.ID] = entryID
	return nil
}

// RemoveWorkflow deregisters a workflow by ID. No-op if not registered.
func (s *Scheduler) RemoveWorkflow(workflowID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.workflowEntries[workflowID]; ok {
		s.cron.Remove(id)
		delete(s.workflowEntries, workflowID)
	}
}

// LoadWorkflows reads all workflows from dir and registers enabled ones.
func (s *Scheduler) LoadWorkflows(dir string) error {
	workflows, err := LoadWorkflows(dir)
	if err != nil {
		return err
	}
	for _, w := range workflows {
		if err := s.RegisterWorkflow(w); err != nil {
			logger.Warn("scheduler: skipping workflow with invalid schedule", "workflow_id", w.ID, "err", err)
			continue // skip bad schedules, don't fail all
		}
	}
	return nil
}

// TriggerWorkflow fires a workflow immediately (bypass cron) in a background goroutine.
// Returns synchronously with one of:
//   - nil: workflow has been launched in the background
//   - ErrWorkflowAlreadyRunning: workflow is currently executing
//   - ErrConcurrencyLimitReached: global semaphore is full; caller should respond with 503
//   - error: runner not configured or other early-validation failure
//
// The runner's result is logged but not surfaced to the caller because the
// HTTP handler responds immediately (fire-and-forget).
// TriggerWorkflowWithInputs is the inputs-aware variant of TriggerWorkflow.
// inputs is forwarded to the runner via context and used to seed the run's
// scratchpad so the very first step can reference {{run.scratch.KEY}}.
//
// Manual UI triggers and webhook deliveries call this; the scheduler's own
// cron path keeps using TriggerWorkflow (no inputs).
func (s *Scheduler) TriggerWorkflowWithInputs(ctx context.Context, w *Workflow, inputs map[string]string) error {
	if len(inputs) > 0 {
		ctx = WithInitialInputs(ctx, inputs)
	}
	return s.TriggerWorkflow(ctx, w)
}

// RunWorkflowSyncWithInputs executes a workflow inline (blocking) using the
// configured runner and returns the persisted WorkflowRun so the caller can
// inspect step outputs. Phase 8 sub-workflow steps use this so the parent
// runner can adopt the child's last-step output as its own.
//
// Unlike TriggerWorkflow this path:
//   - does NOT acquire the global concurrency semaphore (the caller already
//     holds one slot — counting the child would make a sub-workflow chain
//     trivially deadlock-prone);
//   - does NOT register a cancel func keyed by workflow id (the parent's
//     context already governs lifetime);
//   - does NOT enforce the per-id "already running" gate (a parent workflow
//     that calls itself recursively is the caller's problem to bound).
//
// The returned WorkflowRun is the same value the runner persisted via the
// run store, so it is safe to read fields off of it.
func (s *Scheduler) RunWorkflowSyncWithInputs(ctx context.Context, w *Workflow, inputs map[string]string) (*WorkflowRun, error) {
	s.mu.Lock()
	wr := s.workflowRunner
	store := s.workflowRunStore
	s.mu.Unlock()
	if wr == nil {
		return nil, fmt.Errorf("scheduler: workflow runner not configured")
	}
	runCtx := ctx
	if len(inputs) > 0 {
		runCtx = WithInitialInputs(runCtx, inputs)
	}
	// Capture the timestamp BEFORE invoking the runner so we can pick the
	// just-persisted run out of the store. The runner stamps StartedAt with
	// time.Now().UTC(), so any run with StartedAt >= startedAtFloor that
	// matches w.ID is ours. This is robust to other workflows running in
	// parallel because we filter by workflow ID via store.List.
	startedAtFloor := time.Now().UTC().Add(-time.Second)
	if err := wr(runCtx, w); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, nil
	}
	// List returns runs newest-first; pull a small window so we can pick
	// the freshest entry that was created during this call. 5 is plenty
	// for the common case while keeping the query cheap.
	recent, err := store.List(w.ID, 5)
	if err != nil {
		return nil, err
	}
	for _, r := range recent {
		if r == nil {
			continue
		}
		if !r.StartedAt.Before(startedAtFloor) {
			return r, nil
		}
	}
	return nil, nil
}

func (s *Scheduler) TriggerWorkflow(ctx context.Context, w *Workflow) error {
	// Snapshot the inputs from the caller's context BEFORE we hand off to the
	// goroutine — TriggerWorkflowWithInputs uses ctx as the carrier, but the
	// goroutine builds its own context.Background() under the hood, so the
	// inputs would otherwise be lost.
	triggerInputs := initialInputs(ctx)
	s.mu.Lock()
	wr := s.workflowRunner
	if wr == nil {
		s.mu.Unlock()
		return fmt.Errorf("scheduler: workflow runner not configured")
	}
	if s.workflowRunning[w.ID] {
		s.mu.Unlock()
		return fmt.Errorf("%w: %q", ErrWorkflowAlreadyRunning, w.ID)
	}
	sem := s.sem
	s.mu.Unlock()

	// Try to acquire the global concurrency semaphore synchronously so the
	// HTTP handler can return 503 immediately instead of silently dropping the run.
	select {
	case sem <- struct{}{}:
	default:
		logger.Warn("scheduler: concurrency limit reached, rejecting manual workflow trigger",
			"workflow_id", w.ID)
		return ErrConcurrencyLimitReached
	}

	// Mark the workflow as running only after acquiring the semaphore.
	s.mu.Lock()
	s.workflowRunning[w.ID] = true
	s.mu.Unlock()

	// Launch asynchronously so the HTTP handler can return immediately.
	// We must not use the HTTP request context here because it is cancelled
	// as soon as the handler responds — this is a fire-and-forget goroutine.
	go func() {
		defer func() { <-sem }()
		defer func() {
			s.mu.Lock()
			delete(s.workflowRunning, w.ID)
			delete(s.workflowCancels, w.ID)
			s.mu.Unlock()
		}()
		// Two-layer context: outer carries the cancel cause; inner adds timeout.
		baseCtx, causeCancel := context.WithCancelCause(context.Background())
		// Forward trigger-supplied inputs onto the goroutine's fresh context.
		baseCtx = WithInitialInputs(baseCtx, triggerInputs)
		runCtx, timeoutCancel := context.WithTimeout(baseCtx, workflowTimeout(w))
		defer timeoutCancel()
		defer causeCancel(nil) // satisfy resource-release requirement
		// Store causeCancel so CancelWorkflow can tag the cause as errUserCancelled.
		s.mu.Lock()
		s.workflowCancels[w.ID] = causeCancel
		s.mu.Unlock()
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("scheduler: panic in triggered workflow runner",
						"workflow_id", w.ID, "panic", r)
				}
			}()
			if err := wr(runCtx, w); err != nil {
				logger.Error("scheduler: triggered workflow run failed",
					"workflow_id", w.ID, "err", err)
			}
		}()
	}()
	return nil
}

// CancelWorkflow interrupts a running workflow by cancelling its context.
// Returns true if the workflow was running and its cancel was called,
// false if the workflow is not currently executing.
// The deferred cleanup inside the goroutine removes the cancel func and marks
// the workflow as not running — callers do not need to do this themselves.
func (s *Scheduler) CancelWorkflow(id string) bool {
	s.mu.Lock()
	cancel, ok := s.workflowCancels[id]
	s.mu.Unlock()
	if !ok {
		return false
	}
	cancel(errUserCancelled)
	return true
}
