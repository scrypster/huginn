package backend

import (
	"strings"
	"testing"
)

// liveWinstonResidual is the 2026-08-26 14b CoS agentOutput on the 160
// binary: delegate_to_agent and wait_for_threads had already executed
// (Reggie replied PONG), yet the speech channel still carried the playbook.
const liveWinstonResidual = "<wait for Reggie to finish>\nOnce Reggie has finished:\nThen calculate:\n7 times 8 is 56.{\n  \"pong_response\": \"PONG\",\n  \"multiplication_result\": \"56\"\n}"

func TestVisibleAssistantContentAfterTools_LiveWinstonResidual(t *testing.T) {
	got := VisibleAssistantContentAfterTools(liveWinstonResidual)
	if got != "7 times 8 is 56." {
		t.Fatalf("residual playbook leaked into speech: %q", got)
	}
}

func TestVisibleAssistantContentAfterTools_ProseWithResidualJSON(t *testing.T) {
	content := "Reggie said PONG. 7 times 8 is 56.\n" +
		`{"name":"recall_thread_result","arguments":{"thread_id":"<thread_id>"}}` + "\n" +
		`{"pong_response":"PONG","multiplication_result":"56"}`
	got := VisibleAssistantContentAfterTools(content)
	if got != "Reggie said PONG. 7 times 8 is 56." {
		t.Fatalf("got %q", got)
	}
	for _, leak := range []string{"recall_thread_result", "pong_response", "{", "}"} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
}

func TestVisibleAssistantContentAfterTools_InventedToolJSONStrippedNotPromoted(t *testing.T) {
	// An invented name must be stripped from speech, and the promotion path
	// must not lift it into a tool call (it is not granted).
	content := "Done.\n" + `{"name":"recall_thread_result","arguments":{"thread_id":"<thread_id>"}}`
	resp := &ChatResponse{Content: content, DoneReason: "stop"}
	PromoteGrantedContentToolCalls(resp, []Tool{{Function: ToolFunction{Name: "bash"}}})
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("invented tool was promoted: %+v", resp.ToolCalls)
	}
	if got := VisibleAssistantContentAfterTools(resp.Content); got != "Done." {
		t.Fatalf("got %q", got)
	}
}

