package tui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/scrypster/huginn/internal/agents"
)

type wizardStep int

const (
	wizStepName      wizardStep = iota
	wizStepModel                // model selection
	wizStepBackstory            // textarea for backstory
	wizStepMemory               // NEW — memory configuration
	wizStepConfirm              // show summary and confirm
	wizStepDone                 // created
)

var agentNameRegex = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`)

// isValidAgentName returns true if name matches ^[a-z][a-z0-9_-]{1,63}$
func isValidAgentName(name string) bool {
	return agentNameRegex.MatchString(name)
}

// AgentWizardDoneMsg is sent when the wizard completes successfully.
type AgentWizardDoneMsg struct {
	Agent agents.AgentDef
}

// AgentWizardCancelMsg is sent when the user cancels.
type AgentWizardCancelMsg struct{}

type agentWizardModel struct {
	step          wizardStep
	nameInput     string
	nameErr       string
	selectedModel string
	backstory     string
	ta            textarea.Model  // for backstory step
	ti            textinput.Model // for name step
	availModels   []string
	modelCursor   int
	width         int

	// MuninnDB memory fields
	muninnEndpoint  string // empty = not configured
	muninnConnected bool
	memoryEnabled   bool
	vaultName       string
	vaultCollision  string // non-empty = auto-proposed collision name
}

// newAgentWizardWithMemory creates an agentWizardModel pre-loaded with MuninnDB connection info.
func newAgentWizardWithMemory(muninnEndpoint string, connected bool) agentWizardModel {
	m := newAgentWizardModel()
	m.muninnEndpoint = muninnEndpoint
	m.muninnConnected = connected
	m.memoryEnabled = connected // default on if MuninnDB is available
	return m
}

func newAgentWizardModel() agentWizardModel {
	ti := textinput.New()
	ti.Placeholder = "e.g. steve"
	ti.Focus()

	ta := textarea.New()
	ta.Placeholder = "What is this agent's purpose and context..."
	ta.SetWidth(60)
	ta.SetHeight(8)

	return agentWizardModel{
		step:        wizStepName,
		ti:          ti,
		ta:          ta,
		availModels: []string{"claude-sonnet-4-6", "claude-opus-4-6", "claude-haiku-4-5-20251001", "qwen3:30b", "qwen3:8b"},
		width:       80,
	}
}

func (m agentWizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEsc:
			return m, func() tea.Msg { return AgentWizardCancelMsg{} }

		case tea.KeyEnter:
			switch m.step {
			case wizStepName:
				name := strings.TrimSpace(m.ti.Value())
				if !isValidAgentName(name) {
					m.nameErr = "Name must be lowercase letters/digits/hyphens/underscores, start with a letter, min 2 chars"
					return m, nil
				}
				m.nameInput = name
				m.nameErr = ""
				m.step = wizStepModel

			case wizStepModel:
				m.selectedModel = m.availModels[m.modelCursor]
				m.step = wizStepBackstory
				m.ta.Focus()

			case wizStepBackstory:
				m.backstory = m.ta.Value()
				m.step = wizStepMemory

			case wizStepMemory:
				if !m.muninnConnected {
					// Enter skips — proceed without memory
					m.memoryEnabled = false
					m.step = wizStepConfirm
					return m, nil
				}
				// Validate vault name
				if m.vaultName == "" {
					m.vaultName = "huginn-" + m.nameInput
				}
				m.step = wizStepConfirm

			case wizStepConfirm:
				enabled := m.memoryEnabled
				def := agents.AgentDef{
					Name:         m.nameInput,
					Model:        m.selectedModel,
					SystemPrompt: m.backstory,
				}
				def.MemoryEnabled = &enabled // always set — &true or &false explicitly
				if m.memoryEnabled && m.vaultName != "" {
					def.VaultName = m.vaultName
				}
				return m, func() tea.Msg { return AgentWizardDoneMsg{Agent: def} }
			}

		case tea.KeyUp:
			if m.step == wizStepModel && m.modelCursor > 0 {
				m.modelCursor--
			}
			if m.step == wizStepMemory && m.muninnConnected {
				m.memoryEnabled = !m.memoryEnabled
			}
		case tea.KeyDown:
			if m.step == wizStepModel && m.modelCursor < len(m.availModels)-1 {
				m.modelCursor++
			}
			if m.step == wizStepMemory && m.muninnConnected {
				m.memoryEnabled = !m.memoryEnabled
			}

		case tea.KeyTab:
			if m.step == wizStepMemory && m.vaultCollision != "" {
				m.vaultName = m.vaultCollision
				m.vaultCollision = ""
			}
		}
	}

	// Delegate input to active widget.
	switch m.step {
	case wizStepName:
		m.ti, cmd = m.ti.Update(msg)
	case wizStepBackstory:
		m.ta, cmd = m.ta.Update(msg)
	}

	return m, cmd
}

func (m agentWizardModel) View() string {
	var sb strings.Builder

	switch m.step {
	case wizStepName:
		sb.WriteString(StyleAccent.Render("Create new agent") + "\n\n")
		sb.WriteString("Agent name (lowercase, e.g. 'steve'):\n")
		sb.WriteString(m.ti.View())
		if m.nameErr != "" {
			sb.WriteString("\n  " + StyleError.Render("! "+m.nameErr))
		}
		sb.WriteString("\n\n" + StyleDim.Render("Enter to continue · Esc to cancel"))

	case wizStepModel:
		sb.WriteString(fmt.Sprintf("%s\n\n", StyleAccent.Render("Agent: "+m.nameInput)))
		sb.WriteString("Choose model:\n\n")
		for i, mod := range m.availModels {
			if i == m.modelCursor {
				sb.WriteString(fmt.Sprintf("  %s %s\n", StyleAccent.Render("▶"), StyleAssistantMsg.Render(mod)))
			} else {
				sb.WriteString(fmt.Sprintf("    %s\n", mod))
			}
		}
		sb.WriteString("\n" + StyleDim.Render("↑↓ move · Enter select · Esc cancel"))

	case wizStepBackstory:
		sb.WriteString(fmt.Sprintf("%s\n\n", StyleAccent.Render(fmt.Sprintf("Agent: %s (%s)", m.nameInput, m.selectedModel))))
		sb.WriteString("Backstory / system prompt:\n\n")
		sb.WriteString(m.ta.View())
		sb.WriteString("\n\n" + StyleDim.Render("Enter to continue · Esc cancel"))

	case wizStepMemory:
		sb.WriteString(fmt.Sprintf("%s\n\n", StyleAccent.Render(fmt.Sprintf("Agent: %s (%s)", m.nameInput, m.selectedModel))))
		sb.WriteString(StyleAccent.Render("Memory") + "\n\n")

		if !m.muninnConnected {
			sb.WriteString("  MuninnDB not connected — agents learn best with persistent memory.\n")
			sb.WriteString("  Download at " + StyleAccent.Render("muninndb.com") + "\n\n")
			sb.WriteString("\n" + StyleDim.Render("↵ Skip for now (uses local Pebble fallback) · Esc cancel"))
			return sb.String()
		}

		// Connected state
		vaultName := m.vaultName
		if vaultName == "" {
			vaultName = "huginn-" + m.nameInput
		}

		enabledStr := "Yes"
		disabledStr := "No"
		if m.memoryEnabled {
			sb.WriteString(fmt.Sprintf("  Enable memory?   %s  %s\n\n", StyleAccent.Render("["+enabledStr+"]"), disabledStr))
		} else {
			sb.WriteString(fmt.Sprintf("  Enable memory?   %s  %s\n\n", enabledStr, StyleAccent.Render("["+disabledStr+"]")))
		}

		if m.memoryEnabled {
			sb.WriteString(fmt.Sprintf("  Vault name:  %s\n", StyleAccent.Render(vaultName)))
			if m.vaultCollision != "" {
				sb.WriteString(fmt.Sprintf("               ↳ %s already exists\n", vaultName))
				sb.WriteString(fmt.Sprintf("               ↳ %s  or  create %s\n",
					StyleDim.Render("use existing"),
					StyleAccent.Render(m.vaultCollision)))
			} else {
				sb.WriteString("               ↳ will be created in MuninnDB\n")
			}
		}

		sb.WriteString("\n" + StyleDim.Render("↑↓ toggle memory · Tab use proposed vault · ↵ continue · Esc cancel"))

	case wizStepConfirm:
		sb.WriteString(StyleAccent.Render("Confirm new agent:") + "\n\n")
		sb.WriteString(fmt.Sprintf("  Name:    %s\n", m.nameInput))
		sb.WriteString(fmt.Sprintf("  Model:   %s\n", m.selectedModel))
		backstory := m.backstory
		if len(backstory) > 80 {
			backstory = backstory[:77] + "..."
		}
		sb.WriteString(fmt.Sprintf("  Purpose: %s\n", backstory))
		if m.memoryEnabled && m.vaultName != "" {
			sb.WriteString(fmt.Sprintf("  Memory:  vault %s\n", m.vaultName))
		} else {
			sb.WriteString("  Memory:  Pebble (local fallback)\n")
		}
		sb.WriteString("\n" + StyleDim.Render("Enter to create · Esc cancel"))
	}

	return sb.String()
}

func (m agentWizardModel) Init() tea.Cmd {
	return textinput.Blink
}

// StandaloneAgentWizard is an exported tea.Model wrapper around agentWizardModel
// for use as a standalone tea.Program (e.g. `huginn agents new`).
// It records the final agent on confirm and calls tea.Quit on done/cancel.
type StandaloneAgentWizard struct {
	inner agentWizardModel
	done  bool
	saved bool
	agent agents.AgentDef
}

// NewStandaloneAgentWizard returns a StandaloneAgentWizard ready for tea.NewProgram.
func NewStandaloneAgentWizard() StandaloneAgentWizard {
	return StandaloneAgentWizard{inner: newAgentWizardModel()}
}

// NewStandaloneAgentWizardWithMemory creates a standalone wizard pre-loaded with MuninnDB info.
func NewStandaloneAgentWizardWithMemory(muninnEndpoint string, connected bool) StandaloneAgentWizard {
	return StandaloneAgentWizard{inner: newAgentWizardWithMemory(muninnEndpoint, connected)}
}

func (s StandaloneAgentWizard) Init() tea.Cmd { return s.inner.Init() }

func (s StandaloneAgentWizard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case AgentWizardDoneMsg:
		s.done = true
		s.saved = true
		s.agent = m.Agent
		return s, tea.Quit
	case AgentWizardCancelMsg:
		s.done = true
		s.saved = false
		return s, tea.Quit
	}
	inner, cmd := s.inner.Update(msg)
	s.inner = inner.(agentWizardModel)
	return s, cmd
}

func (s StandaloneAgentWizard) View() string { return s.inner.View() }

// IsDone returns true if the wizard finished (confirmed or cancelled).
func (s StandaloneAgentWizard) IsDone() bool { return s.done }

// WasSaved returns true if the user confirmed agent creation.
func (s StandaloneAgentWizard) WasSaved() bool { return s.saved }

// SavedAgent returns the agent that was confirmed, or a zero value.
func (s StandaloneAgentWizard) SavedAgent() agents.AgentDef { return s.agent }
