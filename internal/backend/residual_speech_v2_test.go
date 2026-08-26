package backend

import (
	"strings"
	"testing"
)

// liveWinstonResidualV2 is the 2026-08-26 huginn-dev161 S5 speech turn after
// delegate_to_agent + wait_for_threads had already run (Reggie: PONG). The
// tool JSON carries a // comment (invalid JSON, so 161 left it in place) and
// the answer is followed by echoed result fragments.
const liveWinstonResidualV2 = "After Reggie responds with PONG:\n\n{\n  \"name\": \"recall_thread_result\",\n  \"arguments\": {\n    \"thread_id\": \"thread-12345\"  // Replace with the actual thread ID\n  }\n}\n\n7 times 8 is 56.\"PONG\"\n56PONG\n56"

func TestVisibleAssistantContentAfterTools_LiveWinstonResidualV2(t *testing.T) {
	got := VisibleAssistantContentAfterTools(liveWinstonResidualV2)
	if got != "7 times 8 is 56." {
		t.Fatalf("residual leaked into speech: %q", got)
	}
}

func TestPromoteGranted_CommentedToolJSONNeverExecutes(t *testing.T) {
	// Even when recall_thread_result is a granted tool, a placeholder object
	// with a // comment is not valid JSON and must never be promoted.
	resp := &ChatResponse{Content: liveWinstonResidualV2, DoneReason: "stop"}
	PromoteGrantedContentToolCalls(resp, []Tool{{Function: ToolFunction{Name: "recall_thread_result"}}})
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("commented placeholder promoted: %+v", resp.ToolCalls)
	}
	PromoteContentToolCalls(resp)
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("commented placeholder promoted by lone-object path: %+v", resp.ToolCalls)
	}
}

func TestStripResidualSpeech_WaitGlueVariants(t *testing.T) {
	for _, glue := range []string{
		"After Reggie responds with PONG:",
		"When Reggie replies:",
		"Once Reggie comes back with the result:",
		"After Reggie responds,",
		"When the thread finishes:",
	} {
		got := StripResidualSpeech(glue + "\n7 times 8 is 56.")
		if got != "7 times 8 is 56." {
			t.Errorf("%q not treated as wait-glue: %q", glue, got)
		}
	}
	// Real teammate sentences that merely start with a conjunction stay.
	for _, keep := range []string{
		"Once the migration has finished, the table will have 3 columns.",
		"After lunch I'll pick this up.",
		"When you're ready, say go.",
	} {
		if got := StripResidualSpeechAfterTools(keep); got != keep {
			t.Errorf("prose %q rewritten to %q", keep, got)
		}
	}
}

func TestStripResidualSpeechAfterTools_EchoFragments(t *testing.T) {
	// Trailing echo lines are only dropped when every fragment already
	// appeared earlier in the message; a fresh number is an answer.
	if got := StripResidualSpeechAfterTools("7 times 8 is:\n56"); got != "7 times 8 is:\n56" {
		t.Fatalf("fresh answer line dropped: %q", got)
	}
	if got := StripResidualSpeechAfterTools("Reggie said PONG. 7 times 8 is 56.\n56PONG\n56"); got != "Reggie said PONG. 7 times 8 is 56." {
		t.Fatalf("echo lines kept: %q", got)
	}
	// A glued JSON string after sentence punctuation goes; a quoted word
	// after a space is speech.
	if got := StripResidualSpeechAfterTools("The answer is 56.\"PONG\""); got != "The answer is 56." {
		t.Fatalf("glued string kept: %q", got)
	}
	keep := "Reggie said \"PONG\" back."
	if got := StripResidualSpeechAfterTools(keep); got != keep {
		t.Fatalf("quoted speech rewritten: %q", got)
	}
}

func TestStripResidualSpeechAfterTools_CommentedJSONKeepsGoFence(t *testing.T) {
	content := "Here:\n```go\n// add returns a+b\nfunc add(a, b int) int { return a + b }\n```\nAfter Reggie responds with PONG:\n{\"name\": \"bash\", \"arguments\": {\"command\": \"ls\" // list\n}}\nThat compiles."
	got := VisibleAssistantContentAfterTools(content)
	want := "Here:\n```go\n// add returns a+b\nfunc add(a, b int) int { return a + b }\n```\nThat compiles."
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if strings.Contains(got, "\"name\"") {
		t.Fatalf("commented tool JSON leaked: %q", got)
	}
}
