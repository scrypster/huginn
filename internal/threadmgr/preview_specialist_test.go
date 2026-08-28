package threadmgr_test

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/threadmgr"
)

// TestApproveSpecialist_Conditional_AlwaysRequiresApproval verifies S10:
// unlike Approve, ApproveSpecialist in conditional mode never auto-approves
// on the risky-hint heuristic — every specialist spawn blocks for a human ack.
func TestApproveSpecialist_Conditional_AlwaysRequiresApproval(t *testing.T) {
	preview := threadmgr.NewDelegationPreviewGateWithConfig("conditional", 2*time.Second)
	ready := make(chan struct{})
	resultCh := make(chan bool, 1)
	go func() {
		approved := preview.ApproveSpecialist(
			context.Background(),
			"sess-spec", "t-spec1", "Rust Audit Specialist", "list top 3 Go testing tips", "",
			threadmgr.SpecialistPreviewInfo{Model: "claude-opus-4-6", InputCostPerMTok: 5, OutputCostPerMTok: 25},
			func(_, _ string, _ map[string]any) { close(ready) },
		)
		resultCh <- approved
	}()

	select {
	case <-ready:
		// good — broadcast happened even for a benign-sounding task
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected specialist spawn to always broadcast for approval in conditional mode")
	}
	preview.Ack("sess-spec", "t-spec1", true)
	select {
	case result := <-resultCh:
		if !result {
			t.Error("expected approval after Ack(true)")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for ApproveSpecialist to return")
	}
}

// TestApproveSpecialist_PayloadCarriesModelAndCost verifies the preview
// payload includes the model and estimated cost (S10 cost visibility).
func TestApproveSpecialist_PayloadCarriesModelAndCost(t *testing.T) {
	preview := threadmgr.NewDelegationPreviewGateWithConfig("manual", 2*time.Second)
	ready := make(chan struct{})
	var gotPayload map[string]any
	resultCh := make(chan bool, 1)
	go func() {
		approved := preview.ApproveSpecialist(
			context.Background(),
			"sess-spec2", "t-spec2", "Rust Audit Specialist", "audit crate", "",
			threadmgr.SpecialistPreviewInfo{Model: "claude-opus-4-6", InputCostPerMTok: 5, OutputCostPerMTok: 25},
			func(_, _ string, payload map[string]any) {
				gotPayload = payload
				close(ready)
			},
		)
		resultCh <- approved
	}()
	<-ready
	if gotPayload["model"] != "claude-opus-4-6" {
		t.Errorf("expected model in payload, got %v", gotPayload["model"])
	}
	if gotPayload["input_cost_per_mtok"] != 5.0 {
		t.Errorf("expected input cost in payload, got %v", gotPayload["input_cost_per_mtok"])
	}
	if gotPayload["output_cost_per_mtok"] != 25.0 {
		t.Errorf("expected output cost in payload, got %v", gotPayload["output_cost_per_mtok"])
	}
	if gotPayload["specialist"] != true {
		t.Errorf("expected specialist=true in payload, got %v", gotPayload["specialist"])
	}
	preview.Ack("sess-spec2", "t-spec2", true)
	<-resultCh
}

// TestApproveSpecialist_TimeoutDenies verifies S10's reversal: a specialist
// spawn preview that times out without a human ack is DENIED, not
// auto-approved (unlike the general Approve() timeout default).
func TestApproveSpecialist_TimeoutDenies(t *testing.T) {
	preview := threadmgr.NewDelegationPreviewGateWithConfig("manual", 25*time.Millisecond)
	approved := preview.ApproveSpecialist(
		context.Background(),
		"sess-spec3", "t-spec3", "Rust Audit Specialist", "audit crate", "",
		threadmgr.SpecialistPreviewInfo{Model: "claude-opus-4-6"},
		nil,
	)
	if approved {
		t.Fatal("expected specialist spawn preview timeout to DENY, not auto-approve")
	}
}

// TestApproveSpecialist_OffAndAutoModesBypass verifies parity with Approve()
// for the operator-controlled Off/Auto modes — S10 changes conditional-mode
// and timeout behavior only, not the global disable/trusted-auto switches.
func TestApproveSpecialist_OffAndAutoModesBypass(t *testing.T) {
	off := threadmgr.NewDelegationPreviewGateWithConfig("off", 2*time.Second)
	if !off.ApproveSpecialist(context.Background(), "s", "t", "Spec", "task", "", threadmgr.SpecialistPreviewInfo{}, nil) {
		t.Error("expected off mode to bypass specialist approval")
	}
	auto := threadmgr.NewDelegationPreviewGateWithConfig("auto", 2*time.Second)
	if !auto.ApproveSpecialist(context.Background(), "s", "t2", "Spec", "task", "", threadmgr.SpecialistPreviewInfo{}, nil) {
		t.Error("expected auto mode to bypass specialist approval")
	}
}

// TestApproveSpecialist_DeniedByAck verifies explicit denial still works
// (not just the timeout path).
func TestApproveSpecialist_DeniedByAck(t *testing.T) {
	preview := threadmgr.NewDelegationPreviewGateWithConfig("manual", 2*time.Second)
	ready := make(chan struct{})
	resultCh := make(chan bool, 1)
	go func() {
		approved := preview.ApproveSpecialist(
			context.Background(), "sess-spec4", "t-spec4", "Rust Audit Specialist", "audit crate", "",
			threadmgr.SpecialistPreviewInfo{Model: "claude-opus-4-6"},
			func(_, _ string, _ map[string]any) { close(ready) },
		)
		resultCh <- approved
	}()
	<-ready
	preview.Ack("sess-spec4", "t-spec4", false)
	select {
	case result := <-resultCh:
		if result {
			t.Error("expected denial after Ack(false)")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for ApproveSpecialist to return")
	}
}
