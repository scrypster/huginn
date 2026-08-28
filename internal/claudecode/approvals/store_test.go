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

// TestListCanRememberFalseWhenTruncated is the guard against prefix matching
// re-entering through the back door.
//
// The hook clips a Bash command at maxCommandBytes. Everything past the clip
// is unconstrained, so remembering a truncated command would authorise every
// later command that merely SHARES ITS FIRST 4 KiB — exactly the prefix match
// this package forbids, and with no card, no broadcast and no human.
func TestListCanRememberFalseWhenTruncated(t *testing.T) {
	s := New(time.Minute)
	defer s.Close()
	r := req()
	r.SummaryTruncated = true
	if _, err := s.Register(r); err != nil {
		t.Fatal(err)
	}
	if s.List()[0].CanRemember {
		t.Fatal("CanRemember = true for a TRUNCATED command; the bytes past the clip are " +
			"unconstrained, so remembering it is prefix matching")
	}
}

// TestListCanRememberFalseWhenSummaryEmpty covers the other half: the hook
// returns an empty summary whenever tool_input has no usable `command` (a
// non-string, a missing key, an unparseable input). Remembering "" would
// auto-allow EVERY future Bash call whose input failed to parse.
func TestListCanRememberFalseWhenSummaryEmpty(t *testing.T) {
	s := New(time.Minute)
	defer s.Close()
	for _, summary := range []string{"", "   ", "\n\t"} {
		r := req()
		r.Summary = summary
		p, err := s.Register(r)
		if err != nil {
			t.Fatal(err)
		}
		var got bool
		for _, v := range s.List() {
			if v.ID == p.ID {
				got = v.CanRemember
			}
		}
		if got {
			t.Fatalf("CanRemember = true for summary %q; an empty command must never be "+
				"rememberable or every unparseable Bash input auto-allows", summary)
		}
	}
}

// TestRememberRefusesEmptyCommand pins the guard inside the store itself. The
// rule must not depend on the caller — or on the hook — being honest.
func TestRememberRefusesEmptyCommand(t *testing.T) {
	s := New(time.Minute)
	defer s.Close()
	for _, cmd := range []string{"", "   ", "\r\n"} {
		s.Remember("codey", "Bash", cmd)
		if s.Remembered("codey", "Bash", cmd) {
			t.Fatalf("Remember(%q) stored an empty command; every Bash call with an "+
				"unparseable tool_input would then auto-allow", cmd)
		}
		// The trimmed-equal spelling matters too: memKey trims trailing
		// whitespace, so a stored "  " would also match a later "".
		if s.Remembered("codey", "Bash", "") {
			t.Fatalf("Remember(%q) made the empty command remembered", cmd)
		}
	}
}

// TestRememberableRejectsTruncatedAndEmpty pins the shared predicate directly,
// so the rule cannot drift between List() and the server handler.
func TestRememberableRejectsTruncatedAndEmpty(t *testing.T) {
	ok := Request{ToolName: "Bash", Summary: "go test ./..."}
	if !ok.Rememberable() {
		t.Fatal("a complete, non-empty Bash command must be rememberable")
	}
	truncated := ok
	truncated.SummaryTruncated = true
	if truncated.Rememberable() {
		t.Fatal("a truncated command must never be rememberable")
	}
	empty := ok
	empty.Summary = "  \n"
	if empty.Rememberable() {
		t.Fatal("a whitespace-only command must never be rememberable")
	}
	write := ok
	write.ToolName = "Write"
	if write.Rememberable() {
		t.Fatal("only Bash carries exact-command memory")
	}
}

// TestCloseReleasesPendingWaitersWithDeny pins Close's contract at the store
// level: Server.Stop relies on it to unblock shutdown, and every released
// waiter must land on Deny.
func TestCloseReleasesPendingWaitersWithDeny(t *testing.T) {
	// A deadline far beyond this test: only Close can end this Wait in time.
	s := New(5 * time.Minute)
	p, err := s.Register(req())
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan Decision, 1)
	go func() { got <- s.Wait(context.Background(), p) }()

	// Let the waiter park before closing, so this exercises the release path
	// rather than a Wait that was never blocked.
	time.Sleep(20 * time.Millisecond)
	s.Close()

	select {
	case d := <-got:
		if d != Deny {
			t.Fatalf("Wait after Close = %v, want Deny", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not release a parked waiter; Server.Stop then blocks in " +
			"Shutdown for the whole approval deadline")
	}
	s.Close() // idempotent
}
