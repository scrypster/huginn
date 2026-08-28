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
	s.agentSaver = func(a agents.AgentDef) error {
		saved = append(saved, a.ClaudeAllowedTools)
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
	s.agentSaver = func(a agents.AgentDef) error { calls++; return nil }
	if err := s.promoteClaudeTool("codey", "Bash"); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("saver called %d times for an already-allowed tool, want 0", calls)
	}
}

func TestDecideAllowToolPromotesTheRightAgentAndTool(t *testing.T) {
	s := &Server{approvals: approvals.New(2 * time.Second)}
	defer s.approvals.Close()

	var savedAgent, savedTool string
	s.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{
			{Name: "other", ClaudeAllowedTools: []string{"Read"}},
			{Name: "codey", ClaudeAllowedTools: []string{"Read"}},
		}}, nil
	}
	// The saver now receives exactly the agent being promoted, so this also
	// asserts the RIGHT one was picked out of the two in the config: "other"
	// comes first, so an implementation that promotes cfg.Agents[0] fails here.
	s.agentSaver = func(a agents.AgentDef) error {
		if len(a.ClaudeAllowedTools) == 2 {
			savedAgent = a.Name
			savedTool = a.ClaudeAllowedTools[1]
		}
		return nil
	}

	p, err := s.approvals.Register(approvals.Request{
		AgentName: "codey", ToolName: "Bash", Summary: "ls",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan approvals.Decision, 1)
	go func() { got <- s.approvals.Wait(context.Background(), p) }()

	rr := httptest.NewRecorder()
	s.handleDecideClaudeApproval(rr, httptest.NewRequest("POST", "/api/v1/claude/approve/decide",
		strings.NewReader(`{"id":"`+p.ID+`","decision":"allow_tool"}`)))
	if rr.Code != 200 {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if d := <-got; d != approvals.AllowTool {
		t.Fatalf("waiter got %v, want AllowTool", d)
	}
	// The agent and tool must come from the PENDING ENTRY, not from a default
	// or an empty string. An implementation that promoted nothing, or promoted
	// the first agent in the config, fails here.
	if savedAgent != "codey" {
		t.Fatalf("promoted agent = %q, want codey", savedAgent)
	}
	if savedTool != "Bash" {
		t.Fatalf("promoted tool = %q, want Bash", savedTool)
	}
}

// TestPromoteClaudeToolSavesOnlyThePromotedAgent is the blast-radius test.
//
// Promotion used to call agents.SaveAgents(cfg), which loops over EVERY agent
// calling SaveAgent — bumping every agent's Version and rewriting every
// <name>.yaml. Two consequences: an unrelated agent open in the config UI got
// a spurious 409 version conflict on its next save, and a user whose agents
// live in legacy .json files silently gained .yaml files that then take
// precedence, so the hand-edit undo the docs promise points at a file that is
// no longer read. Promotion must touch exactly one agent, like
// handleUpdateAgent does.
func TestPromoteClaudeToolSavesOnlyThePromotedAgent(t *testing.T) {
	s := &Server{}
	var saved []agents.AgentDef
	s.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{
			{Name: "other", ClaudeAllowedTools: []string{"Read"}},
			{Name: "codey", ClaudeAllowedTools: []string{"Read"}},
		}}, nil
	}
	s.agentSaver = func(a agents.AgentDef) error {
		saved = append(saved, a)
		return nil
	}
	if err := s.promoteClaudeTool("codey", "Bash"); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 {
		t.Fatalf("saved %d agents, want exactly 1 — promotion must not rewrite "+
			"every agent file", len(saved))
	}
	if saved[0].Name != "codey" {
		t.Fatalf("saved agent = %q, want codey", saved[0].Name)
	}
	if got := saved[0].ClaudeAllowedTools; len(got) != 2 || got[1] != "Bash" {
		t.Fatalf("saved ClaudeAllowedTools = %v, want Read+Bash", got)
	}
}

// TestPromoteClaudeToolRefreshesTheLiveRegistry pins the other half of the
// sibling's behaviour. handleUpdateAgent calls notifyAgentsChanged and
// broadcasts agent_changed after a save; promotion did neither, so the
// in-memory agent registry and any open agent-config UI went stale — the tool
// stayed gated in the running process even though the file said otherwise.
func TestPromoteClaudeToolRefreshesTheLiveRegistry(t *testing.T) {
	hub := newWSHub()
	go hub.run()
	client := &wsClient{send: make(chan WSMessage, 8), ctx: context.Background()}
	hub.registerWithSession(client, "")

	s := &Server{wsHub: hub}
	var notified int
	s.SetOnAgentsChanged(func() { notified++ })
	s.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{
			{Name: "codey", ClaudeAllowedTools: []string{"Read"}},
		}}, nil
	}
	s.agentSaver = func(a agents.AgentDef) error { return nil }

	if err := s.promoteClaudeTool("codey", "Bash"); err != nil {
		t.Fatal(err)
	}
	if notified != 1 {
		t.Fatalf("onAgentsChanged fired %d times, want 1 — the live registry still "+
			"gates a tool the config now allows", notified)
	}
	var sawAgentChanged bool
	for {
		select {
		case m := <-client.send:
			if m.Type == "agent_changed" {
				sawAgentChanged = true
				if name, _ := m.Payload["name"].(string); name != "codey" {
					t.Fatalf("agent_changed named %q, want codey", name)
				}
			}
			continue
		case <-time.After(200 * time.Millisecond):
		}
		break
	}
	if !sawAgentChanged {
		t.Fatal("no agent_changed broadcast after promotion; an open agent-config UI " +
			"never learns the tool was permanently allowed")
	}
}

// TestPromoteClaudeToolDoesNotNotifyWhenNothingChanged: an already-allowed
// tool writes nothing, so it must not spuriously reload the registry either.
func TestPromoteClaudeToolDoesNotNotifyWhenNothingChanged(t *testing.T) {
	s := &Server{}
	var notified int
	s.SetOnAgentsChanged(func() { notified++ })
	s.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{
			{Name: "codey", ClaudeAllowedTools: []string{"Bash"}},
		}}, nil
	}
	s.agentSaver = func(a agents.AgentDef) error {
		t.Fatal("saver called for an already-allowed tool")
		return nil
	}
	if err := s.promoteClaudeTool("codey", "Bash"); err != nil {
		t.Fatal(err)
	}
	if notified != 0 {
		t.Fatalf("onAgentsChanged fired %d times for a no-op promotion, want 0", notified)
	}
}
