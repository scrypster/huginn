package threadmgr

import (
	"errors"
	"testing"
	"time"
)

func TestProposalRegistry_RequireToken_BlocksHighRiskWithoutToken(t *testing.T) {
	reg := NewProposalRegistry()
	err := reg.RequireToken("", TokenRequirement{
		HighRisk:  true,
		SessionID: "sess-1",
		ThreadID:  "thread-1",
		Provider:  "github",
		Action:    "delete_issue",
	})
	if !errors.Is(err, ErrApprovalTokenRequired) {
		t.Fatalf("expected ErrApprovalTokenRequired, got %v", err)
	}
}

func TestProposalRegistry_RequireToken_BlocksExpiredToken(t *testing.T) {
	reg := NewProposalRegistry()
	now := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	reg.now = func() time.Time { return now }

	proposal, err := reg.Submit(ProposalRequest{
		SessionID: "sess-1",
		ThreadID:  "thread-1",
		AgentID:   "SubAgent",
		Provider:  "github",
		Action:    "delete_issue",
		Risk:      RiskCritical,
	})
	if err != nil {
		t.Fatalf("submit proposal: %v", err)
	}
	token, err := reg.Approve(proposal.ID, "LeadAgent", 5*time.Minute)
	if err != nil {
		t.Fatalf("approve proposal: %v", err)
	}

	now = now.Add(6 * time.Minute)
	err = reg.RequireToken(token.Token, TokenRequirement{
		HighRisk:  true,
		SessionID: "sess-1",
		ThreadID:  "thread-1",
		Provider:  "github",
		Action:    "delete_issue",
	})
	if !errors.Is(err, ErrApprovalTokenExpired) {
		t.Fatalf("expected ErrApprovalTokenExpired, got %v", err)
	}
}

func TestProposalRegistry_RequireToken_BlocksOutOfScopeToken(t *testing.T) {
	reg := NewProposalRegistry()
	proposal, err := reg.Submit(ProposalRequest{
		SessionID: "sess-1",
		ThreadID:  "thread-1",
		AgentID:   "SubAgent",
		Provider:  "github",
		Action:    "delete_issue",
		Risk:      RiskCritical,
	})
	if err != nil {
		t.Fatalf("submit proposal: %v", err)
	}
	token, err := reg.Approve(proposal.ID, "LeadAgent", 10*time.Minute)
	if err != nil {
		t.Fatalf("approve proposal: %v", err)
	}

	err = reg.RequireToken(token.Token, TokenRequirement{
		HighRisk:  true,
		SessionID: "sess-1",
		ThreadID:  "thread-1",
		Provider:  "sendgrid",
		Action:    "send_email",
	})
	if !errors.Is(err, ErrApprovalTokenScopeMismatch) {
		t.Fatalf("expected ErrApprovalTokenScopeMismatch, got %v", err)
	}
}
