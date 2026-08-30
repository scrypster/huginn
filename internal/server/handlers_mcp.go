package server

import (
	"net/http"
	"reflect"

	"github.com/scrypster/huginn/internal/mcp"
)

// mcpServersEqual reports whether two MCP server config slices are
// equivalent, order-insensitive by name. Used by handleUpdateConfig to
// decide whether a config save (e.g. the Settings → Browser toggle) needs a
// restart to take effect — ServerManager.StartAll only runs once at boot.
func mcpServersEqual(a, b []mcp.MCPServerConfig) bool {
	if len(a) != len(b) {
		return false
	}
	byName := make(map[string]mcp.MCPServerConfig, len(a))
	for _, cfg := range a {
		byName[cfg.Name] = cfg
	}
	for _, cfg := range b {
		prev, ok := byName[cfg.Name]
		if !ok || !reflect.DeepEqual(prev, cfg) {
			return false
		}
	}
	return true
}

// handleMCPStatus reports live connection state for every configured MCP
// server (e.g. the "playwright" browser-automation server), for the web
// Settings → Browser toggle to show running/unavailable/not-installed
// instead of leaving the user guessing after a save.
//
// Returns an empty list — never an error — when no MCP manager is wired
// (no mcp_servers configured, or running in a mode that doesn't start one).
func (s *Server) handleMCPStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	mgr := s.mcpMgr
	s.mu.Unlock()

	if mgr == nil {
		jsonOK(w, map[string]any{"servers": []any{}})
		return
	}
	jsonOK(w, map[string]any{"servers": mgr.Status()})
}
