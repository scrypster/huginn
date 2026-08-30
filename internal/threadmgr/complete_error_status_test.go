package threadmgr

import "testing"

func TestComplete_ErrorSummarySetsStatusError(t *testing.T) {
	tm := New()
	th, err := tm.Create(CreateParams{SessionID: "sess-lab", AgentID: "Sam", Task: "2+2"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	tm.Complete(th.ID, FinishSummary{Summary: "Sam couldn't get a key for this.", Status: "error"})
	got, ok := tm.Get(th.ID)
	if !ok {
		t.Fatal("missing thread")
	}
	if got.Status != StatusError {
		t.Fatalf("status %q, want error", got.Status)
	}
	if got.Summary == nil || got.Summary.Summary != "Sam couldn't get a key for this." {
		t.Fatalf("summary %+v", got.Summary)
	}
}

func TestComplete_OKSummaryStaysDone(t *testing.T) {
	tm := New()
	th, err := tm.Create(CreateParams{SessionID: "sess-lab", AgentID: "Sam", Task: "2+2"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	tm.Complete(th.ID, FinishSummary{Summary: "4", Status: "ok"})
	got, ok := tm.Get(th.ID)
	if !ok {
		t.Fatal("missing thread")
	}
	if got.Status != StatusDone {
		t.Fatalf("status %q, want done", got.Status)
	}
}
