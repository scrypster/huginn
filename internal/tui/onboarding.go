package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/scrypster/huginn/internal/models"
	"github.com/scrypster/huginn/internal/runtime"
)

type onboardingState int

const (
	onboardDownloadRuntime onboardingState = iota
	onboardModelSelect
	onboardDownloadModel
	onboardDone
)

// onboardProgressMsg carries download progress updates.
type onboardProgressMsg struct {
	downloaded int64
	total      int64
	speed      float64
	done       bool
	err        error
	phase      string // "runtime" or "model"
}

// OnboardingModel is the Bubble Tea model for first-run setup.
type OnboardingModel struct {
	width  int
	height int
	state  onboardingState
	err    error

	// Runtime download
	runtimeMgr *runtime.Manager
	runtimeDL  onboardProgressMsg
	runtimeCh  chan onboardProgressMsg

	// Model selection
	catalog   map[string]models.ModelEntry
	menuItems []string
	cursor    int
	ramGB     int

	// Model download
	selectedModel string
	modelDL       onboardProgressMsg
	modelCh       chan onboardProgressMsg
	modelStore    *models.Store
}

// NewOnboarding creates the onboarding model.
func NewOnboarding(mgr *runtime.Manager, store *models.Store, ramGB int) *OnboardingModel {
	return &OnboardingModel{
		runtimeMgr: mgr,
		modelStore: store,
		ramGB:      ramGB,
		state:      onboardDownloadRuntime,
	}
}

// Init satisfies tea.Model — kicks off the runtime download immediately.
func (m *OnboardingModel) Init() tea.Cmd {
	return m.startRuntimeDownload()
}

func (m *OnboardingModel) startRuntimeDownload() tea.Cmd {
	ch := make(chan onboardProgressMsg, 64)
	m.runtimeCh = ch
	go func() {
		err := m.runtimeMgr.Download(context.Background(), func(dl, total int64) {
			ch <- onboardProgressMsg{downloaded: dl, total: total, phase: "runtime"}
		})
		ch <- onboardProgressMsg{done: true, err: err, phase: "runtime"}
	}()
	return pollProgressCmd(ch)
}

// pollProgressCmd returns a tea.Cmd that reads one message from ch and returns it.
func pollProgressCmd(ch chan onboardProgressMsg) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}

func (m *OnboardingModel) loadCatalog() tea.Cmd {
	return func() tea.Msg {
		catalog, err := models.LoadMerged()
		if err != nil {
			return onboardProgressMsg{err: err, phase: "runtime"}
		}
		m.catalog = catalog

		// Build menu: models that fit in user's RAM
		var items []string
		for name, e := range catalog {
			if e.MinRAMGB <= m.ramGB || m.ramGB == 0 {
				items = append(items, name)
			}
		}
		if len(items) == 0 {
			for name := range catalog {
				items = append(items, name)
			}
		}
		sort.Strings(items)
		m.menuItems = items
		return nil
	}
}

func (m *OnboardingModel) startModelDownload() tea.Cmd {
	ch := make(chan onboardProgressMsg, 64)
	m.modelCh = ch
	go func() {
		entry := m.catalog[m.selectedModel]
		destPath := m.modelStore.ModelPath(entry.Filename)
		err := models.Pull(context.Background(), entry.URL, destPath, entry.SHA256, func(p models.PullProgress) {
			ch <- onboardProgressMsg{downloaded: p.Downloaded, total: p.Total, phase: "model"}
		})
		ch <- onboardProgressMsg{done: true, err: err, phase: "model"}
	}()
	return pollProgressCmd(ch)
}

// Update satisfies tea.Model.
func (m *OnboardingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case onboardProgressMsg:
		if msg.phase == "runtime" {
			m.runtimeDL = msg
			if msg.err != nil {
				m.err = msg.err
				return m, nil
			}
			if msg.done {
				m.state = onboardModelSelect
				return m, m.loadCatalog()
			}
			// Not done yet — re-arm the poll so we keep receiving progress.
			return m, pollProgressCmd(m.runtimeCh)
		} else if msg.phase == "model" {
			m.modelDL = msg
			if msg.err != nil {
				m.err = msg.err
				return m, nil
			}
			if msg.done {
				m.state = onboardDone
				return m, tea.Quit
			}
			// Not done yet — re-arm the poll so we keep receiving progress.
			return m, pollProgressCmd(m.modelCh)
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		}
		if m.state == onboardModelSelect {
			switch msg.String() {
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < len(m.menuItems)-1 {
					m.cursor++
				}
			case "enter":
				if len(m.menuItems) > 0 {
					m.selectedModel = m.menuItems[m.cursor]
					m.state = onboardDownloadModel
					return m, m.startModelDownload()
				}
			}
		}
	}
	return m, nil
}

