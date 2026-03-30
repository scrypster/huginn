package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	mcp "github.com/scrypster/huginn/internal/mcp"
	"github.com/scrypster/huginn/internal/memory"
)

// handleMuninnTest tests a MuninnDB connection using provided credentials.
// POST /api/v1/muninn/test
// Body: {"endpoint":"...","username":"...","password":"..."}
// Response: {"ok":true} or {"ok":false,"error":"..."}
// Note: always returns HTTP 200 by design so callers can distinguish
// "probe ran but auth failed" from "network unreachable". Check the
// "ok" field for the actual result.
func (s *Server) handleMuninnTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Endpoint string `json:"endpoint"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": "invalid request"})
		return
	}
	if req.Endpoint == "" {
		req.Endpoint = "http://localhost:8475"
	}
	if req.Username == "" {
		req.Username = "root"
	}

	client := memory.NewMuninnSetupClient(req.Endpoint)
	_, err := client.Login(req.Username, req.Password)
	if err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

// handleMuninnConnect saves MuninnDB connection settings and root password.
// POST /api/v1/muninn/connect
// Body: {"endpoint":"...","username":"...","password":"..."}
func (s *Server) handleMuninnConnect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Endpoint string `json:"endpoint"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid request")
		return
	}

	// Test the connection before saving.
	client := memory.NewMuninnSetupClient(req.Endpoint)
	if _, err := client.Login(req.Username, req.Password); err != nil {
		jsonError(w, http.StatusBadRequest, "connection failed: "+err.Error())
		return
	}

	// Save endpoint + username to muninn.json.
	cfg, err := memory.LoadGlobalConfig(s.muninnCfgPath)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "load config: "+err.Error())
		return
	}
	cfg.Endpoint = req.Endpoint
	cfg.Username = req.Username
	if err := memory.SaveGlobalConfig(s.muninnCfgPath, cfg); err != nil {
		jsonError(w, http.StatusInternalServerError, "save config: "+err.Error())
		return
	}

	// Save password to keychain.
	ps := memory.NewKeychainPasswordStore()
	if err := ps.StorePassword(req.Password); err != nil {
		jsonError(w, http.StatusInternalServerError, "keychain: "+err.Error())
		return
	}

	jsonOK(w, map[string]any{"ok": true})
}

// handleMuninnVaultsList returns vaults known to Huginn (from vault_tokens).
// GET /api/v1/muninn/vaults
// Response: {"vaults":["huginn-steve","huginn-chris"],"endpoint":"...","username":"...","connected":true}
func (s *Server) handleMuninnVaultsList(w http.ResponseWriter, r *http.Request) {
	cfg, err := memory.LoadGlobalConfig(s.muninnCfgPath)
	if err != nil {
		jsonOK(w, map[string]any{
			"vaults":    []string{},
			"endpoint":  "",
			"username":  "",
			"connected": false,
		})
		return
	}
	vaults := make([]string, 0, len(cfg.VaultTokens))
	for v := range cfg.VaultTokens {
		vaults = append(vaults, v)
	}
	sort.Strings(vaults)
	jsonOK(w, map[string]any{
		"vaults":    vaults,
		"endpoint":  cfg.Endpoint,
		"username":  cfg.Username,
		"connected": cfg.Endpoint != "",
	})
}

