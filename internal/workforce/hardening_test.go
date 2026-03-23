package workforce_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/workforce"
)

// ── DelegationContext boundary tests ─────────────────────────────────────────

func TestNewDelegationContext_NegativeMaxDepth_DefaultsTo5(t *testing.T) {
	dc := workforce.NewDelegationContext("req-1", "Tom", -10)
	if dc.MaxDepth != 5 {
		t.Errorf("expected MaxDepth 5 for negative input, got %d", dc.MaxDepth)
	}
}

func TestNewDelegationContext_PreservesRequestID(t *testing.T) {
	dc := workforce.NewDelegationContext("req-abc-123", "Tom", 5)
	if dc.RequestID != "req-abc-123" {
		t.Errorf("expected RequestID %q, got %q", "req-abc-123", dc.RequestID)
	}
}

func TestWithDelegate_SelfDelegation_ReturnsError(t *testing.T) {
	dc := workforce.NewDelegationContext("req-1", "Tom", 5)
	_, err := dc.WithDelegate("Tom")
	if !errors.Is(err, workforce.ErrDelegationCycle) {
		t.Errorf("self-delegation should return ErrDelegationCycle, got %v", err)
	}
}

func TestWithDelegate_EmptyAgentName_Succeeds(t *testing.T) {
	// Empty string is technically a valid agent name at the type level.
	dc := workforce.NewDelegationContext("req-1", "Tom", 5)
	dc2, err := dc.WithDelegate("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dc2.Stack) != 2 || dc2.Stack[1] != "" {
		t.Errorf("expected Stack=[Tom, ], got %v", dc2.Stack)
	}
}

func TestWithDelegate_MaxDepth1_OnlyOriginatorAllowed(t *testing.T) {
	dc := workforce.NewDelegationContext("req-1", "Tom", 1)
	// Stack is [Tom], len=1 which equals MaxDepth=1, so push fails.
	_, err := dc.WithDelegate("Sarah")
	if !errors.Is(err, workforce.ErrDelegationDepthExceeded) {
		t.Errorf("expected depth exceeded for MaxDepth=1, got %v", err)
	}
}

func TestWithDelegate_ChainPreservesOriginator(t *testing.T) {
	dc := workforce.NewDelegationContext("req-1", "Tom", 10)
	dc2, _ := dc.WithDelegate("Sarah")
	dc3, _ := dc2.WithDelegate("DevOps")
	if dc3.Originator != "Tom" {
		t.Errorf("Originator should be Tom throughout chain, got %q", dc3.Originator)
	}
}

func TestWithDelegate_DoesNotMutateOriginal(t *testing.T) {
	dc := workforce.NewDelegationContext("req-1", "Tom", 5)
	original := make([]string, len(dc.Stack))
	copy(original, dc.Stack)

	dc2, _ := dc.WithDelegate("Sarah")
	dc2.Stack[0] = "MUTATED" // mutating the new stack

	if dc.Stack[0] != original[0] {
		t.Error("mutation of derived context leaked back to original")
	}
}

func TestWithDelegate_ExactlyAtDepth_CycleCheckFirst(t *testing.T) {
	// When at max depth AND the agent is already in the chain, cycle error takes priority.
	dc := workforce.NewDelegationContext("req-1", "A", 2)
	dc2, _ := dc.WithDelegate("B")
	// Stack=[A,B], MaxDepth=2. Adding "A" hits both cycle and depth. Cycle should trigger first.
	_, err := dc2.WithDelegate("A")
	if !errors.Is(err, workforce.ErrDelegationCycle) {
		t.Errorf("expected cycle detection to take priority, got %v", err)
	}
}

// ── Context value round-trip ─────────────────────────────────────────────────

func TestDelegationContext_ContextRoundTrip(t *testing.T) {
	dc := &workforce.DelegationContext{
		RequestID:  "req-42",
		Stack:      []string{"Tom", "Sarah"},
		MaxDepth:   5,
		Originator: "Tom",
	}
	ctx := workforce.WithDelegationContext(context.Background(), dc)
	got := workforce.GetDelegationContext(ctx)
	if got == nil {
		t.Fatal("expected non-nil DelegationContext from context")
	}
	if got.RequestID != "req-42" {
		t.Errorf("expected RequestID %q, got %q", "req-42", got.RequestID)
	}
	if len(got.Stack) != 2 {
		t.Errorf("expected stack len 2, got %d", len(got.Stack))
	}
}

func TestGetDelegationContext_NoValue_ReturnsNil(t *testing.T) {
	got := workforce.GetDelegationContext(context.Background())
	if got != nil {
		t.Errorf("expected nil when no DelegationContext in context, got %+v", got)
	}
}

func TestGetDelegationContext_WrongType_ReturnsNil(t *testing.T) {
	// Using a different key type should not return a DelegationContext.
	ctx := context.WithValue(context.Background(), "wrong-key", "wrong-value")
	got := workforce.GetDelegationContext(ctx)
	if got != nil {
		t.Errorf("expected nil for wrong key type, got %+v", got)
	}
}

// ── Concurrent delegation ────────────────────────────────────────────────────

func TestWithDelegate_ConcurrentPushes_NoRace(t *testing.T) {
	dc := workforce.NewDelegationContext("req-1", "Tom", 100)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := string(rune('A' + (n % 26)))
			// Each goroutine pushes independently from the same base; no mutation expected.
			_, _ = dc.WithDelegate(name)
		}(i)
	}
	wg.Wait()
	// Original should be untouched.
	if len(dc.Stack) != 1 {
		t.Errorf("original stack was mutated under concurrency: len=%d", len(dc.Stack))
	}
}

// ── ValidateKind / ValidateStatus edge cases ─────────────────────────────────

func TestValidateKind_EmptyString(t *testing.T) {
	if workforce.ValidateKind("") {
		t.Error("empty string should not be a valid kind")
	}
}

func TestValidateStatus_EmptyString(t *testing.T) {
	if workforce.ValidateStatus("") {
		t.Error("empty string should not be a valid status")
	}
}

func TestValidateKind_CaseSensitive(t *testing.T) {
	if workforce.ValidateKind("DOCUMENT") {
		t.Error("ValidateKind should be case-sensitive")
	}
	if workforce.ValidateKind("Document") {
		t.Error("ValidateKind should be case-sensitive")
	}
}

func TestValidateStatus_CaseSensitive(t *testing.T) {
	if workforce.ValidateStatus("DRAFT") {
		t.Error("ValidateStatus should be case-sensitive")
	}
}

// ── Stringer ─────────────────────────────────────────────────────────────────

func TestArtifactKind_String(t *testing.T) {
	if workforce.KindCodePatch.String() != "code_patch" {
		t.Errorf("expected %q, got %q", "code_patch", workforce.KindCodePatch.String())
	}
}

func TestArtifactStatus_String(t *testing.T) {
	if workforce.StatusDraft.String() != "draft" {
		t.Errorf("expected %q, got %q", "draft", workforce.StatusDraft.String())
	}
}

// ── Sentinel errors ──────────────────────────────────────────────────────────

func TestSentinelErrors_AreDistinct(t *testing.T) {
	errs := []error{
		workforce.ErrDelegationCycle,
		workforce.ErrDelegationDepthExceeded,
		workforce.ErrAgentUnavailable,
		workforce.ErrArtifactNotFound,
	}
	for i, a := range errs {
		for j, b := range errs {
			if i != j && errors.Is(a, b) {
				t.Errorf("sentinel errors %d and %d should be distinct", i, j)
			}
		}
	}
}
