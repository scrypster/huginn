// internal/server/handlers_memory.go
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/scrypster/huginn/internal/mcp"
	"github.com/scrypster/huginn/internal/memory"
)

// handleMemoryReplicationStatus returns replication queue counts from SQLite.
// GET /api/v1/memory/replication-status
// Response: {"pending":N,"failed":N,"dead":N,"connected":bool}
func (s *Server) handleMemoryReplicationStatus(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		jsonOK(w, map[string]any{
			"pending":   0,
			"failed":    0,
			"dead":      0,
			"connected": false,
		})
		return
	}

	rows, err := s.db.Read().QueryContext(
		context.Background(),
		`SELECT status, COUNT(*) FROM memory_replication_queue GROUP BY status`,
	)
	if err != nil {
		jsonOK(w, map[string]any{
			"pending":   0,
			"failed":    0,
			"dead":      0,
			"connected": true,
		})
		return
	}
	defer rows.Close()

	result := map[string]int{
		"pending": 0,
		"failed":  0,
		"dead":    0,
	}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err == nil {
			if _, ok := result[status]; ok {
				result[status] = count
			}
		}
	}

	jsonOK(w, map[string]any{
		"pending":   result["pending"],
		"failed":    result["failed"],
		"dead":      result["dead"],
		"connected": true,
	})
}

// allowedMuninnTools is the whitelist of tools the browser may call via the proxy.
// Only read + user-initiated write tools are permitted; no autonomous write tools.
var allowedMuninnTools = map[string]bool{
	"muninn_recall":         true,
	"muninn_read":           true,
	"muninn_find_by_entity": true,
	"muninn_entities":       true,
	"muninn_forget":         true,
}

// handleMuninnTool proxies a MuninnDB tool call from the browser to MuninnDB MCP.
// POST /api/v1/muninn/tool
// Body: {"vault":"huginn:agent:user:alice","tool":"muninn_recall","args":{"context":"..."}}
// Response: {"result": <raw MCP tool response>} or {"error":"..."}
func (s *Server) handleMuninnTool(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Vault string         `json:"vault"`
		Tool  string         `json:"tool"`
		Args  map[string]any `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Vault == "" || req.Tool == "" {
		jsonError(w, http.StatusBadRequest, "vault and tool are required")
		return
	}
	if !allowedMuninnTools[req.Tool] {
		jsonError(w, http.StatusForbidden, "tool not permitted: "+req.Tool)
		return
	}

	cfg, err := memory.LoadGlobalConfig(s.muninnCfgPath)
	if err != nil || cfg.Endpoint == "" {
		jsonError(w, http.StatusServiceUnavailable, "MuninnDB not configured")
		return
	}

	token, tokenErr := memory.MCPTokenFor(cfg, req.Vault)
	if tokenErr != nil || token == "" {
		jsonError(w, http.StatusNotFound, "no token for vault: "+req.Vault)
		return
	}

	mcpURL, urlErr := memory.MCPURLFromEndpoint(cfg.Endpoint)
	if urlErr != nil {
		jsonError(w, http.StatusServiceUnavailable, "bad MuninnDB endpoint: "+urlErr.Error())
		return
	}

	transport := mcp.NewHTTPTransport(mcpURL, token)
	client := mcp.NewMCPClient(transport)

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	defer client.Close()

	result, callErr := client.CallTool(ctx, req.Tool, req.Args)
	if callErr != nil {
		jsonError(w, http.StatusBadGateway, "tool call failed: "+callErr.Error())
		return
	}

	jsonOK(w, map[string]any{"result": result})
}