// handleMuninnVaultCreate creates a vault in MuninnDB, generates a mk_... key,
// stores the token in muninn.json, and returns the vault name.
// POST /api/v1/muninn/vaults
// Body: {"vault_name":"huginn-steve","agent_label":"huginn-agent"}
// Response: {"vault_name":"huginn-steve","token":"mk_..."}
func (s *Server) handleMuninnVaultCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		VaultName  string `json:"vault_name"`
		AgentLabel string `json:"agent_label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.VaultName == "" || req.AgentLabel == "" {
		jsonError(w, http.StatusBadRequest, "vault_name and agent_label required")
		return
	}

	cfg, err := memory.LoadGlobalConfig(s.muninnCfgPath)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "load config: "+err.Error())
		return
	}
	if cfg.Endpoint == "" {
		jsonError(w, http.StatusBadRequest, "MuninnDB not configured")
		return
	}

	// Load root password from keychain.
	ps := memory.NewKeychainPasswordStore()
	password, err := ps.GetPassword()
	if err != nil {
		jsonError(w, http.StatusBadRequest, "MuninnDB password not stored — re-connect in Connections")
		return
	}

	// Login.
	client := memory.NewMuninnSetupClient(cfg.Endpoint)
	sessionCookie, err := client.Login(cfg.Username, password)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "login failed: "+err.Error())
		return
	}

	// Create vault + key.
	token, err := client.CreateVaultAndKey(sessionCookie, req.VaultName, req.AgentLabel)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "create vault: "+err.Error())
		return
	}

	// Store token in muninn.json.
	if cfg.VaultTokens == nil {
		cfg.VaultTokens = make(map[string]string)
	}
	cfg.VaultTokens[req.VaultName] = token
	if err := memory.SaveGlobalConfig(s.muninnCfgPath, cfg); err != nil {
		jsonError(w, http.StatusInternalServerError, "save config: "+err.Error())
		return
	}

	jsonOK(w, map[string]any{
		"vault_name": req.VaultName,
		"token":      token,
	})
}

// handleAgentVaultHealth probes the vault connectivity for a named agent and returns a
// structured health status that admins can observe.
//
// GET /api/v1/agents/{name}/vault-status
// Response: {"status":"ok"|"degraded"|"unavailable","tools_count":N,"warning":"...","latency_ms":N}
//
// A "degraded" status means the vault is reachable but returned a warning (e.g. no token).
// An "unavailable" status means the MCP connection could not be established within 10s.
func (s *Server) handleAgentVaultHealth(w http.ResponseWriter, r *http.Request) {
	agentName := r.PathValue("name")
	if agentName == "" {
		jsonError(w, http.StatusBadRequest, "agent name is required")
		return
	}

	s.mu.Lock()
	cfgPath := s.muninnCfgPath
	s.mu.Unlock()

	start := time.Now()

	if cfgPath == "" {
		jsonOK(w, map[string]any{
			"status":     "unavailable",
			"tools_count": 0,
			"warning":    "muninn config path not set",
			"latency_ms": time.Since(start).Milliseconds(),
		})
		return
	}

	muninnCfg, err := memory.LoadGlobalConfig(cfgPath)
	if err != nil || muninnCfg.Endpoint == "" {
		warn := "muninn config unavailable"
		if err != nil {
			warn = "muninn config load: " + err.Error()
		}
		jsonOK(w, map[string]any{
			"status":     "unavailable",
			"tools_count": 0,
			"warning":    warn,
			"latency_ms": time.Since(start).Milliseconds(),
		})
		return
	}

	// Find the agent's vault name.
	agentLoader := s.agentLoader
	if agentLoader == nil {
		agentLoader = agents.LoadAgents
	}
	agentsCfg, loadErr := agentLoader()
	if loadErr != nil {
		jsonOK(w, map[string]any{
			"status":      "unavailable",
			"tools_count": 0,
			"warning":     "failed to load agents: " + loadErr.Error(),
			"latency_ms":  time.Since(start).Milliseconds(),
		})
		return
	}
	var vaultName string
	for _, a := range agentsCfg.Agents {
		if a.Name == agentName {
			vaultName = a.VaultName
			break
		}
	}
	if vaultName == "" {
		jsonOK(w, map[string]any{
			"status":     "unavailable",
			"tools_count": 0,
			"warning":    "agent has no vault configured",
			"latency_ms": time.Since(start).Milliseconds(),
		})
		return
	}

	token, err := memory.MCPTokenFor(muninnCfg, vaultName)
	if err != nil {
		jsonOK(w, map[string]any{
			"status":     "degraded",
			"tools_count": 0,
			"warning":    "no MCP token configured (set mcp_token in muninn.json): " + err.Error(),
			"latency_ms": time.Since(start).Milliseconds(),
		})
		return
	}

	mcpURL, err := memory.MCPURLFromEndpoint(muninnCfg.Endpoint)
	if err != nil {
		jsonOK(w, map[string]any{
			"status":     "unavailable",
			"tools_count": 0,
			"warning":    "invalid muninn endpoint: " + err.Error(),
			"latency_ms": time.Since(start).Milliseconds(),
		})
		return
	}

	tr := mcp.NewHTTPTransport(mcpURL, token)
	client := mcp.NewMCPClient(tr)

	connectCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := client.Initialize(connectCtx); err != nil {
		jsonOK(w, map[string]any{
			"status":     "unavailable",
			"tools_count": 0,
			"warning":    "connect failed: " + err.Error(),
			"latency_ms": time.Since(start).Milliseconds(),
		})
		return
	}
	defer client.Close()

	mcpTools, err := client.ListTools(connectCtx)
	if err != nil {
		jsonOK(w, map[string]any{
			"status":     "degraded",
			"tools_count": 0,
			"warning":    "list tools failed: " + err.Error(),
			"latency_ms": time.Since(start).Milliseconds(),
		})
		return
	}

	jsonOK(w, map[string]any{
		"status":     "ok",
		"tools_count": len(mcpTools),
		"warning":    "",
		"latency_ms": time.Since(start).Milliseconds(),
	})
}

// handleMuninnStatus returns the current MuninnDB connection status.
// If not configured, probes the default endpoint to detect a local MuninnDB instance.
// GET /api/v1/muninn/status
// Response: {"connected":bool,"detected":bool,"endpoint":"...","username":"..."}
func (s *Server) handleMuninnStatus(w http.ResponseWriter, r *http.Request) {
	const defaultEndpoint = "http://localhost:8475"

	cfg, err := memory.LoadGlobalConfig(s.muninnCfgPath)
	if err != nil || cfg.Endpoint == "" {
		detected := memory.Probe(defaultEndpoint)
		resp := map[string]any{
			"connected": false,
			"detected":  detected,
		}
		if detected {
			resp["endpoint"] = defaultEndpoint
			resp["username"] = "root"
		}
		jsonOK(w, resp)
		return
	}
	jsonOK(w, map[string]any{
		"connected": true,
		"detected":  true,
		"endpoint":  cfg.Endpoint,
		"username":  cfg.Username,
	})
}
