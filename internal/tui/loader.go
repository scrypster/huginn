package tui

import (
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/scrypster/huginn/internal/repo"
)

// IndexResult carries the finished index (or error) back to main.
type IndexResult struct {
	Index *repo.Index
	Err   error
}

// progressMsg is sent for each file indexed.
type progressMsg struct {
	done, total int
	path        string
}

// indexDoneMsg signals indexing is complete.
type indexDoneMsg struct {
	idx *repo.Index
	err error
}

// loaderModel is the Bubble Tea model for the loading screen.
type loaderModel struct {
	dir      string
	progress progress.Model
	done     int
	total    int
	current  string
	width    int
	result   *IndexResult
}

func newLoaderModel(dir string) loaderModel {
	p := progress.New(
		progress.WithGradient("#BB86FC", "#58A6FF"),
		progress.WithoutPercentage(),
	)
	return loaderModel{dir: dir, progress: p}
}

func (m loaderModel) Init() tea.Cmd {
	// Indexing is driven by the goroutine in RunLoader, which calls p.Send().
	// Init must return nil — returning a Cmd here would fire an empty
	// indexDoneMsg{} immediately and quit the program before indexing completes.
	return nil
}

// RunLoader runs a full-screen progress bar while indexing dir.
// Returns the completed Index (or an empty one on error).
func RunLoader(dir string) *repo.Index {
	m := newLoaderModel(dir)
	p := tea.NewProgram(m, tea.WithAltScreen())

	// Kick off indexing in a goroutine that sends progress events to the program.
	go func() {
		idx, err := repo.Build(dir, func(done, total int, path string) {
			p.Send(progressMsg{done: done, total: total, path: path})
		})
		p.Send(indexDoneMsg{idx: idx, err: err})
	}()

	final, _ := p.Run()
	if lm, ok := final.(loaderModel); ok && lm.result != nil {
		if lm.result.Err != nil {
			return &repo.Index{Root: dir}
		}
		if lm.result.Index != nil {
			return lm.result.Index
		}
	}
	return &repo.Index{Root: dir}
}

func (m loaderModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.progress.Width = msg.Width - 8

	case progressMsg:
		m.done = msg.done
		m.total = msg.total
		m.current = msg.path

	case indexDoneMsg:
		m.result = &IndexResult{Index: msg.idx, Err: msg.err}
		return m, tea.Quit
	}

	// progress.Model.Update returns (tea.Model, tea.Cmd) — assert back.
	updated, cmd := m.progress.Update(msg)
	if pm, ok := updated.(progress.Model); ok {
		m.progress = pm
	}
	return m, cmd
}

func (m loaderModel) View() string {
	if m.width == 0 {
		return ""
	}

	title := StyleAccent.Render("  HUGINN") + StyleDim.Render(" — indexing repository…")

	var bar string
	if m.total > 0 {
		pct := float64(m.done) / float64(m.total)
		bar = "  " + m.progress.ViewAs(pct)
	} else {
		// Walk mode: unknown total — show done count as an indeterminate pulse.
		pct := indeterminatePct(m.done)
		bar = "  " + m.progress.ViewAs(pct)
	}

	var stats string
	if m.total > 0 {
		stats = StyleDim.Render(fmt.Sprintf("  %d / %d files", m.done, m.total))
	} else if m.done > 0 {
		stats = StyleDim.Render(fmt.Sprintf("  %d files…", m.done))
	}

	var cur string
	if m.current != "" {
		name := filepath.Base(m.current)
		dir := filepath.Dir(m.current)
		if dir == "." {
			dir = ""
		}
		cur = StyleDim.Render("  " + dir + "/") + StyleDim.Render(name)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		"",
		title,
		"",
		bar,
		stats,
		cur,
	)
}

// indeterminatePct maps a monotonically increasing counter to a 0→1 bounce
// so the bar slides forward even when total is unknown.
func indeterminatePct(n int) float64 {
	// Cycle through 0→1 every 100 files.
	v := float64(n%100) / 100.0
	return v
}
