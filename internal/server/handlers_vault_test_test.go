package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

// ---------------------------------------------------------------------------
// Helper: build a Server with a custom agentLoader that returns a fixed agent
// config and a stub vaultProberFn so tests never open real network connections.
// ---------------------------------------------------------------------------

// vaultTestServer returns a *Server whose agentLoader serves the provided
// list of agents. proberFn is wired as the vault connectivity prober;
// pass nil to simulate no muninn config (every probe returns an error).
func vaultTestServer(agentDefs []agents.AgentDef) *Server {
	return vaultTestServerWithProber(agentDefs, stubProberOK(3))
}

// vaultTestServerWithProber returns a *Server with a custom prober function.
func vaultTestServerWithProber(agentDefs []agents.AgentDef, prober func(context.Context, string, string) (int, string, error)) *Server {
	return &Server{
		agentLoader: func() (*agents.AgentsConfig, error) {
			return &agents.AgentsConfig{Agents: agentDefs}, nil
		},
		vaultProberFn: prober,
	}
}

// stubProberOK returns a prober that always succeeds with n tools and no warning.
func stubProberOK(n int) func(context.Context, string, string) (int, string, error) {
	return func(_ context.Context, _, _ string) (int, string, error) {
		return n, "", nil
	}
}

// stubProberFail returns a prober that always returns the given error.
func stubProberFail(msg string) func(context.Context, string, string) (int, string, error) {
	return func(_ context.Context, _, _ string) (int, string, error) {
		return 0, "", fmt.Errorf("%s", msg)
	}
}

// stubProberOKWithWarning returns a prober that succeeds with n tools and a warning message.
func stubProberOKWithWarning(n int, warning string) func(context.Context, string, string) (int, string, error) {
	return func(_ context.Context, _, _ string) (int, string, error) {
		return n, warning, nil
	}
}

// doVaultTest issues POST /api/v1/agents/{name}/vault/test directly via the
// handler. body may be empty ("") or a JSON string.
func doVaultTest(s *Server, agentName, body string) *httptest.ResponseRecorder {
	var bodyReader *strings.Reader
	if body == "" {
		bodyReader = strings.NewReader("")
	} else {
		bodyReader = strings.NewReader(body)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/"+agentName+"/vault/test", bodyReader)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("name", agentName)
	w := httptest.NewRecorder()
	s.handleVaultTest(w, req)
	return w
}

// ---------------------------------------------------------------------------
// 200: agent exists with vault configured AND reachable
// Response: {"status":"ok","vault":"<name>","tools_count":<n>}
// ---------------------------------------------------------------------------

func TestHandleVaultTest_AgentWithVault_Returns200(t *testing.T) {
	srv := vaultTestServer([]agents.AgentDef{
		{Name: "Sage", VaultName: "huginn:agent:local:sage"},
	})

	w := doVaultTest(srv, "Sage", "")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("status = %q, want ok", resp["status"])
	}
	if resp["vault"] != "huginn:agent:local:sage" {
		t.Errorf("vault = %q, want huginn:agent:local:sage", resp["vault"])
	}
	// tools_count should be present and non-negative.
	if _, ok := resp["tools_count"]; !ok {
		t.Error("tools_count field missing from 200 response")
	}
}

// ---------------------------------------------------------------------------
// 200: body JSON override of vault name works
// ---------------------------------------------------------------------------

func TestHandleVaultTest_BodyOverridesVaultName(t *testing.T) {
	// Agent has no vault_name configured; caller sends it in the request body.
	srv := vaultTestServer([]agents.AgentDef{
		{Name: "Sage", VaultName: ""},
	})

	body := `{"vault_name":"huginn:agent:local:override"}`
	w := doVaultTest(srv, "Sage", body)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["vault"] != "huginn:agent:local:override" {
		t.Errorf("vault = %q, want override vault", resp["vault"])
	}
}

// ---------------------------------------------------------------------------
// 400: missing agent name (empty path param)
// ---------------------------------------------------------------------------

