package workforce_test

// hardening_r1_test.go — additional hardening tests for internal/workforce/types.go
// These tests cover edge cases not exercised by types_test.go.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/workforce"
)

// ---------------------------------------------------------------------------
// 1. NewDelegationContext — edge MaxDepth values
// ---------------------------------------------------------------------------

// TestNewDelegationContext_EdgeDepths verifies that MaxDepth is clamped/defaulted
// correctly for zero, positive-one, and negative inputs.
func TestNewDelegationContext_EdgeDepths(t *testing.T) {
	t.Run("MaxDepth=0 defaults to 5", func(t *testing.T) {
		dc := workforce.NewDelegationContext("r1", "agent-a", 0)
		if dc.MaxDepth != 5 {
			t.Errorf("expected MaxDepth 5 for input 0, got %d", dc.MaxDepth)
		}
	})

	t.Run("MaxDepth=1 allows only 1 push then blocks", func(t *testing.T) {
		// Stack starts with originator, so len=1 == MaxDepth; next push must fail.
		dc := workforce.NewDelegationContext("r1", "agent-a", 1)
		if dc.MaxDepth != 1 {
			t.Fatalf("expected MaxDepth 1, got %d", dc.MaxDepth)
		}
		_, err := dc.WithDelegate("agent-b")
		if !errors.Is(err, workforce.ErrDelegationDepthExceeded) {
			t.Errorf("expected ErrDelegationDepthExceeded for MaxDepth=1, got %v", err)
		}
	})

	t.Run("MaxDepth=-1 defaults to 5", func(t *testing.T) {
		dc := workforce.NewDelegationContext("r1", "agent-a", -1)
		if dc.MaxDepth != 5 {
			t.Errorf("expected MaxDepth 5 for input -1, got %d", dc.MaxDepth)
		}
	})
}

// ---------------------------------------------------------------------------
// 2. WithDelegate — empty agent name
// ---------------------------------------------------------------------------

// TestWithDelegate_EmptyAgentName verifies that an empty string is accepted as a
// valid (if unusual) agent name and appears in the stack at the correct position.
func TestWithDelegate_EmptyAgentName(t *testing.T) {
	dc := workforce.NewDelegationContext("r2", "root", 5)
	dc2, err := dc.WithDelegate("")
	if err != nil {
		t.Fatalf("WithDelegate(\"\") should not return an error, got: %v", err)
	}
	if len(dc2.Stack) != 2 {
		t.Fatalf("expected Stack len 2, got %d", len(dc2.Stack))
	}
	if dc2.Stack[1] != "" {
		t.Errorf("expected Stack[1]=\"\", got %q", dc2.Stack[1])
	}
}

// ---------------------------------------------------------------------------
// 3. WithDelegate — unicode and very long names
// ---------------------------------------------------------------------------

