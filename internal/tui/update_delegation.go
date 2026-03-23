package tui

// update_delegation.go — handlers for agent delegation, swarm, and WebSocket events.
// Extracted from the monolithic Update() in app.go.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/scrypster/huginn/internal/swarm"
)

// handleDelegationStartMsg initialises the delegation UI when the primary
// agent delegates a question to another agent.
func (a *App) handleDelegationStartMsg(msg delegationStartMsg) (tea.Model, tea.Cmd) {
	a.delegationBuf = ""
	a.delegationAgent = msg.To
	color := string(colorAccent)
	if a.agentReg != nil {
		if ag, ok := a.agentReg.ByName(msg.To); ok {
			color = ag.Color
		}
	}
	a.delegationAgentColor = color
	preview := msg.Question
	if len([]rune(preview)) > 60 {
		preview = string([]rune(preview)[:60]) + "…"
	}
	a.addLine("delegation-start", fmt.Sprintf("%s → %s: %q", msg.From, msg.To, preview))
	a.refreshViewport()
	return a, nil
}

// handleDelegationTokenMsg accumulates streaming tokens from the delegated agent.
func (a *App) handleDelegationTokenMsg(msg delegationTokenMsg) (tea.Model, tea.Cmd) {
	a.delegationBuf += msg.Token
	return a, nil
}

// handleDelegationDoneMsg finalises a delegation round, committing the
// delegated agent's answer to the chat history.
func (a *App) handleDelegationDoneMsg(msg delegationDoneMsg) (tea.Model, tea.Cmd) {
	answer := a.delegationBuf
	if answer == "" {
		answer = msg.Answer
	}
	a.delegationBuf = ""
	a.delegationAgent = ""
	a.addLine("delegation-done", fmt.Sprintf("[%s] %s", msg.To, answer))
	a.refreshViewport()
	return a, nil
}

// handleSwarmEventMsg processes a real-time event from the swarm orchestrator,
// updating the swarm view with token output, tool status, or completion state.
func (a *App) handleSwarmEventMsg(msg swarmEventMsg) (tea.Model, tea.Cmd) {
	if a.swarmView == nil {
		a.swarmView = NewSwarmViewModel(a.width, a.height)
	}
	switch msg.event.Type {
	case swarm.EventSwarmReady:
		if specs, ok := msg.event.Payload.([]swarm.SwarmTaskSpec); ok {
			for _, s := range specs {
				a.swarmView.AddAgent(s.ID, s.Name, s.Color)
			}
		}
	case swarm.EventToken:
		if payload, ok := msg.event.Payload.(string); ok {
			a.swarmView.AppendOutput(msg.event.AgentID, payload)
		}
	case swarm.EventToolStart:
		if payload, ok := msg.event.Payload.(string); ok {
			a.swarmView.SetToolName(msg.event.AgentID, payload)
		}
	case swarm.EventStatusChange:
		if status, ok := msg.event.Payload.(swarm.AgentStatus); ok {
			a.swarmView.SetStatus(msg.event.AgentID, status)
		}
	case swarm.EventComplete:
		a.swarmView.SetStatus(msg.event.AgentID, swarm.StatusDone)
	case swarm.EventError:
		a.swarmView.SetStatus(msg.event.AgentID, swarm.StatusError)
	}
	return a, readSwarmEvent(a.swarmEvents)
}

// handleSwarmDoneMsg cleans up the swarm view and returns the TUI to the
// normal chat state after all swarm agents have finished.
func (a *App) handleSwarmDoneMsg(msg swarmDoneMsg) (tea.Model, tea.Cmd) {
	a.state = stateChat
	if msg.output != "" {
		a.addLine("assistant", msg.output)
	}
	a.swarmEvents = nil
	a.swarmView = nil
	a.recalcViewportHeight()
	a.refreshViewport()
	return a, nil
}

// handleWsEventMsg processes inbound WebSocket events such as primary agent
// changes and session cost updates.
func (a *App) handleWsEventMsg(msg wsEventMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case "primary_agent_changed":
		if name, ok := msg.Payload["agent"].(string); ok && name != "" {
			a.primaryAgent = name
		}
	case "cost_update":
		if total, ok := msg.Payload["total"].(float64); ok {
			a.sessionCostUSD = total
		}

	case "agent_briefing_start":
		// Show briefing indicator in chat and mark agent as active in sidebar.
		if msg.Payload == nil {
			break
		}
		agentName, _ := msg.Payload["agent_name"].(string)
		if agentName == "" {
			break
		}
		color := agentColorFromName(agentName)
		a.chat.history = append(a.chat.history, chatLine{
			role:       "system",
			content:    agentName + "  ← briefing from memory...",
			agentName:  agentName,
			agentColor: color,
			agentIcon:  agentIconFromName(agentName),
		})
		// Mark agent active in sidebar.
		if a.activeAgents == nil {
			a.activeAgents = make(map[string]bool)
		}
		a.activeAgents[agentName] = true
		a.sidebar.SetAgentActive(agentName, true)
		a.refreshViewport()

	case "agent_briefing_done":
		// Replace or append the briefing-done line and mark agent idle.
		if msg.Payload == nil {
			break
		}
		agentName, _ := msg.Payload["agent_name"].(string)
		if agentName == "" {
			break
		}
		memoriesLoaded := 0
		if v, ok := msg.Payload["memories_loaded"].(float64); ok {
			memoriesLoaded = int(v)
		}
		artifactsLoaded := 0
		if v, ok := msg.Payload["artifacts_loaded"].(float64); ok {
			artifactsLoaded = int(v)
		}
		readyLine := fmt.Sprintf("%s  ◉  ready  ·  recalled %d memories  ·  %d recent artifacts",
			agentName, memoriesLoaded, artifactsLoaded)
		// Try to update the last briefing-start line for this agent, else append.
		updated := false
		for i := len(a.chat.history) - 1; i >= 0; i-- {
			if a.chat.history[i].agentName == agentName &&
				a.chat.history[i].role == "system" &&
				strings.Contains(a.chat.history[i].content, "briefing from memory") {
				a.chat.history[i].content = readyLine
				updated = true
				break
			}
		}
		if !updated {
			color := agentColorFromName(agentName)
			a.chat.history = append(a.chat.history, chatLine{
				role:       "system",
				content:    readyLine,
				agentName:  agentName,
				agentColor: color,
				agentIcon:  agentIconFromName(agentName),
			})
		}
		// Mark agent idle.
		if a.activeAgents != nil {
			delete(a.activeAgents, agentName)
		}
		a.sidebar.SetAgentActive(agentName, false)
		a.refreshViewport()

	case "swarm_status":
		// Update inline swarm bar.
		if msg.Payload == nil {
			break
		}
		swarmID, _ := msg.Payload["swarm_id"].(string)
		if swarmID == "" {
			break
		}
		var agents []swarmAgentStatus
		if rawAgents, ok := msg.Payload["agents"].([]interface{}); ok {
			for _, raw := range rawAgents {
				if agMap, ok := raw.(map[string]interface{}); ok {
					as := swarmAgentStatus{}
					as.name, _ = agMap["name"].(string)
					as.status, _ = agMap["status"].(string)
					if pct, ok := agMap["pct"].(float64); ok {
						as.pct = int(pct)
					}
					as.tool, _ = agMap["tool"].(string)
					as.elapsed, _ = agMap["elapsed"].(string)
					as.output, _ = agMap["output"].(string)
					agents = append(agents, as)
				}
			}
		}
		a.updateSwarmBar(swarmID, agents)
	}
	return a, nil
}
