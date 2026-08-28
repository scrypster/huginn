package approvals

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func req() Request {
	return Request{AgentName: "codey", ToolName: "Bash", Summary: "go test ./...", CWD: "/tmp"}
}

func TestDeliverAllowUnblocksWait(t *testing.T) {
	s := New(5 * time.Second)
	defer s.Close()
	p, err := s.Register(req())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	go func() {
		// Poll until the waiter is parked, then deliver.
		for i := 0; i < 100; i++ {
			if s.Deliver(p.ID, Allow) {
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	if got := s.Wait(context.Background(), p); got != Allow {
		t.Fatalf("Wait = %v, want Allow", got)
	}
}

func TestWaitDeniesOnDeadline(t *testing.T) {
	// A NON-ZERO deadline: a zero deadline would deny instantly for the wrong
	// reason and this test would pass against a store that never waits at all.
	s := New(60 * time.Millisecond)
	defer s.Close()
	p, _ := s.Register(req())
	start := time.Now()
	got := s.Wait(context.Background(), p)
	elapsed := time.Since(start)
	if got != Deny {
		t.Fatalf("Wait = %v, want Deny", got)
	}
	if elapsed < 50*time.Millisecond {
		t.Fatalf("Wait returned after %v; it did not actually block", elapsed)
	}
}

func TestDeliverUnknownIDReturnsFalse(t *testing.T) {
	s := New(time.Second)
	defer s.Close()
	if s.Deliver("nope", Allow) {
		t.Fatal("Deliver on unknown id returned true")
	}
}

func TestDeliverAfterDeadlineReturnsFalse(t *testing.T) {
	s := New(20 * time.Millisecond)
	defer s.Close()
	p, _ := s.Register(req())
	if got := s.Wait(context.Background(), p); got != Deny {
		t.Fatalf("Wait = %v, want Deny", got)
	}
	if s.Deliver(p.ID, Allow) {
		t.Fatal("Deliver succeeded after the deadline had already denied")
	}
}

func TestConcurrentDeliverAndExpireProduceOneWinner(t *testing.T) {
	// Run under -race. Exactly one of {delivered decision, deadline deny} must win.
	for i := 0; i < 50; i++ {
		s := New(10 * time.Millisecond)
		p, _ := s.Register(req())
		var wg sync.WaitGroup
		wg.Add(1)
		var delivered bool
		go func() {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond)
			delivered = s.Deliver(p.ID, Allow)
		}()
		got := s.Wait(context.Background(), p)
		wg.Wait()
		if delivered && got != Allow {
			t.Fatalf("Deliver reported success but Wait returned %v", got)
		}
		if !delivered && got != Deny {
			t.Fatalf("Deliver failed but Wait returned %v", got)
		}
		s.Close()
	}
}

func TestRegisterErrorsAtCap(t *testing.T) {
	s := New(time.Minute)
	defer s.Close()
	for i := 0; i < maxPending; i++ {
		if _, err := s.Register(req()); err != nil {
			t.Fatalf("Register %d failed early: %v", i, err)
		}
	}
	if _, err := s.Register(req()); !errors.Is(err, ErrTooManyPending) {
		t.Fatalf("Register past cap err = %v, want ErrTooManyPending", err)
	}
}

func TestListReportsRemainingMS(t *testing.T) {
	s := New(500 * time.Millisecond)
	defer s.Close()
	if _, err := s.Register(req()); err != nil {
		t.Fatal(err)
	}
	v := s.List()
	if len(v) != 1 {
		t.Fatalf("List len = %d, want 1", len(v))
	}
	if v[0].RemainingMS <= 0 || v[0].RemainingMS > 500 {
		t.Fatalf("RemainingMS = %d, want (0,500]", v[0].RemainingMS)
	}
	if !v[0].CanRemember {
		t.Fatal("CanRemember = false for a Bash request, want true")
	}
}

func TestListCanRememberFalseForNonBash(t *testing.T) {
	s := New(time.Minute)
	defer s.Close()
	r := req()
	r.ToolName = "Write"
	if _, err := s.Register(r); err != nil {
		t.Fatal(err)
	}
	if s.List()[0].CanRemember {
		t.Fatal("CanRemember = true for Write; exact-command memory is Bash-only")
	}
}

func TestIDsAreUnique(t *testing.T) {
	s := New(time.Minute)
	defer s.Close()
	seen := map[string]bool{}
	for i := 0; i < 32; i++ {
		p, err := s.Register(req())
		if err != nil {
			t.Fatal(err)
		}
		if seen[p.ID] {
			t.Fatalf("duplicate id %q", p.ID)
		}
		seen[p.ID] = true
	}
}
