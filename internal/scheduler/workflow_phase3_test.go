package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/notification"
)

// ─────────────────────────────────────────────────────────────────────────────
// 4-state notify mode helpers
// ─────────────────────────────────────────────────────────────────────────────

func TestStepNotifyConfig_Mode_AllStates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		on, off bool
		want    NotifyMode
	}{
		{false, false, NotifyModeNone},
		{true, false, NotifyModeOnSuccess},
		{false, true, NotifyModeOnFailure},
		{true, true, NotifyModeAlways},
	}
	for _, c := range cases {
		got := (&StepNotifyConfig{OnSuccess: c.on, OnFailure: c.off}).Mode()
		if got != c.want {
			t.Errorf("OnSuccess=%v OnFailure=%v → %s, want %s", c.on, c.off, got, c.want)
		}
	}
	// nil receiver collapses to None — used by UI rendering.
	var nilCfg *StepNotifyConfig
	if got := nilCfg.Mode(); got != NotifyModeNone {
		t.Errorf("nil receiver → %s, want %s", got, NotifyModeNone)
	}
}

func TestStepNotifyConfig_SetMode_RoundTrip(t *testing.T) {
	t.Parallel()
	for _, mode := range []NotifyMode{NotifyModeNone, NotifyModeOnSuccess, NotifyModeOnFailure, NotifyModeAlways} {
		cfg := &StepNotifyConfig{}
		cfg.SetMode(mode)
		if got := cfg.Mode(); got != mode {
			t.Errorf("round-trip: SetMode(%s) → Mode() = %s", mode, got)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// agent_dm delivery
// ─────────────────────────────────────────────────────────────────────────────

// TestDispatchNotification_AgentDM verifies the runner invokes the configured
// AgentDMDeliveryFunc with the step's agent as From, the configured user, and
// the rendered summary/detail.
func TestDispatchNotification_AgentDM(t *testing.T) {
	t.Parallel()
	var got struct {
		agent, user, summary, detail string
		called                       int
	}
	dm := AgentDMDeliveryFunc(func(agent, user, summary, detail string) error {
		got.agent, got.user, got.summary, got.detail = agent, user, summary, detail
		got.called++
		return nil
	})

	n := &notification.Notification{ID: "n1", Summary: "All clear", Detail: "12 PRs scanned"}
	targets := []NotificationDelivery{{Type: "agent_dm", User: "matt"}}

	recs := dispatchNotification(n, targets, nil, nil, dm, "Sentinel", "", nil, nil, nil, "")

	if got.called != 1 {
		t.Fatalf("agent DM fn called %d times, want 1", got.called)
	}
	if got.agent != "Sentinel" || got.user != "matt" || got.summary != "All clear" || got.detail != "12 PRs scanned" {
		t.Fatalf("unexpected DM payload: %+v", got)
	}
	if len(recs) != 2 || recs[1].Type != "agent_dm" || recs[1].Status != "sent" {
		t.Fatalf("expected agent_dm record sent, got %+v", recs)
	}
}

// TestDispatchNotification_AgentDM_FromOverridesStepAgent verifies that an
// explicit `from:` on the delivery target overrides the step's agent so a
// workflow author can route on behalf of a different persona.
func TestDispatchNotification_AgentDM_FromOverridesStepAgent(t *testing.T) {
	t.Parallel()
	var seen string
	dm := AgentDMDeliveryFunc(func(agent, user, summary, detail string) error {
		seen = agent
		return nil
	})
	targets := []NotificationDelivery{{Type: "agent_dm", User: "matt", From: "Auditor"}}
	dispatchNotification(&notification.Notification{}, targets, nil, nil, dm, "Sentinel", "", nil, nil, nil, "")
	if seen != "Auditor" {
		t.Fatalf("From override ignored: agent=%q want Auditor", seen)
	}
}

// TestDispatchNotification_AgentDM_NoBindingSkips verifies that when the
// runner is built without WithAgentDMDelivery the agent_dm target is skipped
// gracefully (no panic, status-success on the only record which is inbox).
func TestDispatchNotification_AgentDM_NoBindingSkips(t *testing.T) {
	t.Parallel()
	targets := []NotificationDelivery{{Type: "agent_dm", User: "matt"}}
	recs := dispatchNotification(&notification.Notification{}, targets, nil, nil, nil, "Sentinel", "", nil, nil, nil, "")
	if len(recs) != 1 || recs[0].Type != "inbox" {
		t.Fatalf("expected only inbox record without DM binding, got %+v", recs)
	}
}

// TestDispatchNotification_AgentDM_DeliveryError records the error and does
// not crash the run.
func TestDispatchNotification_AgentDM_DeliveryError(t *testing.T) {
	t.Parallel()
	dm := AgentDMDeliveryFunc(func(agent, user, summary, detail string) error {
		return errors.New("boom")
	})
	targets := []NotificationDelivery{{Type: "agent_dm", User: "matt"}}
	recs := dispatchNotification(&notification.Notification{}, targets, nil, nil, dm, "Sentinel", "", nil, nil, nil, "")
	if len(recs) != 2 || recs[1].Status != "failed" || recs[1].Error == "" {
		t.Fatalf("expected failed record with error, got %+v", recs[1])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// End-to-end: runner option wiring
// ─────────────────────────────────────────────────────────────────────────────

// TestMakeWorkflowRunner_WithAgentDMDelivery_E2E verifies the variadic option
// is honoured end-to-end: a step with an agent_dm delivery in its Notify
// config triggers the binding wired via WithAgentDMDelivery.
func TestMakeWorkflowRunner_WithAgentDMDelivery_E2E(t *testing.T) {
	t.Parallel()
	store := &mockRunStore{}

	var (
		mu       sync.Mutex
		dmCalled int
		gotAgent string
	)
	dm := AgentDMDeliveryFunc(func(agent, user, summary, detail string) error {
		mu.Lock()
		defer mu.Unlock()
		dmCalled++
		gotAgent = agent
		return nil
	})

	agentFn := func(_ context.Context, _ RunOptions) (string, error) {
		return `{"summary":"all done"}`, nil
	}

	wf := &Workflow{
		ID: "wf-dm", Name: "dm-test",
		Steps: []WorkflowStep{
			{
				Position: 1, Name: "checker", Agent: "Sentinel", Prompt: "do it",
				Notify: &StepNotifyConfig{
					OnSuccess: true,
					DeliverTo: []NotificationDelivery{{Type: "agent_dm", User: "matt"}},
				},
			},
		},
	}

	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil, WithAgentDMDelivery(dm))
	if err := runner(context.Background(), wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if dmCalled != 1 {
		t.Fatalf("DM fn called %d times, want 1", dmCalled)
	}
	if gotAgent != "Sentinel" {
		t.Fatalf("DM author = %q, want Sentinel", gotAgent)
	}
}
