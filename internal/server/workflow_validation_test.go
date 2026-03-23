package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/scheduler"
)

// ---------------------------------------------------------------------------
// validateWorkflowAgentsAndConnections unit tests
//
// These tests call the method directly (no HTTP round-trip) so we can inject
// precise agent/connection state without running a full HTTP server.
// ---------------------------------------------------------------------------

// buildTestServerForValidation returns a *Server wired with the given agents
// and connections for use with validateWorkflowAgentsAndConnections tests.
func buildTestServerForValidation(agentDefs []agents.AgentDef, conns []connections.Connection) *Server {
	srv := &Server{}
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: agentDefs}, nil
	}
	if conns != nil {
		srv.connStore = &staticConnectionStore{conns: conns}
	}
	return srv
}

// staticConnectionStore is a minimal StoreInterface backed by a fixed slice.
type staticConnectionStore struct {
	conns []connections.Connection
}

func (s *staticConnectionStore) List() ([]connections.Connection, error) {
	return s.conns, nil
}
func (s *staticConnectionStore) ListByProvider(p connections.Provider) ([]connections.Connection, error) {
	var out []connections.Connection
	for _, c := range s.conns {
		if c.Provider == p {
			out = append(out, c)
		}
	}
	return out, nil
}
func (s *staticConnectionStore) Get(id string) (connections.Connection, bool) {
	for _, c := range s.conns {
		if c.ID == id {
			return c, true
		}
	}
	return connections.Connection{}, false
}
func (s *staticConnectionStore) Add(conn connections.Connection) error        { return nil }
func (s *staticConnectionStore) Remove(id string) error                       { return nil }
func (s *staticConnectionStore) UpdateExpiry(id string, t time.Time) error    { return nil }
func (s *staticConnectionStore) SetDefault(id string) error                   { return nil }
func (s *staticConnectionStore) UpdateRefreshError(id, errMsg string) error   { return nil }

// ---------------------------------------------------------------------------
// 1. Valid workflow passes validation (no error)
// ---------------------------------------------------------------------------

