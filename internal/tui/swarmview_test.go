package tui

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/swarm"
)

func TestSwarmView_RenderOverview_ShowsAllAgents(t *testing.T) {
	sv := NewSwarmViewModel(80, 20)
	sv.AddAgent("a1", "Architect", "#58A6FF")
	sv.AddAgent("a2", "Coder", "#3FB950")
	sv.SetStatus("a1", swarm.StatusThinking)
	sv.SetToolName("a2", "edit_file")

	out := sv.View()
	if !strings.Contains(out, "Architect") {
		t.Error("expected Architect")
	}
	if !strings.Contains(out, "Coder") {
		t.Error("expected Coder")
	}
	if !strings.Contains(out, "edit_file") {
		t.Error("expected tool name")
	}
}

func TestSwarmView_FocusMode_ShowsOnlyFocused(t *testing.T) {
	sv := NewSwarmViewModel(80, 20)
	sv.AddAgent("a1", "Architect", "#58A6FF")
	sv.AddAgent("a2", "Coder", "#3FB950")
	sv.AppendOutput("a1", "Planning the structure...")
	sv.AppendOutput("a2", "Writing handler code...")
	sv.SetFocus("a1")

	out := sv.View()
	if !strings.Contains(out, "Planning") {
		t.Error("expected a1 output")
	}
	if strings.Contains(out, "Writing handler") {
		t.Error("unexpected a2 content")
	}
}

func TestSwarmView_SpinnerAdvances(t *testing.T) {
	sv := NewSwarmViewModel(80, 20)
	sv.AddAgent("a1", "A", "#58A6FF")
	sv.SetStatus("a1", swarm.StatusThinking)

	f0 := sv.SpinnerFrame()
	sv.TickSpinner()
	f1 := sv.SpinnerFrame()
	sv.TickSpinner()
	f2 := sv.SpinnerFrame()

	frames := "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
	for _, f := range []string{f0, f1, f2} {
		if !strings.ContainsRune(frames, []rune(f)[0]) {
			t.Errorf("invalid frame: %q", f)
		}
	}

	if f0 == f1 && f1 == f2 {
		t.Error("spinner did not advance")
	}
}

func TestSwarmView_FooterSummary(t *testing.T) {
	sv := NewSwarmViewModel(80, 20)
	sv.AddAgent("a1", "A", "#58A6FF")
	sv.AddAgent("a2", "B", "#3FB950")
	sv.SetStatus("a1", swarm.StatusThinking)
	sv.SetStatus("a2", swarm.StatusDone)

	out := sv.View()
	if !strings.Contains(out, "2 agents") {
		t.Errorf("expected '2 agents' in footer: %s", out)
	}
}
