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
// finishes, with a real gap in between. If two runs overlap, the log reads
// start,start,... instead of start,end,start,end.
func writeOverlapCLI(t *testing.T) (binary, logFile string) {
	t.Helper()
	dir := t.TempDir()
	binary = filepath.Join(dir, "fake-claude.sh")
	logFile = filepath.Join(dir, "overlap.log")

	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	fmt.Fprintf(&sb, "printf 'start\\n' >> %q\n", logFile)
	sb.WriteString("sleep 0.3\n")
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
	binary, logFile := writeOverlapCLI(t)
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
func TestConcurrentTurnsOnDifferentSessionsDoNotSerialise(t *testing.T) {
	binary, logFile := writeOverlapCLI(t)

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
			_, _ = b.ChatCompletion(context.Background(), backend.ChatRequest{
				Messages: []backend.Message{{Role: "user", Content: "hi"}},
			})
		}(i)
	}
	close(start)
	wg.Wait()

	raw, _ := os.ReadFile(logFile)
	got := strings.Fields(string(raw))
	if len(got) != 4 {
		t.Fatalf("overlap log = %v, want four entries", got)
	}
	if got[0] != "start" || got[1] != "start" {
		t.Errorf("distinct sessions were serialised against each other: log = %v; the slot must be keyed by session id, not global", got)
	}
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