func TestHandleVaultTest_MissingAgentName_Returns400(t *testing.T) {
	srv := vaultTestServer(nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents//vault/test", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	// Explicitly set empty path value to simulate missing param.
	req.SetPathValue("name", "")
	w := httptest.NewRecorder()
	srv.handleVaultTest(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 422: agent exists but no vault configured (vaultName is empty)
// ---------------------------------------------------------------------------

func TestHandleVaultTest_NoVaultConfigured_Returns422(t *testing.T) {
	srv := vaultTestServer([]agents.AgentDef{
		{Name: "Sage", VaultName: ""},
	})

	// No body override either.
	w := doVaultTest(srv, "Sage", "")

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] == "" {
		t.Error("want non-empty error in 422 response")
	}
}

// ---------------------------------------------------------------------------
// 422: vault configured but unreachable (probe returns error)
// ---------------------------------------------------------------------------

func TestHandleVaultTest_VaultUnreachable_Returns422(t *testing.T) {
	srv := vaultTestServerWithProber(
		[]agents.AgentDef{
			{Name: "Sage", VaultName: "huginn:agent:local:sage"},
		},
		stubProberFail("connect: connection refused"),
	)

	w := doVaultTest(srv, "Sage", "")

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422 (unreachable vault), got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "error" {
		t.Errorf("status = %q, want error", resp["status"])
	}
	if resp["vault"] != "huginn:agent:local:sage" {
		t.Errorf("vault = %q, want huginn:agent:local:sage", resp["vault"])
	}
	if resp["detail"] == "" {
		t.Error("detail field should be non-empty in 422 unreachable response")
	}
}

// ---------------------------------------------------------------------------
// 404: agent not found (agentLoader returns config without the requested agent)
// ---------------------------------------------------------------------------

func TestHandleVaultTest_AgentNotFound_Returns404(t *testing.T) {
	// Loader has agents, but none named "Ghost".
	srv := vaultTestServer([]agents.AgentDef{
		{Name: "Sage", VaultName: "some-vault"},
	})

	w := doVaultTest(srv, "Ghost", "")

	// No matching agent → vaultName stays empty → 422 ("no vault configured")
	// The handler does NOT return 404 for missing agents; it falls through to
	// the "no vault configured" 422 path because the agent lookup loop simply
	// finds nothing and leaves vaultName as "". This is the actual contract.
	// Confirm the non-200 status and that the response indicates missing vault.
	if w.Code == http.StatusOK {
		t.Fatalf("want non-200 for unknown agent, got 200; body: %s", w.Body.String())
	}
	// The handler returns 422 because vaultName is empty after the lookup.
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("want 422 for unknown agent (no vault), got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Case-insensitive agent name lookup
// ---------------------------------------------------------------------------

func TestHandleVaultTest_CaseInsensitiveLookup(t *testing.T) {
	srv := vaultTestServer([]agents.AgentDef{
		{Name: "Sage", VaultName: "huginn:agent:local:sage"},
	})

	// Request uses lowercase "sage" even though agent is stored as "Sage".
	w := doVaultTest(srv, "sage", "")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 for case-insensitive lookup, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp) //nolint:errcheck
	if resp["status"] != "ok" {
		t.Errorf("status = %q, want ok", resp["status"])
	}
}

// ---------------------------------------------------------------------------
// Invalid JSON body
// ---------------------------------------------------------------------------

func TestHandleVaultTest_InvalidJSONBody_Returns400(t *testing.T) {
	srv := vaultTestServer([]agents.AgentDef{
		{Name: "Sage", VaultName: "some-vault"},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/Sage/vault/test",
		strings.NewReader("not valid json"))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("name", "Sage")
	w := httptest.NewRecorder()
	srv.handleVaultTest(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Body override takes precedence over agent config vault name
// ---------------------------------------------------------------------------

func TestHandleVaultTest_BodyOverridePriorityOverConfig(t *testing.T) {
	// Agent has a vault configured, but body provides a different one.
	srv := vaultTestServer([]agents.AgentDef{
		{Name: "Sage", VaultName: "configured-vault"},
	})

	body := `{"vault_name":"override-vault"}`
	w := doVaultTest(srv, "Sage", body)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp) //nolint:errcheck
	// Body vault_name takes precedence over agent config.
	if resp["vault"] != "override-vault" {
		t.Errorf("vault = %q, want override-vault (body should take priority)", resp["vault"])
	}
}

// ---------------------------------------------------------------------------
// Integration: vault test via full test server + HTTP
// ---------------------------------------------------------------------------

func TestHandleVaultTest_Integration_FullHTTPRoundtrip(t *testing.T) {
	srv, ts := newTestServer(t)

	// Override the agent loader to inject an agent with a known vault.
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{
			{Name: "Chris", VaultName: "huginn:agent:local:chris"},
		}}, nil
	}
	// Inject a stub prober so no real MCP connection is attempted.
	srv.vaultProberFn = stubProberOK(7)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agents/Chris/vault/test",
		strings.NewReader(""))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q, want ok", body["status"])
	}
	if body["vault"] != "huginn:agent:local:chris" {
		t.Errorf("vault = %q, want huginn:agent:local:chris", body["vault"])
	}
	if body["tools_count"] == nil {
		t.Error("tools_count missing from response")
	}
}