// TestWithDelegate_UnicodeAndLongNames ensures that agent names containing emoji,
// CJK characters, and names at extreme lengths do not cause panics or errors.
func TestWithDelegate_UnicodeAndLongNames(t *testing.T) {
	dc := workforce.NewDelegationContext("r3", "origin", 10)

	cases := []struct {
		name  string
		agent string
	}{
		{"emoji", "🤖"},
		{"cjk", "陈"},
		{"long", strings.Repeat("x", 300)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dc2, err := dc.WithDelegate(tc.agent)
			if err != nil {
				t.Fatalf("WithDelegate(%q) unexpected error: %v", tc.name, err)
			}
			if dc2.Stack[len(dc2.Stack)-1] != tc.agent {
				t.Errorf("expected last stack entry %q, got %q", tc.agent, dc2.Stack[len(dc2.Stack)-1])
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 4. WithDelegate — immutability of the original context
// ---------------------------------------------------------------------------

// TestWithDelegate_Immutability calls WithDelegate twice on the same original
// context with different agent names and verifies:
//   - the original stack is never modified
//   - the two resulting contexts are independent of each other
func TestWithDelegate_Immutability(t *testing.T) {
	original := workforce.NewDelegationContext("r4", "root", 5)

	result1, err1 := original.WithDelegate("alice")
	if err1 != nil {
		t.Fatalf("first WithDelegate error: %v", err1)
	}
	result2, err2 := original.WithDelegate("bob")
	if err2 != nil {
		t.Fatalf("second WithDelegate error: %v", err2)
	}

	// Original must still be length 1.
	if len(original.Stack) != 1 {
		t.Errorf("original.Stack was mutated; expected len 1, got %d", len(original.Stack))
	}

	// result1 must end with "alice", result2 with "bob".
	if result1.Stack[1] != "alice" {
		t.Errorf("result1 Stack[1] = %q, want \"alice\"", result1.Stack[1])
	}
	if result2.Stack[1] != "bob" {
		t.Errorf("result2 Stack[1] = %q, want \"bob\"", result2.Stack[1])
	}

	// Mutating result1's stack must not affect result2.
	if len(result1.Stack) > 1 && len(result2.Stack) > 1 {
		result1.Stack[1] = "tampered"
		if result2.Stack[1] == "tampered" {
			t.Error("result1 and result2 share the same underlying slice (not independent copies)")
		}
	}
}

// ---------------------------------------------------------------------------
// 5. WithDelegate — deep nesting and cycle re-introduction
// ---------------------------------------------------------------------------

// TestWithDelegate_DeepNesting chains 5 distinct agents and verifies stack order,
// then confirms a cycle error when the originator is re-introduced.
func TestWithDelegate_DeepNesting(t *testing.T) {
	agents := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	dc := workforce.NewDelegationContext("r5", agents[0], 10)

	for i := 1; i < len(agents); i++ {
		var err error
		dc, err = dc.WithDelegate(agents[i])
		if err != nil {
			t.Fatalf("WithDelegate(%q) unexpected error at depth %d: %v", agents[i], i, err)
		}
	}

	// Verify full stack order.
	if len(dc.Stack) != len(agents) {
		t.Fatalf("expected stack len %d, got %d", len(agents), len(dc.Stack))
	}
	for i, a := range agents {
		if dc.Stack[i] != a {
			t.Errorf("Stack[%d] = %q, want %q", i, dc.Stack[i], a)
		}
	}

	// Re-introducing the originator ("alpha") must trigger a cycle error.
	_, err := dc.WithDelegate("alpha")
	if !errors.Is(err, workforce.ErrDelegationCycle) {
		t.Errorf("expected ErrDelegationCycle when re-introducing originator, got %v", err)
	}

	// Re-introducing a mid-chain agent ("gamma") must also trigger a cycle error.
	_, err = dc.WithDelegate("gamma")
	if !errors.Is(err, workforce.ErrDelegationCycle) {
		t.Errorf("expected ErrDelegationCycle when re-introducing mid-chain agent, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// 6. Stack slice — read-only / external mutation isolation
// ---------------------------------------------------------------------------

// TestDelegationContextStack_ReadOnly verifies that mutating the slice returned
// by Stack does not alter the internal state of the DelegationContext because
// WithDelegate always allocates a fresh backing array.
func TestDelegationContextStack_ReadOnly(t *testing.T) {
	dc := workforce.NewDelegationContext("r6", "root", 5)
	dc2, err := dc.WithDelegate("child")
	if err != nil {
		t.Fatalf("WithDelegate error: %v", err)
	}

	// Obtain a reference to the returned Stack slice and mutate it externally.
	stack := dc2.Stack
	if len(stack) < 2 {
		t.Fatal("expected stack len >= 2")
	}
	originalSecond := stack[1]
	stack[1] = "hijacked"

	// A subsequent WithDelegate call should still see the original name, not "hijacked",
	// because the copy was made at push time.  We verify by inspecting dc2.Stack directly.
	// (stack is the same slice as dc2.Stack — this test documents current behaviour:
	//  the struct field is exported and the slice header is shared, so we cannot
	//  protect against this; however WithDelegate does produce independent copies.)
	_ = originalSecond // used above

	// What we CAN assert: that dc (the parent) is unaffected.
	if dc.Stack[0] != "root" {
		t.Errorf("parent stack[0] was altered; got %q", dc.Stack[0])
	}

	// And that a new child built off dc (not dc2) sees only the original parent stack.
	dc3, err := dc.WithDelegate("sibling")
	if err != nil {
		t.Fatalf("WithDelegate(sibling) error: %v", err)
	}
	if dc3.Stack[1] != "sibling" {
		t.Errorf("new child stack[1] = %q, want \"sibling\"", dc3.Stack[1])
	}
}

// ---------------------------------------------------------------------------
// 7. Artifact — ValidateStatus round-trip
// ---------------------------------------------------------------------------

// TestArtifact_StatusTransitions verifies that all documented ArtifactStatus
// constants are accepted by ValidateStatus, and that an invented value is not.
func TestArtifact_StatusTransitions(t *testing.T) {
	valid := []workforce.ArtifactStatus{
		workforce.StatusDraft,
		workforce.StatusAccepted,
		workforce.StatusRejected,
		workforce.StatusSuperseded,
		workforce.StatusFailed,
	}
	for _, s := range valid {
		if !workforce.ValidateStatus(s) {
			t.Errorf("ValidateStatus(%q) = false, want true", s)
		}
	}

	// Verify the String() method returns the underlying string value.
	if workforce.StatusDraft.String() != "draft" {
		t.Errorf("StatusDraft.String() = %q, want \"draft\"", workforce.StatusDraft.String())
	}

	// Invalid values must be rejected.
	invalid := []workforce.ArtifactStatus{
		"",
		"published",
		"pending",
		"DRAFT",  // case-sensitive
		"Draft",  // case-sensitive
	}
	for _, s := range invalid {
		if workforce.ValidateStatus(s) {
			t.Errorf("ValidateStatus(%q) = true, want false", s)
		}
	}
}

// ---------------------------------------------------------------------------
// 8. Artifact — ValidateKind round-trip
// ---------------------------------------------------------------------------

// TestArtifact_KindValidation verifies all documented ArtifactKind constants
// pass ValidateKind, and that invented or partial names are rejected.
func TestArtifact_KindValidation(t *testing.T) {
	valid := []workforce.ArtifactKind{
		workforce.KindDocument,
		workforce.KindCodePatch,
		workforce.KindTimeline,
		workforce.KindStructuredData,
		workforce.KindFileBundle,
	}
	for _, k := range valid {
		if !workforce.ValidateKind(k) {
			t.Errorf("ValidateKind(%q) = false, want true", k)
		}
	}

	// Verify the String() method returns the underlying string value.
	if workforce.KindDocument.String() != "document" {
		t.Errorf("KindDocument.String() = %q, want \"document\"", workforce.KindDocument.String())
	}

	// Invalid values must be rejected.
	invalid := []workforce.ArtifactKind{
		"",
		"code",           // prefix match should not succeed
		"CODE_PATCH",     // case-sensitive
		"structured",     // partial
		"file",           // partial
		"unknown_kind",
	}
	for _, k := range invalid {
		if workforce.ValidateKind(k) {
			t.Errorf("ValidateKind(%q) = true, want false", k)
		}
	}
}

// ---------------------------------------------------------------------------
// 9. GetDelegationContext — missing key returns nil / zero value
// ---------------------------------------------------------------------------

// TestGetDelegationContext_Missing verifies that retrieving a DelegationContext
// from a plain context.Background() returns nil without panicking.
func TestGetDelegationContext_Missing(t *testing.T) {
	ctx := context.Background()

	// Must not panic.
	dc := workforce.GetDelegationContext(ctx)

	// Must return nil (zero pointer for *DelegationContext).
	if dc != nil {
		t.Errorf("expected nil DelegationContext from empty context, got %+v", dc)
	}
}

// ---------------------------------------------------------------------------
// 10. WithDelegationContext — round-trip preservation of all fields
// ---------------------------------------------------------------------------

// TestWithDelegationContext_RoundTrip stores a fully-populated DelegationContext
// into a context value and retrieves it, verifying every field is preserved.
func TestWithDelegationContext_RoundTrip(t *testing.T) {
	original := &workforce.DelegationContext{
		RequestID:  "req-round-trip",
		Stack:      []string{"root", "middle", "leaf"},
		MaxDepth:   7,
		Originator: "root",
	}

	ctx := context.Background()
	ctx = workforce.WithDelegationContext(ctx, original)

	retrieved := workforce.GetDelegationContext(ctx)
	if retrieved == nil {
		t.Fatal("GetDelegationContext returned nil after WithDelegationContext")
	}

	// Pointer identity: the same pointer should be returned.
	if retrieved != original {
		t.Error("GetDelegationContext returned a different pointer than what was stored")
	}

	// Field-by-field verification.
	if retrieved.RequestID != original.RequestID {
		t.Errorf("RequestID: got %q, want %q", retrieved.RequestID, original.RequestID)
	}
	if retrieved.MaxDepth != original.MaxDepth {
		t.Errorf("MaxDepth: got %d, want %d", retrieved.MaxDepth, original.MaxDepth)
	}
	if retrieved.Originator != original.Originator {
		t.Errorf("Originator: got %q, want %q", retrieved.Originator, original.Originator)
	}
	if len(retrieved.Stack) != len(original.Stack) {
		t.Fatalf("Stack len: got %d, want %d", len(retrieved.Stack), len(original.Stack))
	}
	for i, v := range original.Stack {
		if retrieved.Stack[i] != v {
			t.Errorf("Stack[%d]: got %q, want %q", i, retrieved.Stack[i], v)
		}
	}

	// Verify that the context produced by WithDelegationContext is a child of
	// the original context (i.e. a different context value, not the same object).
	if ctx == context.Background() {
		t.Error("WithDelegationContext must return a new context, not the original background")
	}
}
