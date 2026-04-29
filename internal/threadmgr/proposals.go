package threadmgr

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ActionRisk captures how sensitive a proposed action is.
type ActionRisk string

const (
	RiskSafe     ActionRisk = "safe"
	RiskElevated ActionRisk = "elevated"
	RiskCritical ActionRisk = "critical"
)

// ProposalStatus tracks lifecycle for a proposed high-risk action.
type ProposalStatus string

const (
	ProposalPending  ProposalStatus = "pending"
	ProposalApproved ProposalStatus = "approved"
	ProposalDenied   ProposalStatus = "denied"
)

var (
	// ErrApprovalTokenRequired is returned when a high-risk action is attempted
	// without an approval token.
	ErrApprovalTokenRequired = errors.New("threadmgr: approval token required for high-risk action")
	// ErrApprovalTokenExpired is returned when an approval token has expired.
	ErrApprovalTokenExpired = errors.New("threadmgr: approval token expired")
	// ErrApprovalTokenScopeMismatch is returned when a token is used outside the
	// approved action/provider/session/thread scope.
	ErrApprovalTokenScopeMismatch = errors.New("threadmgr: approval token scope mismatch")
	// ErrProposalNotFound is returned when a proposal ID does not exist.
	ErrProposalNotFound = errors.New("threadmgr: proposal not found")
)

// ProposalRequest is a sub-agent request for lead approval.
type ProposalRequest struct {
	SessionID     string
	ThreadID      string
	AgentID       string
	Provider      string
	Action        string
	Risk          ActionRisk
	Justification string
}

// ActionProposal is the persisted request/decision envelope.
type ActionProposal struct {
	ID            string
	SessionID     string
	ThreadID      string
	AgentID       string
	Provider      string
	Action        string
	Risk          ActionRisk
	Justification string
	Status        ProposalStatus
	CreatedAt     time.Time
	DecidedAt     *time.Time
	DecidedBy     string
}

// ApprovalToken is a scoped, time-bounded lead approval artifact.
type ApprovalToken struct {
	Token      string
	ProposalID string
	SessionID  string
	ThreadID   string
	Provider   string
	Action     string
	IssuedAt   time.Time
	ExpiresAt  time.Time
}

// TokenRequirement defines the scope expected for a risky action.
type TokenRequirement struct {
	HighRisk  bool
	SessionID string
	ThreadID  string
	Provider  string
	Action    string
}

// ProposalRegistry stores action proposals and scoped approval tokens.
type ProposalRegistry struct {
	mu        sync.RWMutex
	proposals map[string]ActionProposal
	tokens    map[string]ApprovalToken
	now       func() time.Time
}

// NewProposalRegistry returns an empty in-memory registry.
func NewProposalRegistry() *ProposalRegistry {
	return &ProposalRegistry{
		proposals: map[string]ActionProposal{},
		tokens:    map[string]ApprovalToken{},
		now:       time.Now,
	}
}

// Submit records a proposed action awaiting lead approval.
func (r *ProposalRegistry) Submit(req ProposalRequest) (ActionProposal, error) {
	if r == nil {
		return ActionProposal{}, fmt.Errorf("proposal registry not configured")
	}
	if strings.TrimSpace(req.SessionID) == "" {
		return ActionProposal{}, fmt.Errorf("session_id is required")
	}
	if strings.TrimSpace(req.ThreadID) == "" {
		return ActionProposal{}, fmt.Errorf("thread_id is required")
	}
	if strings.TrimSpace(req.Action) == "" {
		return ActionProposal{}, fmt.Errorf("action is required")
	}
	if req.Risk == "" {
		req.Risk = RiskElevated
	}
	req.Provider = strings.ToLower(strings.TrimSpace(req.Provider))
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))

	p := ActionProposal{
		ID:            newID(),
		SessionID:     req.SessionID,
		ThreadID:      req.ThreadID,
		AgentID:       req.AgentID,
		Provider:      req.Provider,
		Action:        req.Action,
		Risk:          req.Risk,
		Justification: strings.TrimSpace(req.Justification),
		Status:        ProposalPending,
		CreatedAt:     r.now(),
	}

	r.mu.Lock()
	r.proposals[p.ID] = p
	r.mu.Unlock()
	return p, nil
}

// Approve marks a proposal approved and issues a scoped token.
func (r *ProposalRegistry) Approve(proposalID, approver string, ttl time.Duration) (ApprovalToken, error) {
	if r == nil {
		return ApprovalToken{}, fmt.Errorf("proposal registry not configured")
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	now := r.now()

	r.mu.Lock()
	defer r.mu.Unlock()

	p, ok := r.proposals[proposalID]
	if !ok {
		return ApprovalToken{}, ErrProposalNotFound
	}
	decidedAt := now
	p.Status = ProposalApproved
	p.DecidedAt = &decidedAt
	p.DecidedBy = strings.TrimSpace(approver)
	r.proposals[proposalID] = p

	tok := ApprovalToken{
		Token:      "approval_" + newID(),
		ProposalID: p.ID,
		SessionID:  p.SessionID,
		ThreadID:   p.ThreadID,
		Provider:   p.Provider,
		Action:     p.Action,
		IssuedAt:   now,
		ExpiresAt:  now.Add(ttl),
	}
	r.tokens[tok.Token] = tok
	return tok, nil
}

// RequireToken validates the token for a high-risk action requirement.
func (r *ProposalRegistry) RequireToken(token string, req TokenRequirement) error {
	if !req.HighRisk {
		return nil
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrApprovalTokenRequired
	}
	if r == nil {
		return ErrApprovalTokenRequired
	}

	r.mu.RLock()
	tok, ok := r.tokens[token]
	now := r.now()
	r.mu.RUnlock()
	if !ok {
		return ErrApprovalTokenRequired
	}
	if now.After(tok.ExpiresAt) {
		return ErrApprovalTokenExpired
	}
	if req.SessionID != "" && tok.SessionID != req.SessionID {
		return ErrApprovalTokenScopeMismatch
	}
	if req.ThreadID != "" && tok.ThreadID != req.ThreadID {
		return ErrApprovalTokenScopeMismatch
	}
	if req.Provider != "" && !strings.EqualFold(tok.Provider, req.Provider) {
		return ErrApprovalTokenScopeMismatch
	}
	if req.Action != "" && !strings.EqualFold(tok.Action, req.Action) {
		return ErrApprovalTokenScopeMismatch
	}
	return nil
}
