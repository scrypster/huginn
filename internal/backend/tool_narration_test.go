package backend

import "testing"

// liveWinstonMuninnNarration is the 2026-08-27 14b muninn-tools Winston DM
// repro: "what is our production database named?" persisted with the
// literal tool name and a plan narration sentence still in the speech
// channel, alongside the real answer.
const liveWinstonMuninnNarration = "To answer your question, I need to consult our internal memory. " +
	"I'll use the muninn_recall function to search for information about our production database. " +
	"Our production database is named yggdrasil."

func TestStripResidualSpeechAfterTools_ToolPlanNarrationStripped(t *testing.T) {
	got := StripResidualSpeechAfterTools(liveWinstonMuninnNarration)
	if got != "Our production database is named yggdrasil." {
		t.Fatalf("tool-plan narration leaked into speech: %q", got)
	}
}

func TestPersistVisibleAssistantContent_ToolPlanNarrationStripped(t *testing.T) {
	got := PersistVisibleAssistantContent(liveWinstonMuninnNarration, "what is our production database named?")
	if got != "Our production database is named yggdrasil." {
		t.Fatalf("tool-plan narration leaked into persisted speech: %q", got)
	}
}

func TestVisibleAssistantContent_ToolPlanNarrationStrippedDuringStreaming(t *testing.T) {
	// The base (pre-tools) streaming filter must also hide plan narration —
	// a model that announces its tool plan before the tool call executes
	// should never show that plan to the user.
	got := VisibleAssistantContent(liveWinstonMuninnNarration)
	if got != "Our production database is named yggdrasil." {
		t.Fatalf("tool-plan narration leaked into streamed speech: %q", got)
	}
}

func TestVisibleAssistantContentAfterTools_ToolPlanNarrationNoClip(t *testing.T) {
	// No mid-word/mid-phrase clipping: the answer sentence survives intact,
	// with no dangling leading fragment ("answer your question, ...").
	got := VisibleAssistantContentAfterTools(liveWinstonMuninnNarration, "what is our production database named?")
	if got != "Our production database is named yggdrasil." {
		t.Fatalf("got %q", got)
	}
	if got != "" && got[0] >= 'a' && got[0] <= 'z' {
		t.Errorf("leading word clipped, speech starts lowercase: %q", got)
	}
}

func TestStripResidualSpeechAfterTools_ToolPlanNarrationVariants(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "let me check the tool",
			in:   "Let me check the muninn_recall tool for that. Our staging server is valkyrie.",
			want: "Our staging server is valkyrie.",
		},
		{
			name: "searching my memory for",
			in:   "Searching my memory for the answer. It's yggdrasil.",
			want: "It's yggdrasil.",
		},
		{
			name: "I need to consult the memory",
			in:   "I need to consult the memory to be sure. It's yggdrasil.",
			want: "It's yggdrasil.",
		},
		{
			name: "delegate_to_agent narration",
			in:   "I'll use the delegate_to_agent tool to ask Reggie. Reggie said PONG.",
			want: "Reggie said PONG.",
		},
		{
			name: "wait_for_threads narration",
			in:   "Let me call wait_for_threads to check on that. 7 times 8 is 56.",
			want: "7 times 8 is 56.",
		},
		{
			name: "no narration present, untouched",
			in:   "Our production database is named yggdrasil.",
			want: "Our production database is named yggdrasil.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := StripResidualSpeechAfterTools(tc.in)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStripResidualSpeechAfterTools_ToolPlanNarrationOnlyFillsWithNothingLeft(t *testing.T) {
	// If narration stripping would leave nothing, leave the leftover-empty
	// handling to the existing persist/fill chain rather than fabricating
	// an answer here.
	in := "I'll use the muninn_recall function to search for that."
	got := StripResidualSpeechAfterTools(in)
	if got != "" {
		t.Fatalf("expected pure narration to strip to empty, got %q", got)
	}
}
