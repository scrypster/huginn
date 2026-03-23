package scheduler

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/notification"
)

// configurableFailNotifStore is a notification store with a configurable Put error.
type configurableFailNotifStore struct {
	mockNotifStore
	putErr error
}

func (f *configurableFailNotifStore) Put(n *notification.Notification) error {
	return f.putErr
}

// TestMakeWorkflowRunner_NotifStore_PutError_StepNotif verifies that a failing
// notifStore.Put on a step notification logs a warning instead of silently discarding.
func TestMakeWorkflowRunner_NotifStore_PutError_StepNotif(t *testing.T) {
	// Capture slog output.
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	store := NewWorkflowRunStore(t.TempDir())
	agentFn := func(ctx context.Context, opts RunOptions) (string, error) {
		return `{"summary": "ok"}` + "\nDone.", nil
	}
	ns := &configurableFailNotifStore{putErr: errors.New("disk full")}

	wfRunner := MakeWorkflowRunner(store, agentFn, ns, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-notif-err",
		Name: "Notif Error WF",
		Steps: []WorkflowStep{
			{
				Name:     "step-one",
				Agent:    "TestAgent",
				Prompt:   "do the thing",
				Position: 0,
				Notify:   &StepNotifyConfig{OnSuccess: true},
			},
		},
		Notification: WorkflowNotificationConfig{OnSuccess: false, OnFailure: false},
	}
	if err := wfRunner(context.Background(), w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logs := buf.String()
	if !strings.Contains(logs, "failed to store step notification") {
		t.Errorf("expected warning log about step notification failure, got:\n%s", logs)
	}
	if !strings.Contains(logs, "disk full") {
		t.Errorf("expected error detail in log, got:\n%s", logs)
	}
}

// TestMakeWorkflowRunner_NotifStore_PutError_WorkflowNotif verifies that a failing
// notifStore.Put on a workflow-level notification logs a warning.
func TestMakeWorkflowRunner_NotifStore_PutError_WorkflowNotif(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	store := NewWorkflowRunStore(t.TempDir())
	agentFn := func(ctx context.Context, opts RunOptions) (string, error) {
		return `{"summary": "ok"}`, nil
	}
	ns := &configurableFailNotifStore{putErr: errors.New("io error")}

	wfRunner := MakeWorkflowRunner(store, agentFn, ns, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-notif-err-wf",
		Name: "WF Level Notif Error",
		Steps: []WorkflowStep{
			{Name: "s1", Agent: "A", Prompt: "x", Position: 0},
		},
		Notification: WorkflowNotificationConfig{OnSuccess: true},
	}
	if err := wfRunner(context.Background(), w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logs := buf.String()
	if !strings.Contains(logs, "failed to store workflow notification") {
		t.Errorf("expected warning log about workflow notification failure, got:\n%s", logs)
	}
}
