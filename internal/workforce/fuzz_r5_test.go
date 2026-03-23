package workforce_test

// fuzz_r5_test.go — stress / fuzz-like tests for internal/workforce types.

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/workforce"
)

// TestDelegationContext_MaxDepth_Boundary verifies that exactly MaxDepth-1 delegates
// can be added (filling the stack to MaxDepth), and the next push returns ErrDelegationDepthExceeded.
// MaxDepth=5: stack starts with originator [A], so we can add B,C,D,E (4 more = 5 total).
// The 5th push (to agent F) must fail.
func TestDelegationContext_MaxDepth_Boundary(t *testing.T) {
	dc := workforce.NewDelegationContext("req-depth", "A", 5)

	// Push B through E — these should all succeed.
	agents := []string{"B", "C", "D", "E"}
	for _, a := range agents {
		var err error
		dc, err = dc.WithDelegate(a)
		if err != nil {
			t.Fatalf("WithDelegate(%q) unexpected error: %v", a, err)
		}
	}

	// Stack is now [A, B, C, D, E] = 5 entries = MaxDepth. The next push must fail.
	_, err := dc.WithDelegate("F")
	if !errors.Is(err, workforce.ErrDelegationDepthExceeded) {
		t.Errorf("6th push: expected ErrDelegationDepthExceeded, got %v", err)
	}
}

// TestDelegationContext_CycleDetection_LongChain verifies that a cycle is detected
// when the chain wraps: A→B→C→D→E→A (A already in stack).
func TestDelegationContext_CycleDetection_LongChain(t *testing.T) {
	// Use MaxDepth=10 so the cycle attempt is not blocked by depth first.
	dc := workforce.NewDelegationContext("req-cycle", "A", 10)

	chain := []string{"B", "C", "D", "E"}
	for _, a := range chain {
		var err error
		dc, err = dc.WithDelegate(a)
		if err != nil {
			t.Fatalf("WithDelegate(%q) unexpected error: %v", a, err)
		}
	}

	// Stack is now [A, B, C, D, E]. Adding "A" should produce ErrDelegationCycle.
	_, err := dc.WithDelegate("A")
	if !errors.Is(err, workforce.ErrDelegationCycle) {
		t.Errorf("cycle at step 6 (A→B→C→D→E→A): expected ErrDelegationCycle, got %v", err)
	}
}

// TestArtifact_AllStatusKindCombinations creates an Artifact for every combination of
// ArtifactKind and ArtifactStatus and verifies that no combination panics during field access.
func TestArtifact_AllStatusKindCombinations(t *testing.T) {
	kinds := []workforce.ArtifactKind{
		workforce.KindCodePatch,
		workforce.KindDocument,
		workforce.KindTimeline,
		workforce.KindStructuredData,
		workforce.KindFileBundle,
	}
	statuses := []workforce.ArtifactStatus{
		workforce.StatusDraft,
		workforce.StatusAccepted,
		workforce.StatusRejected,
		workforce.StatusSuperseded,
		workforce.StatusFailed,
	}

	for _, k := range kinds {
		for _, s := range statuses {
			k, s := k, s
			t.Run(string(k)+"/"+string(s), func(t *testing.T) {
				a := &workforce.Artifact{
					ID:        "test-id",
					Kind:      k,
					Title:     "combination test",
					AgentName: "test-agent",
					SessionID: "test-session",
					Status:    s,
					Content:   []byte("data"),
				}
				// Access all fields — must not panic.
				_ = a.ID
				_ = a.Kind
				_ = a.Title
				_ = a.MimeType
				_ = a.Content
				_ = a.ContentRef
				_ = a.Metadata
				_ = a.AgentName
				_ = a.ThreadID
				_ = a.SessionID
				_ = a.TriggeringMessageID
				_ = a.Status
				_ = a.RejectionReason
				_ = a.CreatedAt
				_ = a.UpdatedAt
				_ = a.Kind.String()
				_ = a.Status.String()
				_ = workforce.ValidateKind(a.Kind)
				_ = workforce.ValidateStatus(a.Status)
			})
		}
	}
}

// TestWithDelegate_ConcurrentContextCreation verifies that 20 goroutines calling
// WithDelegate on the same root context concurrently all succeed or fail cleanly,
// with no panics and no data races.
func TestWithDelegate_ConcurrentContextCreation(t *testing.T) {
	root := workforce.NewDelegationContext("req-concurrent", "root", 5)

	const goroutines = 20
	results := make([]struct {
		dc  workforce.DelegationContext
		err error
	}, goroutines)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			// Each goroutine tries to add a unique agent — cycle is impossible here
			// since root starts fresh for each call, and we use unique names.
			dc, err := root.WithDelegate("agent-" + string(rune('A'+i%26)) + "-" + string(rune('0'+i/26)))
			results[i].dc = dc
			results[i].err = err
		}()
	}
	wg.Wait()

	// The root stack has 1 entry; MaxDepth=5. Each goroutine calls WithDelegate once on root
	// (stack len 1 < 5), so all 20 calls should succeed.
	for i, r := range results {
		if r.err != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, r.err)
		}
		if len(r.dc.Stack) != 2 {
			t.Errorf("goroutine %d: expected stack len 2, got %d", i, len(r.dc.Stack))
		}
	}

	// The original context must be unmodified.
	if len(root.Stack) != 1 || root.Stack[0] != "root" {
		t.Errorf("root context was mutated by concurrent WithDelegate calls: stack=%v", root.Stack)
	}
}

// TestArtifact_ZeroValue verifies that a zero-value Artifact{} does not panic
// when all exported fields are accessed.
func TestArtifact_ZeroValue(t *testing.T) {
	var a workforce.Artifact

	// Access every field — must not panic.
	_ = a.ID
	_ = a.Kind
	_ = a.Title
	_ = a.MimeType
	_ = a.Content
	_ = a.ContentRef
	_ = a.Metadata
	_ = a.AgentName
	_ = a.ThreadID
	_ = a.SessionID
	_ = a.TriggeringMessageID
	_ = a.Status
	_ = a.RejectionReason
	_ = a.CreatedAt
	_ = a.UpdatedAt

	// Call methods on the zero-value status and kind.
	_ = a.Kind.String()
	_ = a.Status.String()

	// ValidateKind / ValidateStatus on zero values must return false, not panic.
	if workforce.ValidateKind(a.Kind) {
		t.Error("ValidateKind on zero-value Kind should return false")
	}
	if workforce.ValidateStatus(a.Status) {
		t.Error("ValidateStatus on zero-value Status should return false")
	}

	// Map access on nil Metadata must not panic.
	_ = a.Metadata["key"]
	if a.Metadata != nil {
		t.Error("zero-value Metadata should be nil")
	}

	// Validate context round-trip with a nil-valued *DelegationContext.
	ctx := context.Background()
	ctx = workforce.WithDelegationContext(ctx, nil)
	dc := workforce.GetDelegationContext(ctx)
	if dc != nil {
		t.Errorf("expected nil DelegationContext from nil store, got %+v", dc)
	}
}
