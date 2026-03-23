package agents

import (
	"strings"
	"testing"
	"time"
)

func TestBuildPersonaPromptWithMemory_NoSummaries(t *testing.T) {
	ag := &Agent{Name: "Mark"}
	result := BuildPersonaPromptWithMemory(ag, "context text", nil)
	base := BuildPersonaPrompt(ag, "context text")
	if result != base {
		t.Errorf("expected prompt with no summaries to equal base prompt\ngot: %q\nwant: %q", result, base)
	}
}

func TestBuildPersonaPromptWithMemory_WithSummaries_ContainsSection(t *testing.T) {
	ag := &Agent{Name: "Mark"}
	summaries := []SessionSummary{
		{
			SessionID: "sess-1",
			AgentName: "Mark",
			Timestamp: time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC),
			Summary:   "Refactored the auth module",
		},
	}
	result := BuildPersonaPromptWithMemory(ag, "context text", summaries)
	if !strings.Contains(result, "## Recent Work Context") {
		t.Error("expected result to contain '## Recent Work Context' section")
	}
	if !strings.Contains(result, "Refactored the auth module") {
		t.Error("expected result to contain summary text")
	}
	if !strings.Contains(result, "2024-06-15") {
		t.Error("expected result to contain formatted date")
	}
}

func TestBuildPersonaPromptWithMemory_MultipleSummaries(t *testing.T) {
	ag := &Agent{Name: "Chris"}
	summaries := []SessionSummary{
		{
			SessionID:    "sess-1",
			AgentName:    "Chris",
			Timestamp:    time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC),
			Summary:      "Worked on database layer",
			FilesTouched: []string{"db.go", "schema.go"},
		},
		{
			SessionID:     "sess-2",
			AgentName:     "Chris",
			Timestamp:     time.Date(2024, 1, 11, 0, 0, 0, 0, time.UTC),
			Summary:       "Refactored query builder",
			OpenQuestions: []string{"add indexes?", "cache queries?"},
		},
	}
	result := BuildPersonaPromptWithMemory(ag, "ctx", summaries)

	if !strings.Contains(result, "Worked on database layer") {
		t.Error("missing first summary text")
	}
	if !strings.Contains(result, "Refactored query builder") {
		t.Error("missing second summary text")
	}
	if !strings.Contains(result, "db.go") {
		t.Error("missing files touched")
	}
	if !strings.Contains(result, "add indexes?") {
		t.Error("missing open questions")
	}
}

func TestBuildPersonaPromptWithMemory_EmptySlice(t *testing.T) {
	ag := &Agent{Name: "Odin"}
	result := BuildPersonaPromptWithMemory(ag, "context", []SessionSummary{})
	base := BuildPersonaPrompt(ag, "context")
	if result != base {
		t.Errorf("expected empty summaries to return base prompt\ngot: %q\nwant: %q", result, base)
	}
}

func TestBuildPersonaPromptWithMemory_SummaryAfterPersona(t *testing.T) {
	ag := &Agent{Name: "Frigg", SystemPrompt: "You are Frigg, goddess of wisdom."}
	summaries := []SessionSummary{
		{
			SessionID: "sess-1",
			AgentName: "Frigg",
			Timestamp: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
			Summary:   "Advised on architecture decisions",
		},
	}
	result := BuildPersonaPromptWithMemory(ag, "repo context", summaries)

	// Persona should come before the Recent Work Context section
	personaIdx := strings.Index(result, "You are Frigg")
	recentIdx := strings.Index(result, "## Recent Work Context")
	if personaIdx == -1 {
		t.Error("expected persona text in result")
	}
	if recentIdx == -1 {
		t.Error("expected Recent Work Context section in result")
	}
	if personaIdx >= recentIdx {
		t.Errorf("expected persona to appear before Recent Work Context (persona=%d, recent=%d)", personaIdx, recentIdx)
	}
}
