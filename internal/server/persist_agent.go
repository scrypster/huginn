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

	// Approved-tools RMW race (vet E): a grant from the permission banner
	// (grantApprovedTool) does a read-modify-write of ApprovedTools, while a
	// PUT from the AgentsView editor sends the whole form as one blob. If a
	// grant lands while an editor tab is open, and that tab then saves
	// without touching the approved-tools chips, its stale in-memory copy
	// (captured at form-load time) would silently clobber the just-landed
	// grant back off disk.
	//
	// Rule: incoming.ApprovedTools is authoritative ONLY when it differs
	// from incoming.LoadedApprovedTools (the snapshot the client echoes back
	// from when it loaded the form) — that difference is the signal the user
	// actually touched the chips. Otherwise (nil — an old client/API caller
	// that never touched the field at all — or unchanged from the loaded
	// snapshot) the on-disk value wins, so a grant that landed after load
	// survives an untouched save. A genuine edit still overwrites a
	// concurrent grant (last-writer-wins) — documented, not fixed here.
	if existingAgent != nil && !approvedToolsExplicitlyEdited(incoming.ApprovedTools, incoming.LoadedApprovedTools) {
		incoming.ApprovedTools = existingAgent.ApprovedTools
	}
	incoming.LoadedApprovedTools = nil // bridge-only; never persisted

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
		// A live one-off specialist holds its name in the registry's ephemeral
		// overlay, which is NOT in existingCfg.Agents. Without this guard a
		// created/renamed hire could shadow a running specialist (ByName hits
		// the main map first), silently substituting a different model/prompt
		// into its in-flight thread (Opus vet 2026-08-29).
		if s.orch != nil {
			if reg := s.orch.GetAgentRegistry(); reg != nil && reg.IsEphemeral(incoming.Name) {
				return false, persistErr(409, fmt.Sprintf("name %q is in use by a one-off specialist that is still running", incoming.Name))
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

// approvedToolsExplicitlyEdited reports whether current represents a genuine
// user edit of the approved-tools chips, as opposed to a client echoing back
// the same snapshot it loaded (or an old client that never sent
// loaded_approved_tools at all — nil loaded still means "unknown", not
// "edited", so a nil current is never treated as an edit here regardless).
// Compared as sets: chip UI reordering is not an edit.
//
// Known limitation: (current=[], loaded=nil) — an old client explicitly
// sending an empty array to clear every chip — compares set-equal to nil and
// is therefore NOT treated as an edit, so the on-disk value is preserved and
// the clear does not happen. Accepted deliberately: honoring it would
// re-open the grant-clobbering RMW race for exactly the clients that cannot
// echo a loaded snapshot. The chip UI is unaffected (AgentsView always sends
// loaded_approved_tools); clearing without it means editing the agent YAML.
// Pinned by TestPersistAgent_EmptyArrayFromOldClientCannotClear.
func approvedToolsExplicitlyEdited(current, loaded []string) bool {
	if current == nil {
		return false
	}
	return !stringSetEqual(current, loaded)
}

// stringSetEqual reports whether a and b contain the same strings,
// ignoring order and duplicate counts.
func stringSetEqual(a, b []string) bool {
	setA := map[string]bool{}
	for _, s := range a {
		setA[s] = true
	}
	setB := map[string]bool{}
	for _, s := range b {
		setB[s] = true
	}
	if len(setA) != len(setB) {
		return false
	}
	for k := range setA {
		if !setB[k] {
			return false
		}
	}
	return true
}
