package swarm

import (
	"context"
	"fmt"
	"math/rand"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// AgentStatus represents the current state of an agent in the swarm.
type AgentStatus int

const (
	StatusQueued AgentStatus = iota
	StatusThinking
	StatusTooling
	StatusDone
	StatusError
	StatusCancelled
)

// defaultMaxConcurrency is the maximum number of agents that run in parallel
// when NewSwarm is called with maxParallel <= 0.
const defaultMaxConcurrency = 16

// defaultEventBufferSize is the channel buffer size used when SwarmConfig.EventBufferSize is 0.
const defaultEventBufferSize = 512

// EventType categorizes events emitted by agents.
type EventType int

const (
	EventToken EventType = iota
	EventToolStart
	EventToolDone
	EventStatusChange
	EventComplete
	EventError
	EventSwarmReady  // Payload = []SwarmTaskSpec — TUI setup
	EventAgentPanic  // Payload = string (formatted panic + stack); AgentID and TaskID set
)

// SwarmConfig holds optional configuration for a Swarm.
// Zero values use sensible defaults.
type SwarmConfig struct {
	// MaxParallel is the maximum number of agents that run concurrently.
	// 0 or negative uses defaultMaxConcurrency (16).
	MaxParallel int

	// EventBufferSize is the capacity of the event channel buffer.
	// 0 uses defaultEventBufferSize (512).
	EventBufferSize int

	// OutputBufferSize is the per-agent ring buffer capacity in bytes.
	// 0 uses outputRingCap (512 KB). Increase for agents that produce large
	// outputs; decrease to reduce peak memory on resource-constrained systems.
	OutputBufferSize int
}

// SwarmEvent represents an event emitted by an agent.
type SwarmEvent struct {
	AgentID   string
	AgentName string
	Type      EventType
	Payload   any
	At        time.Time
}

// SwarmTaskSpec describes a task for TUI initialization.
type SwarmTaskSpec struct {
	ID    string
	Name  string
	Color string
}

// SwarmTask is a runnable task in the swarm.
type SwarmTask struct {
	ID          string
	Name        string
	Color       string
	Timeout     time.Duration    // per-task timeout; 0 means inherit the Run context
	MaxRetries  int              // number of retries after first failure (0 = no retry)
	RetryDelay  time.Duration    // delay between retries; 0 means no delay
	IsRetryable func(error) bool // if set, only retry when this returns true
	Run         func(ctx context.Context, emit func(SwarmEvent)) error
}

// TaskError wraps a per-task failure with agent identity information.
type TaskError struct {
	AgentID   string
	AgentName string
	Err       error
}

func (te TaskError) Error() string {
	return fmt.Sprintf("agent %q (%s): %v", te.AgentName, te.AgentID, te.Err)
}

func (te TaskError) Unwrap() error { return te.Err }

// panicError wraps a recovered panic value. Panics are recorded per-agent but
// are NOT propagated as the Run fatal error so a single misbehaving agent
// cannot kill the swarm return value.
type panicError struct{ v any }

func (p *panicError) Error() string { return fmt.Sprintf("panic: %v", p.v) }

// SwarmAgent tracks the state of an individual agent.
type SwarmAgent struct {
	ID     string
	Name   string
	Color  string
	Status AgentStatus
	Cancel context.CancelFunc
	Err    error
	output *outputRing // bounded ring buffer; access under mu
	mu     sync.Mutex
}

// Swarm orchestrates parallel execution of tasks with semaphore-based concurrency control.
type Swarm struct {
	maxParallel   int
	outputBufSize int          // per-agent ring buffer capacity; 0 = default (512 KB)
	sem           chan struct{}
	runMu         sync.Mutex   // prevents concurrent Run() calls on the same Swarm
	mu            sync.RWMutex
	agents        map[string]*SwarmAgent
	eventsCh      chan SwarmEvent
	emitMu        sync.Mutex // guards eventsCh sends vs close
	closed        bool       // true after eventsCh is closed
	closeOnce     sync.Once
	droppedEvents atomic.Int64 // counts events dropped due to full buffer
	totalRetries  atomic.Int64 // counts total retries across all tasks
}

// NewSwarm creates a new Swarm with the specified max concurrency.
// If maxParallel is <= 0, defaultMaxConcurrency (16) is used.
// The event buffer size defaults to defaultEventBufferSize (512).
func NewSwarm(maxParallel int) *Swarm {
	return NewSwarmWithConfig(SwarmConfig{MaxParallel: maxParallel})
}

// NewSwarmWithConfig creates a new Swarm using explicit configuration.
// Zero values in SwarmConfig use sensible defaults.
func NewSwarmWithConfig(cfg SwarmConfig) *Swarm {
	maxParallel := cfg.MaxParallel
	if maxParallel <= 0 {
		maxParallel = defaultMaxConcurrency
	}
	bufSize := cfg.EventBufferSize
	if bufSize <= 0 {
		bufSize = defaultEventBufferSize
	}
	return &Swarm{
		maxParallel:   maxParallel,
		outputBufSize: cfg.OutputBufferSize,
		sem:           make(chan struct{}, maxParallel),
		agents:        make(map[string]*SwarmAgent),
		eventsCh:      make(chan SwarmEvent, bufSize),
	}
}

// Events returns the read-only event channel.
func (s *Swarm) Events() <-chan SwarmEvent {
	return s.eventsCh
}

// DroppedEvents returns the number of events silently dropped because the
// event channel buffer was full. Non-zero values indicate consumer lag.
func (s *Swarm) DroppedEvents() int64 {
	return s.droppedEvents.Load()
}

// Run executes all tasks in parallel respecting maxParallel concurrency.
// Returns:
//   - per-task results in original task order
//   - TaskErrors for tasks that failed with a non-context error
//   - total retry count across all tasks
//   - total dropped event count
//   - a fatal error (first non-panic TaskError, wrapping the raw error with agent context)
//
// Panics are recovered and recorded per-agent but are NOT included in the fatal error.
// Context-cancelled tasks are recorded as StatusCancelled and excluded from TaskErrors.
func (s *Swarm) Run(ctx context.Context, tasks []SwarmTask) ([]SwarmResult, []TaskError, int64, int64, error) {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	s.mu.Lock()
	for _, t := range tasks {
		s.agents[t.ID] = &SwarmAgent{
			ID:     t.ID,
			Name:   t.Name,
			Color:  t.Color,
			Status: StatusQueued,
			output: newOutputRingWithSize(s.outputBufSize),
		}
	}
	s.mu.Unlock()

	// Track per-task start times for Duration computation.
	startTimes := make(map[string]time.Time, len(tasks))
	var startMu sync.Mutex

	var wg sync.WaitGroup
	for _, task := range tasks {
		task := task
		wg.Add(1)
		go func() {
			defer wg.Done()
			startMu.Lock()
			startTimes[task.ID] = time.Now()
			startMu.Unlock()
			s.runTask(ctx, task)
		}()
	}
	wg.Wait()
	s.closeOnce.Do(func() {
		s.emitMu.Lock()
		s.closed = true
		close(s.eventsCh)
		s.emitMu.Unlock()
	})

	// Collect per-task results and TaskErrors in original task order.
	// Only StatusError agents are included in TaskErrors; StatusCancelled are excluded.
	results := make([]SwarmResult, len(tasks))
	var taskErrors []TaskError
	s.mu.RLock()
	for i, task := range tasks {
		ag := s.agents[task.ID]
		if ag == nil {
			continue
		}
		ag.mu.Lock()
		status := ag.Status
		agErr := ag.Err
		var output string
		if ag.output != nil {
			output = ag.output.string()
		}
		ag.mu.Unlock()

		startMu.Lock()
		start := startTimes[task.ID]
		startMu.Unlock()

		results[i] = SwarmResult{
			TaskID:    task.ID,
			AgentID:   task.ID,
			AgentName: ag.Name,
			Output:    output,
			Err:       agErr,
			Duration:  time.Since(start),
		}
		if status == StatusError && agErr != nil {
			taskErrors = append(taskErrors, TaskError{
				AgentID:   task.ID,
				AgentName: ag.Name,
				Err:       agErr,
			})
		}
	}
	s.mu.RUnlock()

	// Fatal error = first non-panic task error, exposed as TaskError so the
	// caller can see which agent failed (e.g. error message contains agent ID).
	// Panics are captured per-agent but not surfaced here.
	var fatalErr error
	for _, te := range taskErrors {
		if _, isPanic := te.Err.(*panicError); !isPanic {
			fatalErr = te
			break
		}
	}

	return results, taskErrors, s.totalRetries.Load(), s.droppedEvents.Load(), fatalErr
}

// RunWithProgress executes all tasks and calls progressFn after each task completes.
// progressFn receives the number of completed tasks, total tasks, and the latest result.
func (s *Swarm) RunWithProgress(ctx context.Context, tasks []SwarmTask, progressFn func(completed, total int, latest SwarmResult)) ([]SwarmResult, []TaskError, error) {
	results, taskErrors, _, _, err := s.Run(ctx, tasks)
	if progressFn != nil {
		for i, r := range results {
			progressFn(i+1, len(tasks), r)
		}
	}
	return results, taskErrors, err
}

// runTask executes a single task with semaphore throttling, context handling, and retry logic.
func (s *Swarm) runTask(ctx context.Context, task SwarmTask) {
	select {
	case s.sem <- struct{}{}:
	case <-ctx.Done():
		s.setStatus(task.ID, StatusCancelled)
		s.emit(SwarmEvent{
			AgentID:   task.ID,
			AgentName: task.Name,
			Type:      EventStatusChange,
			Payload:   StatusCancelled,
			At:        time.Now(),
		})
		return
	}
	defer func() { <-s.sem }()

	var agentCtx context.Context
	var cancel context.CancelFunc
	if task.Timeout > 0 {
		agentCtx, cancel = context.WithTimeout(ctx, task.Timeout)
	} else {
		agentCtx, cancel = context.WithCancel(ctx)
	}
	s.mu.Lock()
	if ag, ok := s.agents[task.ID]; ok {
		ag.Cancel = cancel
		ag.Status = StatusThinking
	}
	s.mu.Unlock()
	defer cancel()

	s.emit(SwarmEvent{
		AgentID:   task.ID,
		AgentName: task.Name,
		Type:      EventStatusChange,
		Payload:   StatusThinking,
		At:        time.Now(),
	})

	emit := func(ev SwarmEvent) {
		if ev.AgentID == "" {
			ev.AgentID = task.ID
		}
		if ev.AgentName == "" {
			ev.AgentName = task.Name
		}
		if ev.At.IsZero() {
			ev.At = time.Now()
		}
		// Capture token output for SwarmResult.Output.
		if ev.Type == EventToken {
			if payload, ok := ev.Payload.(string); ok {
				s.mu.Lock()
				if ag, ok2 := s.agents[task.ID]; ok2 && ag.output != nil {
					ag.output.write(payload)
				}
				s.mu.Unlock()
			}
		}
		s.emit(ev)
	}

	maxAttempts := 1 + task.MaxRetries
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			s.totalRetries.Add(1)
			// Wait for retry delay (with jitter) or context cancellation.
			if task.RetryDelay > 0 {
				// Add up to 50% random jitter to spread thundering-herd retries.
				// jitter ∈ [0, RetryDelay/2)
				jitter := time.Duration(rand.Int63n(int64(task.RetryDelay / 2))) //nolint:gosec // not crypto-sensitive
				delay := task.RetryDelay + jitter
				select {
				case <-time.After(delay):
				case <-agentCtx.Done():
				}
			}
			if agentCtx.Err() != nil {
				break
			}
			// Clear previous output for retry.
			s.mu.Lock()
			if ag, ok := s.agents[task.ID]; ok && ag.output != nil {
				ag.output.reset()
			}
			s.mu.Unlock()
		}

		err = func() (runErr error) {
			defer func() {
				if r := recover(); r != nil {
					stack := debug.Stack()
					panicMsg := fmt.Sprintf("panic recovered: %v\n%s", r, stack)
					runErr = &panicError{v: r}
					// Emit a structured panic event so observers (TUI, logging) are
					// notified. Best-effort: drops silently if channel is full.
					s.emit(SwarmEvent{
						AgentID:   task.ID,
						AgentName: task.Name,
						Type:      EventAgentPanic,
						Payload:   panicMsg,
						At:        time.Now(),
					})
				}
			}()
			return task.Run(agentCtx, emit)
		}()

		if err == nil {
			break
		}

		// Stop retrying if context is done.
		if agentCtx.Err() != nil {
			break
		}

		// Check IsRetryable gate before next attempt.
		if attempt < maxAttempts-1 && task.IsRetryable != nil && !task.IsRetryable(err) {
			break
		}
	}

	if err != nil {
		// Distinguish context-cancelled (StatusCancelled) from real errors (StatusError).
		st := StatusError
		if agentCtx.Err() != nil {
			st = StatusCancelled
		}
		s.setStatus(task.ID, st)
		s.mu.Lock()
		if ag, ok := s.agents[task.ID]; ok {
			ag.Err = err
		}
		s.mu.Unlock()
		s.emit(SwarmEvent{
			AgentID:   task.ID,
			AgentName: task.Name,
			Type:      EventStatusChange,
			Payload:   st,
			At:        time.Now(),
		})
		s.emit(SwarmEvent{
			AgentID:   task.ID,
			AgentName: task.Name,
			Type:      EventError,
			Payload:   err,
			At:        time.Now(),
		})
		return
	}

	s.setStatus(task.ID, StatusDone)
	s.emit(SwarmEvent{
		AgentID:   task.ID,
		AgentName: task.Name,
		Type:      EventStatusChange,
		Payload:   StatusDone,
		At:        time.Now(),
	})
	s.emit(SwarmEvent{
		AgentID:   task.ID,
		AgentName: task.Name,
		Type:      EventComplete,
		At:        time.Now(),
	})
}

// emit sends an event to the event channel.
// Uses emitMu to prevent sends on a closed eventsCh.
func (s *Swarm) emit(ev SwarmEvent) {
	s.emitMu.Lock()
	defer s.emitMu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.eventsCh <- ev:
	default: // buffer full, drop event and count it
		s.droppedEvents.Add(1)
	}
}

// setStatus atomically updates the status of an agent.
func (s *Swarm) setStatus(id string, status AgentStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ag, ok := s.agents[id]; ok {
		ag.mu.Lock()
		ag.Status = status
		ag.mu.Unlock()
	}
}

// CancelAll cancels all running agents.
func (s *Swarm) CancelAll() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, ag := range s.agents {
		ag.mu.Lock()
		if ag.Cancel != nil {
			ag.Cancel()
		}
		ag.mu.Unlock()
	}
}
