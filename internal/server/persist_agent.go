package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/scrypster/huginn/internal/agents"
)

// persistAgentError is the HTTP-mapped error from PersistAgent / persistAgent.
// handleUpdateAgent stays a thin decoder over these status codes.
type persistAgentError struct {
	status int
	msg    string
}

func (e *persistAgentError) Error() string { return e.msg }

func persistErr(status int, msg string) error {
	return &persistAgentError{status: status, msg: msg}
}

func writePersistError(w http.ResponseWriter, err error) {
	if pe, ok := err.(*persistAgentError); ok {
		jsonError(w, pe.status, pe.msg)
		return
	}
	jsonError(w, 500, err.Error())
}

// PersistAgent writes incoming to the same store as PUT /api/v1/agents/{name}.
// created is true when no agent existed under the path name (a create).
// Duplicate create/rename → 409. Vault collision / invalid / toolbelt fail-closed → 422.
func (s *Server) PersistAgent(incoming agents.AgentDef) (created bool, err error) {
	if strings.TrimSpace(incoming.Name) == "" {
		return false, persistErr(400, "agent name is required")
	}
	return s.persistAgent(incoming, incoming.Name)
}

// persistAgent is the shared write path. pathName is the URL name on HTTP PUT
// (may differ from incoming.Name on rename). The hire tool passes incoming.Name.
func (s *Server) persistAgent(incoming agents.AgentDef, pathName string) (created bool, err error) {
	if strings.TrimSpace(pathName) == "" {
		return false, persistErr(400, "agent name is required")
	}

	existingCfg, _ := agents.LoadAgents()
	if existingCfg == nil {
		existingCfg = agents.DefaultAgentsConfig()
	}

	var existingAgent *agents.AgentDef
	for i := range existingCfg.Agents {
		if strings.EqualFold(existingCfg.Agents[i].Name, pathName) {
			existingAgent = &existingCfg.Agents[i]
			break
		}
	}

	if incoming.APIKey == "[REDACTED]" && existingAgent != nil {
		incoming.APIKey = existingAgent.APIKey
	}

	if incoming.Version > 0 && existingAgent != nil && incoming.Version != existingAgent.Version {
		return false, persistErr(409, fmt.Sprintf("agent version conflict: stored=%d, submitted=%d — reload and retry",
			existingAgent.Version, incoming.Version))
	}

	isRename := !strings.EqualFold(incoming.Name, pathName)
	isCreation := existingAgent == nil
	if isRename || isCreation {
		for _, a := range existingCfg.Agents {
			if strings.EqualFold(a.Name, incoming.Name) {
				if isRename && strings.EqualFold(a.Name, pathName) {
					continue
				}
				return false, persistErr(409, fmt.Sprintf("agent %q already exists", incoming.Name))
			}
		}
	}

	if err := agents.CheckVaultNameCollision(incoming, pathName, "", existingCfg.Agents); err != nil {
		return false, persistErr(422, err.Error())
	}

	if incoming.Provider == "" && incoming.Model != "" {
		incoming.Provider = agents.InferProvider(incoming.Model)
	}

	if err := incoming.ApplyMemoryType(); err != nil {
		return false, persistErr(400, "invalid memory_type: "+err.Error())
	}
	if incoming.Description == "" {
		incoming.Description = agents.ExtractRoleBlurb(incoming.SystemPrompt, "")
	}
	if err := incoming.Validate(); err != nil {
		return false, persistErr(422, "invalid agent: "+err.Error())
	}
	toolbeltResult, err := s.evaluateToolbelt(incoming.Toolbelt)
	if err != nil {
		return false, persistErr(500, "validate toolbelt: "+err.Error())
	}
	if !toolbeltResult.Valid {
		if denied, ok := toolbeltResult.FirstDenied(); ok {
			return false, persistErr(422, fmt.Sprintf("invalid toolbelt: %s (%s)", denied.Reason, denied.ReasonCode))
		}
		return false, persistErr(422, "invalid toolbelt")
	}
	if err := agents.SaveAgentDefault(incoming); err != nil {
		return false, persistErr(500, "save agent: "+err.Error())
	}
	if isRename {
		_ = agents.DeleteAgentDefault(pathName)
	}
	if isRename {
		_ = agents.RenameHeartbeatYAMLDefault(pathName, incoming)
	} else {
		_ = agents.SyncHeartbeatYAMLDefault(incoming)
	}
	s.notifyAgentsChanged()
	action := "updated"
	if existingAgent == nil {
		action = "created"
	}
	s.BroadcastWS(WSMessage{
		Type: "agent_changed",
		Payload: map[string]any{
			"name":   incoming.Name,
			"action": action,
		},
	})
	if existingAgent == nil {
		s.logEntityAudit("agent_create", "hired agent "+incoming.Name, map[string]any{"agent": incoming.Name})
	}
	return existingAgent == nil, nil
}
