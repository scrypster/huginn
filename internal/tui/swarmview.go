package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/scrypster/huginn/internal/swarm"
)

const spinnerFrames = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
const maxOutputLines = 50

// swarmAgentView represents the UI state of a single agent.
type swarmAgentView struct {
	id       string
	name     string
	color    string
	status   swarm.AgentStatus
	toolName string
	output   []string
	startAt  time.Time
}

// SwarmViewModel manages the TUI view state for a swarm of agents.
type SwarmViewModel struct {
	agents     []*swarmAgentView
	agentIndex map[string]*swarmAgentView
	focusedID  string
	spinnerIdx int
	width      int
	height     int
}

// NewSwarmViewModel creates a new SwarmViewModel.
func NewSwarmViewModel(width, height int) *SwarmViewModel {
	return &SwarmViewModel{
		agentIndex: make(map[string]*swarmAgentView),
		width:      width,
		height:     height,
	}
}

// AddAgent adds a new agent to the view model.
func (sv *SwarmViewModel) AddAgent(id, name, color string) {
	av := &swarmAgentView{
		id:      id,
		name:    name,
		color:   color,
		status:  swarm.StatusQueued,
		startAt: time.Now(),
	}
	sv.agents = append(sv.agents, av)
	sv.agentIndex[id] = av
}

// SetStatus updates the status of an agent.
func (sv *SwarmViewModel) SetStatus(id string, status swarm.AgentStatus) {
	if av, ok := sv.agentIndex[id]; ok {
		av.status = status
		if status == swarm.StatusDone || status == swarm.StatusError || status == swarm.StatusCancelled {
			av.toolName = ""
		}
	}
}

// SetToolName updates the current tool name for an agent.
func (sv *SwarmViewModel) SetToolName(id, toolName string) {
	if av, ok := sv.agentIndex[id]; ok {
		av.toolName = toolName
		av.status = swarm.StatusTooling
	}
}

// AppendOutput appends a line of output to an agent's output buffer.
func (sv *SwarmViewModel) AppendOutput(id, line string) {
	if av, ok := sv.agentIndex[id]; ok {
		av.output = append(av.output, line)
		if len(av.output) > maxOutputLines {
			av.output = av.output[len(av.output)-maxOutputLines:]
		}
	}
}

// SetFocus sets which agent is focused.
func (sv *SwarmViewModel) SetFocus(id string) {
	sv.focusedID = id
}

// FocusedID returns the currently focused agent ID.
func (sv *SwarmViewModel) FocusedID() string {
	return sv.focusedID
}

// TickSpinner advances the spinner frame index.
func (sv *SwarmViewModel) TickSpinner() {
	frames := []rune(spinnerFrames)
	sv.spinnerIdx = (sv.spinnerIdx + 1) % len(frames)
}

// SpinnerFrame returns the current spinner frame.
func (sv *SwarmViewModel) SpinnerFrame() string {
	frames := []rune(spinnerFrames)
	return string(frames[sv.spinnerIdx])
}

// View renders the current view (overview or focused agent).
func (sv *SwarmViewModel) View() string {
	if sv.focusedID != "" {
		return sv.viewFocused()
	}
	return sv.viewOverview()
}

// viewOverview renders the overview with all agent cards.
func (sv *SwarmViewModel) viewOverview() string {
	var sections []string
	for _, av := range sv.agents {
		sections = append(sections, sv.renderCard(av))
	}
	sections = append(sections, sv.renderFooter())
	return strings.Join(sections, "\n")
}

// renderCard renders a single agent card.
func (sv *SwarmViewModel) renderCard(av *swarmAgentView) string {
	elapsed := time.Since(av.startAt).Round(100 * time.Millisecond).Seconds()
	statusLabel := sv.statusLabel(av)
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(av.color)).Bold(true)
	title := titleStyle.Render(fmt.Sprintf(" %s [%s] ─── %.1fs ", av.name, statusLabel, elapsed))

	preview := ""
	if av.status == swarm.StatusTooling && av.toolName != "" {
		preview = "$ " + av.toolName
	} else if len(av.output) > 0 {
		last := av.output[len(av.output)-1]
		if len([]rune(last)) > sv.width-4 {
			last = string([]rune(last)[:sv.width-7]) + "…"
		}
		preview = last
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(av.color)).
		Width(sv.width - 2).
		Padding(0, 1)

	return boxStyle.Render(title + "\n" + preview + "\n▌")
}

// viewFocused renders the focused agent's detailed view.
func (sv *SwarmViewModel) viewFocused() string {
	av, ok := sv.agentIndex[sv.focusedID]
	if !ok {
		return "agent not found"
	}

	lines := av.output
	viewH := sv.height - 4
	if viewH < 1 {
		viewH = 1
	}
	if len(lines) > viewH {
		lines = lines[len(lines)-viewH:]
	}

	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(av.color)).Bold(true)
	title := titleStyle.Render(fmt.Sprintf(" %s — %s ", av.name, sv.statusLabel(av)))

	content := strings.Join(lines, "\n")

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(av.color)).
		Width(sv.width - 2).
		Padding(0, 1)

	footer := StyleDim.Render("  esc back to overview · j/k scroll · c cancel")

	return boxStyle.Render(title+"\n"+content) + "\n" + footer
}

// renderFooter renders the summary footer.
func (sv *SwarmViewModel) renderFooter() string {
	total := len(sv.agents)
	running := 0
	for _, av := range sv.agents {
		if av.status == swarm.StatusThinking || av.status == swarm.StatusTooling {
			running++
		}
	}

	focusHint := ""
	if total > 0 && total <= 9 {
		focusHint = fmt.Sprintf(" [1-%d] focus ·", total)
	}

	bar := fmt.Sprintf(" %d agents │ %d running │%s [c] cancel", total, running, focusHint)
	return StyleDim.Render(strings.Repeat("─", sv.width)) + "\n" + bar
}

// statusLabel returns a human-readable status string with icons.
func (sv *SwarmViewModel) statusLabel(av *swarmAgentView) string {
	switch av.status {
	case swarm.StatusQueued:
		return "○ queued"
	case swarm.StatusThinking:
		return sv.SpinnerFrame() + " thinking"
	case swarm.StatusTooling:
		if av.toolName != "" {
			return "⚡ " + av.toolName
		}
		return "⚡ tooling"
	case swarm.StatusDone:
		return "✓ done"
	case swarm.StatusError:
		return "✗ error"
	case swarm.StatusCancelled:
		return "⊘ cancelled"
	}
	return "?"
}

// CountRunning returns the number of currently running agents.
func (sv *SwarmViewModel) CountRunning() int {
	n := 0
	for _, av := range sv.agents {
		if av.status == swarm.StatusThinking || av.status == swarm.StatusTooling {
			n++
		}
	}
	return n
}
