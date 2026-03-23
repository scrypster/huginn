package agents

import (
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// RegisterConsultTool registers the consult_agent tool into the given tool registry.
// It is a no-op when agentReg is nil.
// depth is a shared atomic depth counter to prevent recursive delegation.
func RegisterConsultTool(
	reg *tools.Registry,
	agentReg *AgentRegistry,
	b backend.Backend,
	depth *int32,
	onDelegate func(from, to, question string),
	onDone func(from, to, answer string),
	onToken func(agent, token string),
) {
	if agentReg == nil {
		return
	}
	tool := NewConsultAgentToolFull(agentReg, b, depth, onDelegate, onDone, onToken)
	reg.Register(tool)
}
