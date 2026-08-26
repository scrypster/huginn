package claudecode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
)

// writeOverlapCLI writes a stand-in `claude` that records when it starts and
// finishes, with a gap in between. If two runs overlap, the log reads
// start,start,... instead of start,end,start,end.
//
// gateFile selects how the gap is produced. Empty means a fixed sleep, which
// is right for the serialisation test: there, correct behaviour is what the
// log ORDER shows, and the sleep only has to be long enough that a broken
// implementation is caught. A non-empty gateFile makes each run block until
// that file appears, which is what the NON-serialisation test needs — its
// passing condition is "both started at once", and hanging that on elapsed
// time is exactly the flake a loaded CI machine produces.
func writeOverlapCLI(t *testing.T, gateFile string) (binary, logFile string) {
	t.Helper()
	dir := t.TempDir()
	binary = filepath.Join(dir, "fake-claude.sh")
	logFile = filepath.Join(dir, "overlap.log")

	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	fmt.Fprintf(&sb, "printf 'start\\n' >> %q\n", logFile)
	if gateFile == "" {
		sb.WriteString("sleep 0.3\n")
	} else {
		fmt.Fprintf(&sb, "while [ ! -f %q ]; do sleep 0.01; done\n", gateFile)
	}
	fmt.Fprintf(&sb, "printf 'end\\n' >> %q\n", logFile)
	sb.WriteString("cat <<'HUGINN_EOF'\n")
	sb.WriteString(`{"type":"result","subtype":"success","result":"done","session_id":"S"}` + "\n")
	sb.WriteString("HUGINN_EOF\n")
	sb.WriteString("exit 0\n")

	if err := os.WriteFile(binary, []byte(sb.String()), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	return binary, logFile
}

// countStarts returns how many runs have announced themselves so far.
func countStarts(logFile string) int {
	raw, err := os.ReadFile(logFile)
	if err != nil {
		return 0
	}
	n := 0
	for _, f := range strings.Fields(string(raw)) {
		if f == "start" {
			n++
		}
	}
	return n
}

// TestConcurrentTurnsOnOneSessionSerialise is the regression test for a
// semaphore that lives on the backend INSTANCE.
//
// The backend is rebuilt for every turn, so an instance-scoped semaphore
// guards nothing: two concurrent turns each get their own empty channel and
// run two `claude` processes against the same session id, both writing the
// transcript that session's state is read back from.
//
// With the package-level, session-keyed semaphore the log must read
// start,end,start,end. With a per-instance one it reads start,start,end,end.
func TestConcurrentTurnsOnOneSessionSerialise(t *testing.T) {
	binary, logFile := writeOverlapCLI(t, "")
	const sessionID = "11111111-2222-3333-4444-555555555555"

	newBackend := func() *AgentBackend {
		// A SEPARATE instance per turn, exactly as the resolver builds them.
		return NewAgentBackend(AgentBackendConfig{
			Binary:      binary,
			SessionID:   sessionID,
			CWD:         t.TempDir(),
			Model:       "opus",
			TimeoutSecs: 30,
		})
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			b := newBackend()
			<-start // release both goroutines at once
			_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
				Messages: []backend.Message{{Role: "user", Content: fmt.Sprintf("turn %d", n)}},
			})
			errs <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("ChatCompletion: %v", err)
		}
	}

	raw, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read overlap log: %v", err)
	}
	got := strings.Fields(string(raw))
	want := []string{"start", "end", "start", "end"}
	if len(got) != len(want) {
		t.Fatalf("overlap log = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("two turns on one session ran concurrently: log = %v, want %v", got, want)
		}
	}
}

// TestConcurrentTurnsOnDifferentSessionsDoNotSerialise is the other half: the
// slot is per session, not global. Two agents must not queue behind each other.
//
// Synchronised on an observable event rather than on elapsed time. Both fake
// CLIs block until a gate file appears, so "did they overlap?" is answered by
// whether the second one ever started while the first was still running —
// which is a fact, not a race against a 300ms window on a loaded machine.
// Serialisation shows up as a timeout with a specific message, never as a
// flaky comparison.
func TestConcurrentTurnsOnDifferentSessionsDoNotSerialise(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "gate")
	binary, logFile := writeOverlapCLI(t, gate)
	openGate := func() { _ = os.WriteFile(gate, []byte("go\n"), 0o600) }

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			b := NewAgentBackend(AgentBackendConfig{
				Binary:      binary,
				SessionID:   fmt.Sprintf("session-%d", n),
				CWD:         t.TempDir(),
				TimeoutSecs: 30,
			})
			<-start
			// No t.* calls in here: this goroutine can outlive a failed test.
			_, _ = b.ChatCompletion(context.Background(), backend.ChatRequest{
				Messages: []backend.Message{{Role: "user", Content: "hi"}},
			})
		}(i)
	}
	// Whatever happens below, release the children and reap them, so a failure
	// cannot leave a blocked `sh` behind.
	t.Cleanup(func() {
		openGate()
		wg.Wait()
	})
	close(start)

	// Wait for BOTH runs to be in flight. If the slot were global, the second
	// could not start until the first finished — and the first cannot finish
	// until the gate opens, which happens only after this loop succeeds.
	deadline := time.Now().Add(15 * time.Second)
	for countStarts(logFile) < 2 {
		if time.Now().After(deadline) {
			t.Fatal("only one turn ever started: distinct sessions were serialised against each other; " +
				"the semaphore slot must be keyed by session id, not global")
		}
		time.Sleep(5 * time.Millisecond)
	}

	openGate()
	wg.Wait()

	got := strings.Fields(mustRead(t, logFile))
	if len(got) != 4 {
		t.Fatalf("overlap log = %v, want four entries", got)
	}
	if got[0] != "start" || got[1] != "start" {
		t.Errorf("log = %v, want both runs to have started before either finished", got)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func TestSemKeyForGivesEveryEmptySessionItsOwnSlot(t *testing.T) {
	a := semKeyFor("")
	b := semKeyFor("")
	if a == b {
		t.Fatalf("two empty session ids share the key %q; unrelated agents would queue behind one another", a)
	}
	if got, want := semKeyFor("abc"), semKeyFor("abc"); got != want {
		t.Fatalf("same session id produced different keys: %q vs %q", got, want)
	}
	if semKeyFor("abc") == semKeyFor("abd") {
		t.Fatal("distinct session ids collapsed onto one key")
	}
}

func TestAcquireSessionRespectsContextAndHoldsNothing(t *testing.T) {
	const key = "session:ctx-test"
	release, err := acquireSession(context.Background(), key)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	// A second caller must abandon the wait when ITS OWN context is cancelled,
	// and must not be holding the slot afterwards.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := acquireSession(ctx, key); err == nil {
		t.Fatal("a queued caller acquired the slot while it was held")
	}

	release()

	// The abandoned waiter must not have left a token behind: this must not block.
	done := make(chan struct{})
	go func() {
		r, err := acquireSession(context.Background(), key)
		if err == nil {
			r()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("slot never became free: a cancelled waiter leaked a token")
	}
}

func TestSessionSemRegistryDoesNotLeak(t *testing.T) {
	const key = "session:leak-test"
	release, err := acquireSession(context.Background(), key)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	sessionSems.mu.Lock()
	present := sessionSems.m[key] != nil
	sessionSems.mu.Unlock()
	if !present {
		t.Fatal("held slot missing from the registry")
	}

	release()

	sessionSems.mu.Lock()
	_, stillThere := sessionSems.m[key]
	sessionSems.mu.Unlock()
	if stillThere {
		t.Error("registry kept the slot after the last user released it; the map grows one entry per session forever")
	}
}
