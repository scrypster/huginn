package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
)

// handleVaultTest probes the MCP vault connectivity for a named agent.
//
//	POST /api/v1/agents/{name}/vault/test
//
// Optional JSON body: {"vault_name":"<override>"}
// If vault_name is provided in the body it overrides the agent's configured vault name.
// Response on success (200): {"status":"ok","vault":"<name>","tools_count":<n>}
// Response on unreachable (422): {"status":"error","vault":"<name>","detail":"<msg>"}
// Response on no vault configured (422): {"error":"no vault configured for agent"}
// Response on bad request (400): {"error":"..."}
func (s *Server) handleVaultTest(w http.ResponseWriter, r *http.Request) {
	agentName := r.PathValue("name")
	if agentName == "" {
		jsonError(w, http.StatusBadRequest, "agent name is required")
		return
	}

	// Optional body to override vault name.
	var body struct {
		VaultName string `json:"vault_name"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}

	// Resolve the agent config to get its vault name.
	agentLoader := s.agentLoader
	if agentLoader == nil {
		agentLoader = agents.LoadAgents
	}
	agentsCfg, err := agentLoader()

	// Find the vault name: body override > agent config.
	vaultName := body.VaultName
	if vaultName == "" && agentsCfg != nil {
		for _, a := range agentsCfg.Agents {
			if strings.EqualFold(a.Name, agentName) {
				vaultName = a.VaultName
				break
			}
		}
	}
	if err != nil && vaultName == "" {
		jsonError(w, http.StatusInternalServerError, "failed to load agents: "+err.Error())
		return
	}

	if vaultName == "" {
		jsonError(w, http.StatusUnprocessableEntity, "no vault configured for agent")
		return
	}

	// Resolve the prober function.
	prober := s.vaultProberFn
	if prober == nil {
		// Production default: use agent.ProbeVaultConnectivity.
		prober = func(ctx context.Context, cfgPath, vaultN string) (int, string, error) {
			return agent.ProbeVaultConnectivity(ctx, cfgPath, vaultN)
		}
	}

	s.mu.Lock()
	cfgPath := s.muninnCfgPath
	s.mu.Unlock()

	toolsCount, warning, probeErr := prober(r.Context(), cfgPath, vaultName)
	if probeErr != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "error",
			"vault":  vaultName,
			"detail": probeErr.Error(),
		})
		return
	}

	resp := map[string]any{
		"status":      "ok",
		"vault":       vaultName,
		"tools_count": toolsCount,
	}
	if warning != "" {
		resp["warning"] = warning
	}
	jsonOK(w, resp)
}
