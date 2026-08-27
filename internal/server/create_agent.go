package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/threadmgr"
	"github.com/scrypster/huginn/internal/tools"
)

// NewCreateAgentTool wires the grant-gated hire tool to PersistAgent, Muninn
// vault create, and SeatMember. Register on the live toolReg; do not tag builtin.
func (s *Server) NewCreateAgentTool() *tools.CreateAgentTool {
	return &tools.CreateAgentTool{Deps: tools.CreateAgentDeps{
		Persist: func(req tools.CreateAgentRequest) error {
			return s.persistHiredAgent(req)
		},
		AgentExists: func(name string) bool {
			return s.agentExistsOnDisk(name)
		},
		TryVault: func(vaultName, label string) bool {
			_, err := s.createMuninnVault(vaultName, label)
			return err == nil
		},
		SpaceCompanyID: func(spaceID string) (string, error) {
			cs := s.companyAPI()
			if cs == nil {
				return "", nil
			}
			type spaceCo interface {
				SpaceCompanyID(spaceID string) (string, error)
			}
			if sc, ok := s.spaceStore.(spaceCo); ok {
				return sc.SpaceCompanyID(spaceID)
			}
			return "", nil
		},
		AgentInCompany: func(agentName, companyID string) (bool, error) {
			cs := s.companyAPI()
			if cs == nil {
				return false, nil
			}
			return cs.AgentInCompany(agentName, companyID)
		},
		CompanyName: func(id string) (string, error) {
			cs := s.companyAPI()
			if cs == nil {
				return "", nil
			}
			c, err := cs.GetCompany(id)
			if err != nil || c == nil {
				return "", err
			}
			return c.Name, nil
		},
		SeatMember: func(companyID, agentName string) error {
			cs := s.companyAPI()
			if cs == nil {
				return nil
			}
			return cs.SeatMember(companyID, agentName)
		},
		SeatSpaceMember: func(spaceID, agentName string) error {
			if s.spaceStore == nil {
				return nil
			}
			sp, err := s.spaceStore.GetSpace(spaceID)
			if err != nil || sp == nil || sp.Kind == spaces.KindDM {
				return err
			}
			old := append([]string{}, sp.Members...)
			for _, m := range old {
				if strings.EqualFold(m, agentName) {
					return nil
				}
			}
			members := append(old, agentName)
			if _, err := s.spaceStore.UpdateSpace(spaceID, spaces.SpaceUpdates{Members: &members}); err != nil {
				return err
			}
			s.emitSpaceMemberEvents(spaceID, old, members)
			return nil
		},
		ResolveConn: func(id string) (string, bool) {
			if s.connStore == nil {
				return "", false
			}
			c, ok := s.connStore.Get(id)
			if !ok {
				return "", false
			}
			return string(c.Provider), true
		},
		ValidateName: validateSubdomain,
		CallerFromCtx: threadmgr.GetCallingAgent,
		SpaceFromCtx:  agent.GetSpaceID,
		CallerModel: func(ctx context.Context) string {
			caller := strings.TrimSpace(threadmgr.GetCallingAgent(ctx))
			if caller == "" {
				return ""
			}
			cfg, err := agents.LoadAgents()
			if err != nil || cfg == nil {
				return ""
			}
			for _, a := range cfg.Agents {
				if strings.EqualFold(a.Name, caller) {
					return a.Model
				}
			}
			return ""
		},
		MachineModel: firstNonEmpty(s.cfg.DefaultModel, s.cfg.ReasonerModel),
	}}
}

func (s *Server) persistHiredAgent(req tools.CreateAgentRequest) error {
	if s.agentExistsOnDisk(req.Name) {
		return persistErr(409, fmt.Sprintf("agent %q already exists", req.Name))
	}
	mem := req.Memory
	def := agents.AgentDef{
		Name:         req.Name,
		Description:  req.Description,
		SystemPrompt: req.SystemPrompt,
		Model:        req.Model,
		LocalTools:   req.LocalTools,
		VaultName:    req.VaultName,
		MemoryEnabled: &mem,
		Color:        "#58a6ff",
		Icon:         firstRuneUpper(req.Name),
	}
	if def.Provider == "" && def.Model != "" {
		def.Provider = agents.InferProvider(def.Model)
	}
	if mem {
		def.MemoryType = "muninndb"
	} else {
		def.MemoryType = "none"
	}
	for _, e := range req.Toolbelt {
		def.Toolbelt = append(def.Toolbelt, agents.ToolbeltEntry{
			ConnectionID: e.ConnectionID,
			Provider:     e.Provider,
		})
	}
	_, err := s.persistAgent(def, def.Name)
	return err
}

func (s *Server) agentExistsOnDisk(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	cfg, err := agents.LoadAgents()
	if err != nil || cfg == nil {
		return false
	}
	for _, a := range cfg.Agents {
		if strings.EqualFold(a.Name, name) {
			return true
		}
	}
	return false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func firstRuneUpper(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "A"
	}
	r := []rune(s)
	return strings.ToUpper(string(r[0]))
}
