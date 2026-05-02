package scheduler

import (
	"testing"
)

func TestCloneWorkflow_ScrubbedSMTPPass(t *testing.T) {
	wf := &Workflow{
		ID: "wf-scrub",
		Steps: []WorkflowStep{
			{
				Name: "step1",
				Notify: &StepNotifyConfig{
					DeliverTo: []NotificationDelivery{
						{Type: "email", SMTPPass: "super-secret"},
					},
				},
			},
		},
	}

	cloned := cloneWorkflow(wf)
	if cloned == nil {
		t.Fatal("expected non-nil clone")
	}
	if cloned.Steps[0].Notify.DeliverTo[0].SMTPPass != "" {
		t.Errorf("expected SMTPPass to be scrubbed in snapshot, got %q",
			cloned.Steps[0].Notify.DeliverTo[0].SMTPPass)
	}
	// Original must be untouched.
	if wf.Steps[0].Notify.DeliverTo[0].SMTPPass != "super-secret" {
		t.Errorf("original SMTPPass should be unchanged, got %q",
			wf.Steps[0].Notify.DeliverTo[0].SMTPPass)
	}
}

func TestCloneWorkflow_ScrubbedSMTPPass_WorkflowLevel(t *testing.T) {
	wf := &Workflow{
		ID: "wf-scrub-top",
		Notification: WorkflowNotificationConfig{
			DeliverTo: []NotificationDelivery{
				{Type: "email", SMTPPass: "top-secret"},
			},
		},
	}

	cloned := cloneWorkflow(wf)
	if cloned == nil {
		t.Fatal("expected non-nil clone")
	}
	if cloned.Notification.DeliverTo[0].SMTPPass != "" {
		t.Errorf("expected workflow-level SMTPPass to be scrubbed, got %q",
			cloned.Notification.DeliverTo[0].SMTPPass)
	}
	// Original must be untouched.
	if wf.Notification.DeliverTo[0].SMTPPass != "top-secret" {
		t.Errorf("original workflow-level SMTPPass should be unchanged, got %q",
			wf.Notification.DeliverTo[0].SMTPPass)
	}
}
