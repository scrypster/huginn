package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/claudecode/approvals"
)

func TestListApprovalsReturnsPending(t *testing.T) {
	s := &Server{}
	s.approvals = approvals.New(time.Minute)
	defer s.approvals.Close()
	if _, err := s.approvals.Register(approvals.Request{
		AgentName: "codey", ToolName: "Bash", Summary: "ls",
	}); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	s.handleListClaudeApprovals(rr, httptest.NewRequest("GET", "/api/v1/claude/approvals", nil))
	var got struct {
		Approvals []approvals.PendingView `json:"approvals"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unparseable body: %v", err)
	}
	if len(got.Approvals) != 1 || got.Approvals[0].Summary != "ls" {
		t.Fatalf("approvals = %+v, want one entry summarised %q", got.Approvals, "ls")
	}
	if got.Approvals[0].RemainingMS <= 0 {
		t.Fatal("RemainingMS must be positive and computed server-side")
	}
}

func TestListApprovalsNilStoreReturnsEmpty(t *testing.T) {
	s := &Server{}
	s.approvals = nil
	rr := httptest.NewRecorder()
	s.handleListClaudeApprovals(rr, httptest.NewRequest("GET", "/api/v1/claude/approvals", nil))
	var got struct {
		Approvals []approvals.PendingView `json:"approvals"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unparseable body: %v", err)
	}
	if len(got.Approvals) != 0 {
		t.Fatalf("want empty list, got %+v", got.Approvals)
	}
}

func TestDecideDeliversDecision(t *testing.T) {
	s := &Server{}
	s.approvals = approvals.New(2 * time.Second)
	defer s.approvals.Close()
	p, _ := s.approvals.Register(approvals.Request{AgentName: "codey", ToolName: "Bash", Summary: "ls"})

	got := make(chan approvals.Decision, 1)
	go func() { got <- s.approvals.Wait(context.Background(), p) }()

	rr := httptest.NewRecorder()
	s.handleDecideClaudeApproval(rr, httptest.NewRequest("POST", "/api/v1/claude/approve/decide",
		strings.NewReader(`{"id":"`+p.ID+`","decision":"allow"}`)))
	if rr.Code != 200 {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if d := <-got; d != approvals.Allow {
		t.Fatalf("waiter got %v, want Allow", d)
	}
}

func TestDecideUnknownIDIs404(t *testing.T) {
	s := &Server{}
	s.approvals = approvals.New(time.Minute)
	defer s.approvals.Close()
	rr := httptest.NewRecorder()
	s.handleDecideClaudeApproval(rr, httptest.NewRequest("POST", "/api/v1/claude/approve/decide",
		strings.NewReader(`{"id":"missing","decision":"allow"}`)))
	if rr.Code != 404 {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestDecideRejectsUnknownDecisionString(t *testing.T) {
	s := &Server{}
	s.approvals = approvals.New(time.Minute)
	defer s.approvals.Close()
	p, _ := s.approvals.Register(approvals.Request{AgentName: "codey", ToolName: "Bash"})
	rr := httptest.NewRecorder()
	s.handleDecideClaudeApproval(rr, httptest.NewRequest("POST", "/api/v1/claude/approve/decide",
		strings.NewReader(`{"id":"`+p.ID+`","decision":"sudo"}`)))
	if rr.Code != 400 {
		t.Fatalf("status = %d, want 400 for an unrecognised decision", rr.Code)
	}
	if len(s.approvals.List()) != 1 {
		t.Fatal("a bad decision string consumed the pending entry")
	}
}

func TestDecideNilStoreIs503(t *testing.T) {
	s := &Server{}
	s.approvals = nil
	rr := httptest.NewRecorder()
	s.handleDecideClaudeApproval(rr, httptest.NewRequest("POST", "/api/v1/claude/approve/decide",
		strings.NewReader(`{"id":"x","decision":"allow"}`)))
	if rr.Code != 503 {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
}

func TestPromoteClaudeToolAppendsOnce(t *testing.T) {
	s := &Server{}
	saved := [][]string{}
	s.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{{
			Name: "codey", ClaudeAllowedTools: []string{"Read"},
		}}}, nil
	}
	s.agentSaver = func(cfg *agents.AgentsConfig) error {
		saved = append(saved, cfg.Agents[0].ClaudeAllowedTools)
		return nil
	}
	if err := s.promoteClaudeTool("codey", "Bash"); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || len(saved[0]) != 2 || saved[0][1] != "Bash" {
		t.Fatalf("saved = %v, want Read+Bash", saved)
	}
}

func TestPromoteClaudeToolIsIdempotent(t *testing.T) {
	s := &Server{}
	calls := 0
	s.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{{
			Name: "codey", ClaudeAllowedTools: []string{"Bash"},
		}}}, nil
	}
	s.agentSaver = func(cfg *agents.AgentsConfig) error { calls++; return nil }
	if err := s.promoteClaudeTool("codey", "Bash"); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("saver called %d times for an already-allowed tool, want 0", calls)
	}
}
