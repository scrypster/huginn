package scheduler

import (
	"context"
	"testing"
	"time"
)

// TestScheduler_CronTrigger_InvokesRunner verifies that RegisterWorkflow causes
// the cron to fire and invoke the configured WorkflowRunner.
func TestScheduler_CronTrigger_InvokesRunner(t *testing.T) {
	sched := New()
	sched.Start(context.Background())
	t.Cleanup(func() { sched.Stop(context.Background()) })

	fired := make(chan struct{}, 1)
	sched.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		select {
		case fired <- struct{}{}:
		default:
		}
		return nil
	})

	wf := &Workflow{
		ID:       "cron-trigger-test",
		Name:     "Cron Trigger",
		Schedule: "@every 50ms",
		Enabled:  true,
	}
	if err := sched.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow: %v", err)
	}

	select {
	case <-fired:
		// runner was invoked — pass
	case <-time.After(2 * time.Second):
		t.Error("expected workflow runner to be invoked by cron within 2s")
	}
}
