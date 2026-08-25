package claudecode

import (
	"strings"
	"testing"
)

func TestAssembleSystemPromptIncludesEveryPart(t *testing.T) {
	got := AssembleSystemPrompt(
		"You are Elena. Be terse.",
		[]string{"## Skill: code-review\nAlways check error handling.", "## Skill: testing\nTDD."},
		"Project note: this repo uses Go 1.25.",
	)
	for _, want := range []string{
		"You are Elena. Be terse.",
		"code-review",
		"Always check error handling.",
		"TDD.",
		"Project note: this repo uses Go 1.25.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("assembled prompt missing %q:\n%s", want, got)
		}
	}
}

func TestAssembleSystemPromptSkipsEmptySections(t *testing.T) {
	got := AssembleSystemPrompt("Just the prompt.", nil, "")
	if strings.Contains(got, "Skills") || strings.Contains(got, "Notepad") {
		t.Errorf("empty sections must not produce headings:\n%s", got)
	}
	if strings.TrimSpace(got) != "Just the prompt." {
		t.Errorf("got %q, want exactly the prompt with no decoration", got)
	}
}

func TestAssembleSystemPromptEmptyEverything(t *testing.T) {
	if got := AssembleSystemPrompt("", nil, ""); got != "" {
		t.Errorf("got %q, want empty string so --append-system-prompt can be omitted", got)
	}
}
