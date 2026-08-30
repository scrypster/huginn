package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/mcp"
)

func TestMcpServersEqual(t *testing.T) {
	a := []mcp.MCPServerConfig{{Name: "playwright", Command: "cmd-a"}}
	b := []mcp.MCPServerConfig{{Name: "playwright", Command: "cmd-a"}}
	if !mcpServersEqual(a, b) {
		t.Error("expected identical configs to be equal")
	}
	if mcpServersEqual(a, nil) {
		t.Error("expected different lengths to be unequal")
	}
	c := []mcp.MCPServerConfig{{Name: "playwright", Command: "cmd-b"}}
	if mcpServersEqual(a, c) {
		t.Error("expected a changed command to be unequal")
	}
	d := []mcp.MCPServerConfig{{Name: "other", Command: "cmd-a"}}
	if mcpServersEqual(a, d) {
		t.Error("expected a different server name to be unequal")
	}
}

// TestHandleUpdateConfig_MCPServersChangeRequiresRestart verifies that
// adding an MCP server entry (e.g. the Settings → Browser toggle turning
// on) is flagged as requiring a restart. ServerManager.StartAll only runs
// once at boot — without this, the toggle would silently do nothing until
// the operator happened to restart huginn for an unrelated reason.
func TestHandleUpdateConfig_MCPServersChangeRequiresRestart(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.cfg.MCPServers = nil

	body := `{"mcp_servers":[{"name":"playwright","transport":"stdio","command":"/tmp/playwright-mcp"}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleUpdateConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Saved           bool `json:"saved"`
		RequiresRestart bool `json:"requires_restart"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Saved {
		t.Error("expected saved = true")
	}
	if !resp.RequiresRestart {
		t.Error("expected requires_restart = true when mcp_servers changes")
	}
}

// TestHandleMCPStatus_NoManagerReturnsEmptyList verifies that when no MCP
// manager is wired (no mcp_servers configured), the status endpoint returns
// an empty list rather than an error — "no browser server configured" is the
// default, unconfigured state, not a failure.
func TestHandleMCPStatus_NoManagerReturnsEmptyList(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/mcp/status", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Servers []mcp.ServerStatus `json:"servers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Servers) != 0 {
		t.Errorf("expected empty servers list, got %d", len(body.Servers))
	}
}

// TestHandleMCPStatus_ReportsConnectedServer verifies that once an MCP
// manager is wired via SetMCPManager, the status endpoint surfaces its
// per-server connection state (used by the web Settings → Browser toggle).
func TestHandleMCPStatus_ReportsConnectedServer(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	mgr := mcp.NewServerManager([]mcp.MCPServerConfig{{Name: "playwright", Command: "playwright-mcp-not-on-path"}})
	srv.SetMCPManager(mgr)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/mcp/status", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Servers []mcp.ServerStatus `json:"servers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(body.Servers))
	}
	if body.Servers[0].Name != "playwright" {
		t.Errorf("Name = %q, want %q", body.Servers[0].Name, "playwright")
	}
	if body.Servers[0].Connected {
		t.Error("expected Connected = false — StartAll was never called (no factory ran)")
	}
	if body.Servers[0].BinaryFound {
		t.Error("expected BinaryFound = false for a command not present on $PATH")
	}
	if body.Servers[0].InstallHint == "" {
		t.Error("expected an actionable InstallHint for the known 'playwright' server when its binary is missing")
	}
}
