package threadmgr

import (
	"errors"
	"testing"
)

// TestCreate_SpecialistFixedCompany_Allowed verifies that an ephemeral
// specialist — never seated in any CompanyGate roster — is accepted into a
// company-scoped space when its company was fixed at spawn time via
// SetSpecialistCompany, and rejected once that fixed company no longer
// matches the target space's company.
func TestCreate_SpecialistFixedCompany_Allowed(t *testing.T) {
	tm := New()
	tm.SetMembershipChecker(&stubChecker{members: []string{"Winston"}})
	tm.SetCompanyGate(&stubCompanyGate{companyID: "co-a", seated: []string{"Winston"}})
	tm.SetSpecialistCompany("Rust Audit Specialist", "co-a")

	th, err := tm.Create(CreateParams{
		SessionID: "sess-a",
		AgentID:   "Rust Audit Specialist",
		Task:      "audit the crypto crate",
		SpaceID:   "space-a",
	})
	if err != nil {
		t.Fatalf("specialist fixed to co-a must be allowed in a co-a space, got: %v", err)
	}
	if th.AgentID != "Rust Audit Specialist" {
		t.Errorf("agent = %q, want Rust Audit Specialist", th.AgentID)
	}
}

func TestCreate_SpecialistFixedCompany_WrongCompanyRejected(t *testing.T) {
	tm := New()
	tm.SetMembershipChecker(&stubChecker{members: []string{"Winston"}})
	tm.SetCompanyGate(&stubCompanyGate{companyID: "co-b", seated: []string{"Winston"}})
	tm.SetSpecialistCompany("Rust Audit Specialist", "co-a")

	_, err := tm.Create(CreateParams{
		SessionID: "sess-b",
		AgentID:   "Rust Audit Specialist",
		Task:      "cross-company work",
		SpaceID:   "space-b",
	})
	if err == nil {
		t.Fatal("expected specialist fixed to co-a to be rejected in a co-b space")
	}
	if !errors.Is(err, ErrAgentNotInCompany) {
		t.Errorf("expected ErrAgentNotInCompany, got: %v", err)
	}
}

// TestCreate_SpecialistDeskLevel_Allowed verifies that a specialist not
// present on any space roster (desk-level, no company gate configured) is
// still accepted once SetSpecialistCompany has recorded it as a known
// specialist — CompanyGate.Create must accept overlay agents structurally,
// not just via the company-scoped branch.
func TestCreate_SpecialistDeskLevel_Allowed(t *testing.T) {
	tm := New()
	tm.SetMembershipChecker(&stubChecker{members: []string{"Winston"}})
	// No CompanyGate wired: desk-only roster check would normally reject
	// anyone not in members.
	tm.SetSpecialistCompany("Go Specialist", "")

	th, err := tm.Create(CreateParams{
		SessionID: "sess-desk",
		AgentID:   "Go Specialist",
		Task:      "desk-level one-off",
		SpaceID:   "desk-space",
	})
	if err != nil {
		t.Fatalf("desk-level specialist must be accepted, got: %v", err)
	}
	if th.AgentID != "Go Specialist" {
		t.Errorf("agent = %q, want Go Specialist", th.AgentID)
	}
}

func TestClearSpecialistCompany_RevokesAccess(t *testing.T) {
	tm := New()
	tm.SetMembershipChecker(&stubChecker{members: []string{"Winston"}})
	tm.SetCompanyGate(&stubCompanyGate{companyID: "co-a", seated: []string{"Winston"}})
	tm.SetSpecialistCompany("Rust Audit Specialist", "co-a")
	tm.ClearSpecialistCompany("Rust Audit Specialist")

	_, err := tm.Create(CreateParams{
		SessionID: "sess-a",
		AgentID:   "Rust Audit Specialist",
		Task:      "after eviction",
		SpaceID:   "space-a",
	})
	if err == nil {
		t.Fatal("expected rejection: specialist no longer known after ClearSpecialistCompany")
	}
	if !errors.Is(err, ErrAgentNotInCompany) {
		t.Errorf("expected ErrAgentNotInCompany after clearing, got: %v", err)
	}
}

// Durable specialist attribution: the pill/model must survive the overlay
// eviction that fires on terminal status (Opus vet 2026-08-29). The Thread
// carries Specialist/SpecialistModel independent of the ephemeral registry.
func TestCreate_DurableSpecialistMarker(t *testing.T) {
	tm := New()
	th, err := tm.Create(CreateParams{
		SessionID:       "s1",
		AgentID:         "Rust Audit Specialist",
		Task:            "audit",
		Specialist:      true,
		SpecialistModel: "claude-opus-4-6",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !th.Specialist || th.SpecialistModel != "claude-opus-4-6" {
		t.Fatalf("durable specialist fields not set: %+v", th)
	}
	got, _ := tm.Get(th.ID)
	if !got.Specialist || got.SpecialistModel != "claude-opus-4-6" {
		t.Fatalf("durable fields not retrievable post-create: %+v", got)
	}
}
