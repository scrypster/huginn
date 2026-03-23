package workforce_test

import (
	"errors"
	"testing"

	"github.com/scrypster/huginn/internal/workforce"
)

func TestNewDelegationContext_Defaults(t *testing.T) {
	dc := workforce.NewDelegationContext("req-1", "Tom", 0)
	if dc.Originator != "Tom" {
		t.Errorf("expected Originator Tom, got %q", dc.Originator)
	}
	if dc.MaxDepth != 5 {
		t.Errorf("expected default MaxDepth 5, got %d", dc.MaxDepth)
	}
	if len(dc.Stack) != 1 || dc.Stack[0] != "Tom" {
		t.Errorf("expected Stack=[Tom], got %v", dc.Stack)
	}
}

func TestWithDelegate_Success(t *testing.T) {
	dc := workforce.NewDelegationContext("req-1", "Tom", 5)
	dc2, err := dc.WithDelegate("Sarah")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dc2.Stack) != 2 {
		t.Errorf("expected Stack len 2, got %d", len(dc2.Stack))
	}
	if dc2.Stack[1] != "Sarah" {
		t.Errorf("expected Stack[1]=Sarah, got %q", dc2.Stack[1])
	}
	// Original must be unchanged (immutable).
	if len(dc.Stack) != 1 {
		t.Errorf("original DelegationContext was mutated")
	}
}

func TestWithDelegate_CycleDetection(t *testing.T) {
	dc := workforce.NewDelegationContext("req-1", "Tom", 5)
	dc2, _ := dc.WithDelegate("Sarah")
	dc3, _ := dc2.WithDelegate("DevOps")

	_, err := dc3.WithDelegate("Tom") // Tom is already in stack
	if !errors.Is(err, workforce.ErrDelegationCycle) {
		t.Errorf("expected ErrDelegationCycle, got %v", err)
	}
}

func TestWithDelegate_DepthExceeded(t *testing.T) {
	dc := workforce.NewDelegationContext("req-1", "A", 3)
	dc, _ = dc.WithDelegate("B")
	dc, _ = dc.WithDelegate("C")
	// Stack is now [A, B, C] = len 3 = MaxDepth; next push must fail.
	_, err := dc.WithDelegate("D")
	if !errors.Is(err, workforce.ErrDelegationDepthExceeded) {
		t.Errorf("expected ErrDelegationDepthExceeded, got %v", err)
	}
}

func TestValidateKind(t *testing.T) {
	valid := []workforce.ArtifactKind{
		workforce.KindCodePatch, workforce.KindDocument,
		workforce.KindTimeline, workforce.KindStructuredData, workforce.KindFileBundle,
	}
	for _, k := range valid {
		if !workforce.ValidateKind(k) {
			t.Errorf("expected %q to be valid", k)
		}
	}
	if workforce.ValidateKind("code") {
		t.Error("expected old 'code' kind to be invalid")
	}
}

func TestValidateStatus(t *testing.T) {
	valid := []workforce.ArtifactStatus{
		workforce.StatusDraft, workforce.StatusAccepted,
		workforce.StatusRejected, workforce.StatusSuperseded, workforce.StatusFailed,
	}
	for _, s := range valid {
		if !workforce.ValidateStatus(s) {
			t.Errorf("expected %q to be valid", s)
		}
	}
	if workforce.ValidateStatus("published") {
		t.Error("expected old 'published' status to be invalid")
	}
}