// View satisfies tea.Model.
func (m *OnboardingModel) View() string {
	if m.width == 0 {
		return "Setting up Huginn…"
	}

	var body strings.Builder
	body.WriteString("\n")
	body.WriteString(StyleAccent.Render("  Welcome to Huginn"))
	body.WriteString("\n\n")

	switch m.state {
	case onboardDownloadRuntime:
		body.WriteString(StyleDim.Render("  Setting up your local AI runtime…"))
		body.WriteString("\n")
		if m.runtimeDL.total > 0 {
			pct := int(float64(m.runtimeDL.downloaded) / float64(m.runtimeDL.total) * 100)
			body.WriteString(StyleThinking.Render(fmt.Sprintf("  Downloading… %d%%", pct)))
		} else {
			body.WriteString(StyleThinking.Render("  Downloading…"))
		}
		body.WriteString("\n")

	case onboardModelSelect:
		body.WriteString(StyleDim.Render("  Choose a starter model:"))
		body.WriteString("\n\n")
		for i, name := range m.menuItems {
			entry := m.catalog[name]
			sizeStr := ""
			if entry.SizeBytes > 0 {
				sizeStr = fmt.Sprintf("  %.1fG", float64(entry.SizeBytes)/float64(1024*1024*1024))
			}
			if i == m.cursor {
				line := fmt.Sprintf("  %s%s%s", StyleAccent.Render("▸ "), StyleAssistantMsg.Render(name), StyleDim.Render(sizeStr))
				body.WriteString(line)
				body.WriteString("\n")
				if entry.Description != "" {
					body.WriteString(StyleDim.Render(fmt.Sprintf("      %s", entry.Description)))
					body.WriteString("\n")
				}
			} else {
				line := fmt.Sprintf("    %s%s", name, StyleDim.Render(sizeStr))
				body.WriteString(line)
				body.WriteString("\n")
			}
			body.WriteString("\n")
		}
		body.WriteString(StyleDim.Render("  [↑↓] Navigate  [Enter] Select  [q] Quit"))
		body.WriteString("\n")

	case onboardDownloadModel:
		body.WriteString(StyleDim.Render(fmt.Sprintf("  Downloading %s…", m.selectedModel)))
		body.WriteString("\n")
		if m.modelDL.total > 0 {
			pct := int(float64(m.modelDL.downloaded) / float64(m.modelDL.total) * 100)
			body.WriteString(StyleThinking.Render(fmt.Sprintf("  Please wait… %d%%", pct)))
		} else {
			body.WriteString(StyleThinking.Render("  Please wait…"))
		}
		body.WriteString("\n")
		body.WriteString(StyleDim.Render("  [ctrl+c] Cancel"))
		body.WriteString("\n")

	case onboardDone:
		body.WriteString(StyleAssistantMsg.Render("  Setup complete! Starting Huginn…"))
		body.WriteString("\n")
	}

	if m.err != nil {
		body.WriteString("\n")
		body.WriteString(StyleError.Render(fmt.Sprintf("  Error: %v", m.err)))
		body.WriteString("\n")
		body.WriteString(StyleDim.Render("  [q] Quit"))
		body.WriteString("\n")
	}

	footer := StyleStatusBar.Width(m.width).Render(" huginn — first run")

	return lipgloss.JoinVertical(lipgloss.Left, body.String(), footer)
}

// IsDone returns true when onboarding is complete.
func (m *OnboardingModel) IsDone() bool { return m.state == onboardDone }

// SelectedModel returns the name of the model chosen during onboarding.
func (m *OnboardingModel) SelectedModel() string { return m.selectedModel }
