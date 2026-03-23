package threadmgr

import (
	"errors"
	"testing"
)

// stubChecker implements SpaceMembershipChecker for tests.
type stubChecker struct {
	members []string
	err     error
}

func (s *stubChecker) SpaceMembers(_ string) ([]string, error) {
	return s.members, s.err
}

func TestCreate_SpaceIDGuard_MemberAllowed(t *testing.T) {
	tm := New()
	tm.SetMembershipChecker(&stubChecker{members: []string{"alice", "bob"}})

	_, err := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "alice",
		Task:      "test task",
		SpaceID:   "space-abc",
	})
	if err != nil {
		t.Fatalf("expected alice (a member) to be allowed, got: %v", err)
	}
}

func TestCreate_SpaceIDGuard_NonMemberDenied(t *testing.T) {
	tm := New()
	tm.SetMembershipChecker(&stubChecker{members: []string{"alice"}})

	_, err := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "eve",
		Task:      "test task",
		SpaceID:   "space-abc",
	})
	if err == nil {
		t.Fatal("expected eve (not a member) to be denied")
	}
	if !errors.Is(err, ErrAgentNotSpaceMember) {
		t.Errorf("expected ErrAgentNotSpaceMember, got: %v", err)
	}
}

func TestCreate_SpaceIDGuard_SpaceNotFound_DeniesAll(t *testing.T) {
	tm := New()
	// (nil, nil) = space not found → deny-all
	tm.SetMembershipChecker(&stubChecker{members: nil, err: nil})

	_, err := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "alice",
		Task:      "test task",
		SpaceID:   "nonexistent-space",
	})
	if err == nil {
		t.Fatal("expected deny when space not found (nil members)")
	}
	if !errors.Is(err, ErrAgentNotSpaceMember) {
		t.Errorf("expected ErrAgentNotSpaceMember, got: %v", err)
	}
}

func TestCreate_SpaceIDGuard_CheckerError_Propagated(t *testing.T) {
	tm := New()
	tm.SetMembershipChecker(&stubChecker{err: errors.New("db unavailable")})

	_, err := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "alice",
		Task:      "test task",
		SpaceID:   "space-abc",
	})
	if err == nil {
		t.Fatal("expected error when checker returns error")
	}
	if errors.Is(err, ErrAgentNotSpaceMember) {
		t.Error("expected transient error, not ErrAgentNotSpaceMember")
	}
}

func TestCreate_NoSpaceID_CheckerSkipped(t *testing.T) {
	tm := New()
	// Even with a checker that denies everything, no SpaceID means no check.
	tm.SetMembershipChecker(&stubChecker{members: nil, err: nil})

	_, err := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "alice",
		Task:      "test task",
		// SpaceID intentionally empty
	})
	if err != nil {
		t.Fatalf("expected create to succeed when SpaceID is empty, got: %v", err)
	}
}

func TestCreate_SpaceIDGuard_NilChecker_Skipped(t *testing.T) {
	tm := New()
	// No checker wired — guard must be skipped entirely.
	_, err := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "anyone",
		Task:      "test task",
		SpaceID:   "space-abc",
	})
	if err != nil {
		t.Fatalf("expected create to succeed when no checker is wired, got: %v", err)
	}
}

func TestCreate_SpaceIDGuard_CaseInsensitive(t *testing.T) {
	tm := New()
	// Registry stores "Alice" but lookup uses "alice" — must match case-insensitively.
	tm.SetMembershipChecker(&stubChecker{members: []string{"Alice"}})

	_, err := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "alice",
		Task:      "test task",
		SpaceID:   "space-abc",
	})
	if err != nil {
		t.Fatalf("expected case-insensitive match to allow alice, got: %v", err)
	}
}

func TestCreate_SpaceIDGuard_ConcurrentCreate(t *testing.T) {
	tm := New()
	tm.SetMembershipChecker(&stubChecker{members: []string{"worker"}})

	const goroutines = 20
	errs := make([]error, goroutines)
	done := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		idx := i
		go func() {
			_, errs[idx] = tm.Create(CreateParams{
				SessionID: "sess-concurrent",
				AgentID:   "worker",
				Task:      "concurrent task",
				SpaceID:   "space-abc",
			})
			done <- struct{}{}
		}()
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}
	for i, err := range errs {
		if err != nil && !errors.Is(err, ErrThreadLimitExceeded) {
			t.Errorf("goroutine %d: unexpected error: %v", i, err)
		}
	}
}