func TestVisibleAssistantContentAfterTools_KeepsGoFence(t *testing.T) {
	content := "Here is the helper:\n```go\nfunc add(a, b int) int { return a + b }\n```\n<wait for Reggie to finish>\nThat compiles."
	got := VisibleAssistantContentAfterTools(content)
	want := "Here is the helper:\n```go\nfunc add(a, b int) int { return a + b }\n```\nThat compiles."
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestVisibleAssistantContentAfterTools_ResultOnlyMessageKept(t *testing.T) {
	// Nothing but a result object: leave it rather than blank the answer.
	only := `{"pong_response":"PONG"}`
	if got := VisibleAssistantContentAfterTools(only); got != only {
		t.Fatalf("lone result object should survive, got %q", got)
	}
}

func TestVisibleAssistantContentAfterTools_NestedJSONSampleKept(t *testing.T) {
	content := "Use this config: {\"server\": {\"port\": 8080}} and restart."
	if got := VisibleAssistantContentAfterTools(content); got != content {
		t.Fatalf("nested sample rewritten: %q", got)
	}
}

func TestVisibleAssistantContentAfterTools_NoHarnessNamesAsSpeech(t *testing.T) {
	content := "wait_for_threads\nTOOL_FAIL: nope\n<wait_for_threads>\nReggie said PONG.\nrecall_thread_result\nbash"
	if got := VisibleAssistantContentAfterTools(content); got != "Reggie said PONG." {
		t.Fatalf("got %q", got)
	}
}

func TestVisibleAssistantContent_StripsWaitTagKeepsSentenceJSON(t *testing.T) {
	// Base filter: wait residue goes, but in-sentence JSON (no tool ran) stays.
	content := "<wait for Reggie to finish>\nSure, run {\"name\": \"bash\", \"arguments\": {\"command\": \"hostname\"}}"
	want := "Sure, run {\"name\": \"bash\", \"arguments\": {\"command\": \"hostname\"}}"
	if got := VisibleAssistantContent(content); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestStripResidualSpeech_GlueChainOnly(t *testing.T) {
	content := "Then run the tests:\n```sh\ngo test ./...\n```\nAfter that, we ship."
	// "Then …:" is only glue when it continues a wait/once line; standalone it stays.
	if got := StripResidualSpeech(content); got != content {
		t.Fatalf("standalone Then-line rewritten: %q", got)
	}
	glued := "Once Reggie has finished:\nThen calculate:\nNext, add them up:\nThe total is 4."
	if got := StripResidualSpeech(glued); got != "The total is 4." {
		t.Fatalf("got %q", got)
	}
}

func TestStripResidualSpeech_KeepsTeammateProse(t *testing.T) {
	for _, s := range []string{
		"Reggie said PONG. 7 times 8 is 56.",
		"I'll wait for your go-ahead before deploying.",
		"Once the migration has finished, the table will have 3 columns.",
		"When you're done, ping me.",
	} {
		if got := StripResidualSpeech(s); got != s {
			t.Errorf("prose rewritten: %q -> %q", s, got)
		}
	}
}

func TestStripResidualSpeechAfterTools_RemovesFillerLines(t *testing.T) {
	for _, tc := range []struct {
		input, want string
	}{
		{
			"Reggie replied PONG.\nHow can I assist you further?",
			"Reggie replied PONG.",
		},
		{
			"Here is the result:\nNot currently delegating any tasks.",
			"Here is the result:",
		},
		{
			"I'll calculate: 7 times 8 is 56.\nIs there anything else I can help with?",
			"I'll calculate: 7 times 8 is 56.",
		},
		{
			"How can I assist you further?",
			"",
		},
		{
			"Not currently delegating any tasks.",
			"",
		},
		{
			"Hello, how can I help today?", // kept — not a trailing filler phrase
			"Hello, how can I help today?",
		},
	} {
		got := StripResidualSpeechAfterTools(tc.input)
		if got != tc.want {
			t.Errorf("StripResidualSpeechAfterTools(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestStripResidualSpeechAfterTools_PlaceholdersAndSameLineFragments(t *testing.T) {
	// 2026-08-26 live S5 leftover with two key issues:
	// 1. Invalid JSON with placeholders not dropped
	// 2. Same-line glued fragments not stripped
	// With a playbook wait-glue line that's already handled
	const live = "After Reggie responds with PONG:\n" +
		"{\"name\":\"recall_thread_result\",\"arguments\":{\"thread_id\":<thread_id>}}\n" +
		"Reggie's response: PONG\n\n7 times 8 is 56.PONG, 56"

	got := StripResidualSpeechAfterTools(live)
	want := "Reggie's response: PONG\n\n7 times 8 is 56."

	if got != want {
		t.Errorf("StripResidualSpeechAfterTools() = %q\nwant %q", got, want)
	}

	// Verify no leaks.
	for _, leak := range []string{"recall_thread_result", "thread_id", "placeholder", "PONG,"} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
}

func TestStripResidualSpeechAfterTools_LiveS5CommentFences(t *testing.T) {
	// Unlabeled comment fence with tool JSON should be dropped entirely.
	const live = "// Wait until Reggie responds\n" +
		"{\"name\": \"recall_thread_result\", \"arguments\": {\"thread_id\": <thread-id>}}\n" +
		"// Assume Reggie's response is stored in a variable called reggie_response\n" +
		"Reggie's response: PONG\n\n7 times 8 is 56."

	got := StripResidualSpeechAfterTools("```\n" + live + "\n```")
	want := "Reggie's response: PONG\n\n7 times 8 is 56."

	if got != want {
		t.Errorf("StripResidualSpeechAfterTools() = %q\nwant %q", got, want)
	}

	// Verify no leaks.
	for _, leak := range []string{"recall_thread_result", "Wait until", "//"} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
}

func TestStripResidualSpeechAfterTools_StageDirections(t *testing.T) {
	// Parenthetical stage direction lines should be dropped.
	tests := []struct {
		input string
		want  string
	}{
		{
			"Reggie's response: PONG\n(awaits Reggie's response)\n7 times 8 is 56.",
			"Reggie's response: PONG\n7 times 8 is 56.",
		},
		{
			"Result:\n(Report back with Reggie's response and the multiplication result)\nDone.",
			"Result:\nDone.",
		},
		{
			"(Reggie's response will be retrieved and reported)\nFinal answer: 42",
			"Final answer: 42",
		},
		{
			"Real prose with (parenthetical detail) in the middle.",
			"Real prose with (parenthetical detail) in the middle.",
		},
	}
	for _, tc := range tests {
		got := StripResidualSpeechAfterTools(tc.input)
		if got != tc.want {
			t.Errorf("StripResidualSpeechAfterTools(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestStripResidualSpeechAfterTools_LiveS5SpeechLeftover(t *testing.T) {
	// 2026-08-26 live S5 speech after tools: playbook format instructions,
	// bracket stage directions, separator glued to answer, and duplicate sentences.
	const live = "After getting Reggie's response, use the following format:\n" +
		"\n" +
		"Reggie says: <reggie-reply>\n" +
		"\n" +
		"7 times 8 is 56.---\n" +
		"\n" +
		"[After waiting for Reggie's response]\n" +
		"\n" +
		"Reggie said PONG.\n" +
		"\n" +
		"7 times 8 is 56.Reggie said PONG. 7 times 8 is 56."

	got := StripResidualSpeechAfterTools(live)
	if !strings.Contains(got, "Reggie said PONG.") {
		t.Errorf("missing PONG sentence in %q", got)
	}
	if !strings.Contains(got, "7 times 8 is 56.") {
		t.Errorf("missing 56 sentence in %q", got)
	}
	if strings.Count(got, "Reggie said PONG.") != 1 {
		t.Errorf("PONG sentence not unique in %q", got)
	}
	if strings.Count(got, "7 times 8 is 56.") != 1 {
		t.Errorf("56 sentence not unique in %q", got)
	}

	for _, leak := range []string{"use the following format", "Reggie says:", "<reggie-reply>", "---", "[After waiting"} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
}

func TestStripResidualSpeechAfterTools_LeadingGluedStageParen(t *testing.T) {
	in := `(7 times 8 is 56)Reggie replied with "PONG." Seven times eight is 56.`
	got := StripResidualSpeechAfterTools(in)
	if strings.HasPrefix(strings.TrimSpace(got), "(") {
		t.Fatalf("leading paren survived: %q", got)
	}
	if !strings.Contains(got, "PONG") {
		t.Fatalf("missing PONG in %q", got)
	}
	if !strings.Contains(got, "56") {
		t.Fatalf("missing 56 in %q", got)
	}
}

func TestStripResidualSpeechAfterTools_FutureWaitGlue(t *testing.T) {
	in := "Once Reggie has replied with PONG, I will let you know and also inform you that 7 times 8 is 56. I said PONG, and 7 times 8 is 56."
	got := StripResidualSpeechAfterTools(in)
	if strings.Contains(got, "Once Reggie") || strings.Contains(got, "I will let you know") {
		t.Fatalf("future wait glue survived: %q", got)
	}
	if !strings.Contains(got, "56") {
		t.Fatalf("lost answer in %q", got)
	}
}

func TestStripResidualSpeechAfterTools_LeadingBracketStage(t *testing.T) {
	in := `[Delegation initiated. Waiting for Reggie's response. . ]Reggie replied PONG. 7 times 8 is 56.`
	got := StripResidualSpeechAfterTools(in)
	if strings.Contains(got, "Delegation initiated") || strings.HasPrefix(strings.TrimSpace(got), "[") {
		t.Fatalf("bracket stage survived: %q", got)
	}
	if !strings.Contains(got, "PONG") || !strings.Contains(got, "56") {
		t.Fatalf("lost answer in %q", got)
	}
}
