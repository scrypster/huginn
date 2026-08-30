package server

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/scrypster/huginn/internal/mcp"
)

// Opus-vet finding 2026-08-28: handleUpdateConfig decoded the request body
// into a zero-value config.Config, then wrote `*c = newCfg` over the
// on-disk config. A partial JSON body — e.g. what curl sends when a caller
// only means to flip one field — left every field it didn't name at its Go
// zero value, silently wiping reasoner_model, web_ui, and everything else.
func TestHandleUpdateConfig_PartialBody_DoesNotZeroOmittedFields(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.cfg.ReasonerModel = "deepseek-r1:32b"
	srv.cfg.WebUI.Port = 9999
	srv.cfg.WebUI.Bind = "localhost"
	srv.cfg.ToolsEnabled = false

	req := httptest.NewRequest("PUT", "/api/v1/config", bytes.NewReader([]byte(`{"tools_enabled":true}`)))
	rec := httptest.NewRecorder()
	srv.handleUpdateConfig(rec, req)
	if rec.Code != 200 {
		t.Fatalf("PUT config = %d body=%s", rec.Code, rec.Body.String())
	}

	if !srv.cfg.ToolsEnabled {
		t.Fatal("tools_enabled: partial-body field was not applied")
	}
	if srv.cfg.ReasonerModel != "deepseek-r1:32b" {
		t.Fatalf("reasoner_model zeroed by partial body: got %q, want %q", srv.cfg.ReasonerModel, "deepseek-r1:32b")
	}
	if srv.cfg.WebUI.Port != 9999 {
		t.Fatalf("web_ui.port zeroed by partial body: got %d, want 9999", srv.cfg.WebUI.Port)
	}
	if srv.cfg.WebUI.Bind != "localhost" {
		t.Fatalf("web_ui.bind zeroed by partial body: got %q, want %q", srv.cfg.WebUI.Bind, "localhost")
	}
}

// A partial body untouching mcp_servers must leave the live config's MCP
// server list intact — the merge copy must be a true deep copy, not a
// shallow struct assignment that shares slice storage with s.cfg (which
// json.Unmarshal can then write into in place, mutating the live value out
// from under other readers).
func TestHandleUpdateConfig_PartialBody_PreservesUnrelatedSliceField(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.cfg.MCPServers = []mcp.MCPServerConfig{
		{Name: "filesystem", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem"}},
	}

	req := httptest.NewRequest("PUT", "/api/v1/config", bytes.NewReader([]byte(`{"tools_enabled":true}`)))
	rec := httptest.NewRecorder()
	srv.handleUpdateConfig(rec, req)
	if rec.Code != 200 {
		t.Fatalf("PUT config = %d body=%s", rec.Code, rec.Body.String())
	}

	if len(srv.cfg.MCPServers) != 1 || srv.cfg.MCPServers[0].Name != "filesystem" {
		t.Fatalf("mcp_servers corrupted by partial-body PUT: %+v", srv.cfg.MCPServers)
	}
}
