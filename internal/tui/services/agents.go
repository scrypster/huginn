package services

import (
	"github.com/scrypster/huginn/internal/agents"
)

// AgentService is the typed interface for agent CRUD.
type AgentService interface {
	List() []agents.AgentDef
	ByName(name string) (agents.AgentDef, bool)
	Save(def agents.AgentDef) error
	Delete(name string) error
	Names() []string
	SetDefault(name string)
}

// DirectAgentService implements AgentService using the in-process AgentRegistry.
type DirectAgentService struct {
	reg *agents.AgentRegistry
}

// NewDirectAgentService wraps an AgentRegistry.
func NewDirectAgentService(reg *agents.AgentRegistry) AgentService {
	return &DirectAgentService{reg: reg}
}

func (s *DirectAgentService) List() []agents.AgentDef {
	var result []agents.AgentDef
	for _, ag := range s.reg.All() {
		def := agents.AgentDef{
			Name:                ag.Name,
	
			Model:               ag.ModelID,
			SystemPrompt:        ag.SystemPrompt,
			Color:               ag.Color,
			Icon:                ag.Icon,
			IsDefault:           ag.IsDefault,
			Provider:            ag.Provider,
			Endpoint:            ag.Endpoint,
			APIKey:              ag.APIKey,
			VaultName:           ag.VaultName,
			Plasticity:          ag.Plasticity,
			ContextNotesEnabled: ag.ContextNotesEnabled,
			MemoryMode:          ag.MemoryMode,
			VaultDescription:    ag.VaultDescription,
			Toolbelt:            ag.Toolbelt,
			Skills:              ag.Skills,
			LocalTools:          ag.LocalTools,
		}
		me := ag.MemoryEnabled
		def.MemoryEnabled = &me
		result = append(result, def)
	}
	return result
}

func (s *DirectAgentService) ByName(name string) (agents.AgentDef, bool) {
	ag, ok := s.reg.ByName(name)
	if !ok {
		return agents.AgentDef{}, false
	}
	def := agents.AgentDef{
		Name:                ag.Name,

		Model:               ag.ModelID,
		SystemPrompt:        ag.SystemPrompt,
		Color:               ag.Color,
		Icon:                ag.Icon,
		IsDefault:           ag.IsDefault,
		Provider:            ag.Provider,
		Endpoint:            ag.Endpoint,
		APIKey:              ag.APIKey,
		VaultName:           ag.VaultName,
		Plasticity:          ag.Plasticity,
		ContextNotesEnabled: ag.ContextNotesEnabled,
		MemoryMode:          ag.MemoryMode,
		VaultDescription:    ag.VaultDescription,
		Toolbelt:            ag.Toolbelt,
		Skills:              ag.Skills,
		LocalTools:          ag.LocalTools,
	}
	me := ag.MemoryEnabled
	def.MemoryEnabled = &me
	return def, true
}

func (s *DirectAgentService) Save(def agents.AgentDef) error {
	return agents.SaveAgentDefault(def)
}

func (s *DirectAgentService) Delete(name string) error {
	return agents.DeleteAgentDefault(name)
}

func (s *DirectAgentService) Names() []string {
	return s.reg.Names()
}

func (s *DirectAgentService) SetDefault(name string) {
	s.reg.SetDefault(name)
}