func TestValidateWorkflowAgentsAndConnections_ValidWorkflow(t *testing.T) {
	agentDefs := []agents.AgentDef{{Name: "Chris"}}
	conns := []connections.Connection{{ID: "conn-1", Provider: connections.ProviderGitHub}}

	srv := buildTestServerForValidation(agentDefs, conns)

	wf := &scheduler.Workflow{
		Steps: []scheduler.WorkflowStep{
			{Name: "step-a", Agent: "Chris", Connections: map[string]string{"gh": "conn-1"}},
		},
	}

	if err := srv.validateWorkflowAgentsAndConnections(wf); err != nil {
		t.Errorf("want no error for valid workflow, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 2. Unknown agent name → error
// ---------------------------------------------------------------------------

func TestValidateWorkflowAgentsAndConnections_UnknownAgent(t *testing.T) {
	agentDefs := []agents.AgentDef{{Name: "Chris"}}
	srv := buildTestServerForValidation(agentDefs, nil)

	wf := &scheduler.Workflow{
		Steps: []scheduler.WorkflowStep{
			{Name: "step-a", Agent: "Phantom"},
		},
	}

	err := srv.validateWorkflowAgentsAndConnections(wf)
	if err == nil {
		t.Fatal("want error for unknown agent, got nil")
	}
	if !strings.Contains(err.Error(), "Phantom") {
		t.Errorf("error should mention agent name; got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 3. Unknown connection ID → error
// ---------------------------------------------------------------------------

func TestValidateWorkflowAgentsAndConnections_UnknownConnection(t *testing.T) {
	agentDefs := []agents.AgentDef{{Name: "Chris"}}
	conns := []connections.Connection{{ID: "conn-known", Provider: connections.ProviderGitHub}}
	srv := buildTestServerForValidation(agentDefs, conns)

	wf := &scheduler.Workflow{
		Steps: []scheduler.WorkflowStep{
			{
				Name:        "step-a",
				Agent:       "Chris",
				Connections: map[string]string{"gh": "conn-unknown"},
			},
		},
	}

	err := srv.validateWorkflowAgentsAndConnections(wf)
	if err == nil {
		t.Fatal("want error for unknown connection, got nil")
	}
	if !strings.Contains(err.Error(), "conn-unknown") {
		t.Errorf("error should mention connection ID; got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 4. Agent name is case-insensitive (uppercase in step, lowercase in config)
// ---------------------------------------------------------------------------

func TestValidateWorkflowAgentsAndConnections_AgentNameCaseInsensitive(t *testing.T) {
	// Agent stored as "chris" (lowercase), step references "CHRIS" (uppercase).
	agentDefs := []agents.AgentDef{{Name: "chris"}}
	srv := buildTestServerForValidation(agentDefs, nil)

	wf := &scheduler.Workflow{
		Steps: []scheduler.WorkflowStep{
			{Name: "step-a", Agent: "CHRIS"},
		},
	}

	if err := srv.validateWorkflowAgentsAndConnections(wf); err != nil {
		t.Errorf("want no error for case-insensitive agent match, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 5. Empty steps list passes validation (no steps = nothing to check)
// ---------------------------------------------------------------------------

func TestValidateWorkflowAgentsAndConnections_EmptySteps(t *testing.T) {
	srv := buildTestServerForValidation([]agents.AgentDef{{Name: "Chris"}}, nil)

	wf := &scheduler.Workflow{Steps: []scheduler.WorkflowStep{}}

	if err := srv.validateWorkflowAgentsAndConnections(wf); err != nil {
		t.Errorf("want no error for empty steps, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 6. Nil steps list also passes (defensive: loop over nil slice is a no-op)
// ---------------------------------------------------------------------------

func TestValidateWorkflowAgentsAndConnections_NilSteps(t *testing.T) {
	srv := buildTestServerForValidation(nil, nil)

	wf := &scheduler.Workflow{Steps: nil}

	if err := srv.validateWorkflowAgentsAndConnections(wf); err != nil {
		t.Errorf("want no error for nil steps, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 7. Agent loader nil → check skipped (graceful degradation)
// ---------------------------------------------------------------------------

func TestValidateWorkflowAgentsAndConnections_NilAgentLoader_Skips(t *testing.T) {
	// No agentLoader and no agents.LoadAgents file → cfg will likely err.
	// The method skips the agent check when cfg is nil/err, so validation
	// must pass even for an "unknown" agent name.
	srv := &Server{
		agentLoader: func() (*agents.AgentsConfig, error) {
			return nil, nil // simulates graceful degradation
		},
	}

	wf := &scheduler.Workflow{
		Steps: []scheduler.WorkflowStep{
			{Name: "step-a", Agent: "AnyAgentName"},
		},
	}

	// When cfg is nil, knownAgents is never populated → check is skipped.
	if err := srv.validateWorkflowAgentsAndConnections(wf); err != nil {
		t.Errorf("want no error when agent loader returns nil config, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 8. Connection store nil → check skipped (graceful degradation)
// ---------------------------------------------------------------------------

func TestValidateWorkflowAgentsAndConnections_NilConnStore_Skips(t *testing.T) {
	agentDefs := []agents.AgentDef{{Name: "Chris"}}
	// connStore is nil — connection check must be skipped.
	srv := buildTestServerForValidation(agentDefs, nil)
	srv.connStore = nil

	wf := &scheduler.Workflow{
		Steps: []scheduler.WorkflowStep{
			{Name: "step-a", Agent: "Chris", Connections: map[string]string{"gh": "nonexistent-conn"}},
		},
	}

	// With nil connStore, knownConnIDs is never populated → check is skipped.
	if err := srv.validateWorkflowAgentsAndConnections(wf); err != nil {
		t.Errorf("want no error when connStore is nil, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 9. Integration via HTTP: unknown agent → 422 response
// ---------------------------------------------------------------------------

func TestHandleCreateWorkflow_UnknownAgent_Returns422(t *testing.T) {
	srv, ts := newTestServer(t)

	// agentLoader returns only "Chris"; step uses "Phantom" which doesn't exist.
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{
			{Name: "Chris"},
		}}, nil
	}

	payload := `{
		"name": "wf-unknown-agent",
		"enabled": false,
		"schedule": "0 9 * * 1-5",
		"steps": [
			{"name": "step-a", "agent": "Phantom", "position": 0}
		]
	}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("want 422 for unknown agent, got %d", resp.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body) //nolint:errcheck
	errMsg, _ := body["error"].(string)
	if errMsg == "" {
		t.Error("want non-empty error message in 422 response")
	}
}

// ---------------------------------------------------------------------------
// 10. Integration via HTTP: unknown connection → 422 response
// ---------------------------------------------------------------------------

func TestHandleCreateWorkflow_UnknownConnection_Returns422(t *testing.T) {
	srv, ts := newTestServer(t)

	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{
			{Name: "Chris"},
		}}, nil
	}
	// Inject a conn store with a known connection; step uses a different ID.
	srv.mu.Lock()
	srv.connStore = &staticConnectionStore{
		conns: []connections.Connection{
			{ID: "known-conn", Provider: connections.ProviderGitHub},
		},
	}
	srv.mu.Unlock()

	payload := `{
		"name": "wf-unknown-conn",
		"enabled": false,
		"schedule": "0 9 * * 1-5",
		"steps": [
			{
				"name": "step-a",
				"agent": "Chris",
				"position": 0,
				"connections": {"gh": "nonexistent-conn"}
			}
		]
	}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("want 422 for unknown connection, got %d", resp.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body) //nolint:errcheck
	errMsg, _ := body["error"].(string)
	if errMsg == "" {
		t.Error("want non-empty error message in 422 response")
	}
}

// ---------------------------------------------------------------------------
// 11. Integration: agent name case-insensitive match via HTTP
// ---------------------------------------------------------------------------

func TestHandleCreateWorkflow_AgentCaseInsensitive_Passes(t *testing.T) {
	srv, ts := newTestServer(t)

	// Agent config has "Chris"; workflow uses "chris" (lowercase).
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{
			{Name: "Chris"},
		}}, nil
	}

	payload := `{
		"name": "wf-case-insensitive",
		"enabled": false,
		"schedule": "0 9 * * 1-5",
		"steps": [
			{"name": "step-a", "agent": "chris", "position": 0}
		]
	}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200 for case-insensitive agent match, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// 12. Integration: empty steps list passes via HTTP
// ---------------------------------------------------------------------------

func TestHandleCreateWorkflow_EmptySteps_Passes(t *testing.T) {
	_, ts := newTestServer(t)

	payload := `{"name":"empty-steps-wf","enabled":false,"schedule":"0 9 * * 1-5","steps":[]}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200 for empty steps, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Circular step dependency detection tests (validateWorkflow unit tests).
// ---------------------------------------------------------------------------

// TestValidateWorkflow_LinearDepsPass verifies that a linear A→B→C chain passes.
func TestValidateWorkflow_LinearDepsPass(t *testing.T) {
	wf := &scheduler.Workflow{
		Steps: []scheduler.WorkflowStep{
			{Name: "A", Prompt: "step A", Position: 1},
			{Name: "B", Prompt: "step B", Position: 2,
				Inputs: []scheduler.StepInput{{As: "prev", FromStep: "A"}}},
			{Name: "C", Prompt: "step C", Position: 3,
				Inputs: []scheduler.StepInput{{As: "prev", FromStep: "B"}}},
		},
	}
	if err := validateWorkflow(wf); err != nil {
		t.Errorf("expected no error for linear A→B→C, got: %v", err)
	}
}

// TestValidateWorkflow_CircularDepsRejected verifies that A→B→A returns an error.
func TestValidateWorkflow_CircularDepsRejected(t *testing.T) {
	wf := &scheduler.Workflow{
		Steps: []scheduler.WorkflowStep{
			{Name: "A", Prompt: "step A", Position: 1,
				Inputs: []scheduler.StepInput{{As: "b_out", FromStep: "B"}}},
			{Name: "B", Prompt: "step B", Position: 2,
				Inputs: []scheduler.StepInput{{As: "a_out", FromStep: "A"}}},
		},
	}
	err := validateWorkflow(wf)
	if err == nil {
		t.Fatal("expected error for circular dependency A→B→A, got nil")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Errorf("expected 'circular' in error, got: %v", err)
	}
}

// TestValidateWorkflow_SelfReferenceRejectedByCycleCheck verifies that A→A
// is caught. Note: the existing self-reference rule fires first, which is fine.
func TestValidateWorkflow_SelfReferenceRejectedByCycleCheck(t *testing.T) {
	wf := &scheduler.Workflow{
		Steps: []scheduler.WorkflowStep{
			{Name: "A", Prompt: "step A", Position: 1,
				Inputs: []scheduler.StepInput{{As: "own", FromStep: "A"}}},
		},
	}
	err := validateWorkflow(wf)
	if err == nil {
		t.Fatal("expected error for self-reference A→A, got nil")
	}
}

// TestValidateWorkflow_CircularWorkflowHTTP_Returns422 verifies that the full
// HTTP handler rejects a circular workflow with 422.
func TestValidateWorkflow_CircularWorkflowHTTP_Returns422(t *testing.T) {
	_, ts := newTestServer(t)

	// A depends on B, B depends on A → cycle.
	payload := `{
		"name": "Circular WF",
		"enabled": false,
		"steps": [
			{"name":"A","prompt":"do A","position":1,"inputs":[{"alias":"b","from_step":"B"}]},
			{"name":"B","prompt":"do B","position":2,"inputs":[{"alias":"a","from_step":"A"}]}
		]
	}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for circular dependency, got %d", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body["error"], "circular") {
		t.Errorf("expected 'circular' in error, got: %s", body["error"])
	}
}
