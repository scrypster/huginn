package threadmgr

import (
	"sync"
	"testing"
	"time"
)

// The specialist evictor fires on EVERY terminal status, not just StatusDone
// — including StatusCancelled, which is what the spawn-approval DENY path
// produces (main.go cancels the thread the instant a human refuses the
// preview). Eviction itself must happen in all those cases (the ephemeral
// overlay entry has to go), but the S13 finish line — "<Name> is done and
// gone." — is only true for work that actually completed. Broadcasting it on
// a cancel would tell the user a specialist they just declined ran and
// finished.
//
// This test pins the precondition the evictor's caller must handle: the hook
// is invoked with a thread whose Status is NOT StatusDone, so the caller is
// responsible for checking Status before speaking.
func TestSpecialistEvictor_FiresWithNonDoneStatusOnCancel(t *testing.T) {
	tm := New()
	tm.SetMembershipChecker(&stubChecker{members: []string{"Winston"}})
	tm.SetCompanyGate(&stubCompanyGate{companyID: "co-a", seated: []string{"Winston"}})
	tm.SetSpecialistCompany("Rust Audit Specialist", "co-a")

	var mu sync.Mutex
	var seenStatuses []ThreadStatus
	tm.SetSpecialistEvictor(func(name, threadID string) {
		th, ok := tm.Get(threadID)
		mu.Lock()
		if ok {
			seenStatuses = append(seenStatuses, th.Status)
		}
		mu.Unlock()
	})

	th, err := tm.Create(CreateParams{
		SessionID: "sess-deny",
		AgentID:   "Rust Audit Specialist",
		Task:      "audit",
		SpaceID:   "space-a",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tm.RegisterSpecialistThread(th.ID, "Rust Audit Specialist")

	// This is exactly what the spawn-preview DENY path does.
	tm.Cancel(th.ID)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(seenStatuses)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seenStatuses) != 1 {
		t.Fatalf("expected the evictor to fire once on cancel, got %d invocations", len(seenStatuses))
	}
	if seenStatuses[0] == StatusDone {
		t.Fatalf("cancelled specialist thread must not present as StatusDone — "+
			"the evictor would then speak a false finish line; got %v", seenStatuses[0])
	}
	if seenStatuses[0] != StatusCancelled {
		t.Errorf("expected StatusCancelled at eviction time, got %v", seenStatuses[0])
	}
}
