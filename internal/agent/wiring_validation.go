package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/relay"
	"github.com/scrypster/huginn/internal/skills"
)

type orchestratorOptionalDeps struct {
	memoryStore      agents.MemoryStoreIface
	memoryReplicator *MemoryReplicator
	relayHub         relay.Hub
	wsBroadcast      func(sessionID, msgType string, payload map[string]any)
	skillsRegistry   *skills.SkillRegistry
}

// ValidateWiring checks required orchestrator dependencies and invariants.
func (o *Orchestrator) ValidateWiring() error {
	o.mu.RLock()
	defer o.mu.RUnlock()

	var missing []string
	if o.backend == nil {
		missing = append(missing, "backend")
	}
	if o.contextBuilder == nil {
		missing = append(missing, "context_builder")
	}
	if o.sessions == nil {
		missing = append(missing, "sessions")
	}
	if o.defaultSessionID == "" {
		missing = append(missing, "default_session_id")
	} else if o.sessions != nil && o.sessions[o.defaultSessionID] == nil {
		missing = append(missing, "default_session")
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return fmt.Errorf("orchestrator wiring invalid: missing %s", strings.Join(missing, ", "))
}

func (o *Orchestrator) optionalMemoryStoreLocked() agents.MemoryStoreIface {
	if o.optionals.memoryStore != nil {
		return o.optionals.memoryStore
	}
	return o.memoryStore
}

func (o *Orchestrator) optionalMemoryReplicatorLocked() *MemoryReplicator {
	if o.optionals.memoryReplicator != nil {
		return o.optionals.memoryReplicator
	}
	return o.memoryReplicator
}

func (o *Orchestrator) optionalSkillsRegistryLocked() *skills.SkillRegistry {
	if o.optionals.skillsRegistry != nil {
		return o.optionals.skillsRegistry
	}
	return o.skillsReg
}
