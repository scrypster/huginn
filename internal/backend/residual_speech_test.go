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
	content := "wait_for_threads\nTOOL_FAIL: nope\n<wait_for_threads>\nReggie said PONG.\nrecall_thread_result\nbash\ncreate_agent"
	if got := VisibleAssistantContentAfterTools(content); got != "Reggie said PONG." {
		t.Fatalf("got %q", got)
	}
}

// Fake leftover row seen on live persist: helpdesk closer + create_agent
// harness name + injected LocalClockLine. Persist must strip all three.
func TestPersistVisibleAssistantContent_LeftoverHelpdeskCreateAgentClock(t *testing.T) {
	row := "How can I assist you further?\ncreate_agent\nLocal time now: Thursday, August 27, 2026, 1:47 PM ET"
	stamp := "Thursday, August 27, 2026, 1:47 PM ET"
	got := PersistVisibleAssistantContent(row, "what time is it")
	if got != "It's "+stamp+"." {
		t.Fatalf("time-ask got %q", got)
	}
	for _, leak := range []string{"Local time now", "local time now", "create_agent", "How can I assist"} {
		if strings.Contains(got, leak) {
			t.Errorf("time-ask leaked %q in %q", leak, got)
		}
	}
	hello := PersistVisibleAssistantContent(row, "hello")
	for _, leak := range []string{"Local time now", "local time now", "create_agent", "How can I assist"} {
		if strings.Contains(hello, leak) {
			t.Errorf("hello leaked %q in %q", leak, hello)
		}
	}
	if got := PersistVisibleAssistantContent("create_agent", "hello"); got != "" {
		t.Fatalf("bare create_agent persisted: %q", got)
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

func TestVisibleAssistantContentAfterTools_TaggedPlaybookFenceDropped(t *testing.T) {
	// 2026-08-26 live Winston: speech-only turn wrote ```json recall
	// playbooks (with // comments) then the real answer.
	content := "```json\n{\n  \"name\": \"recall_thread_result\",\n  \"arguments\": {\n    \"thread_id\": \"<thread-id>\"  // Use the actual thread ID\n  }\n}\n```\n\n```json\n{\n  \"name\": \"recall_thread_result\",\n  \"arguments\": {\n    \"thread_id\": \"<thread-id>\"\n  }\n}\n```Steve reported that the hostname is 'MJs-MacBook-Pro'. Reggie reported that 7 times 8 equals 56."
	got := VisibleAssistantContentAfterTools(content)
	if strings.Contains(got, "recall_thread_result") || strings.Contains(got, "```") {
		t.Fatalf("tagged playbook leaked: %q", got)
	}
	if !strings.Contains(got, "MJs-MacBook-Pro") || !strings.Contains(got, "56") {
		t.Fatalf("answer stripped: %q", got)
	}
}

func TestStripResidualSpeechAfterTools_LiveHallwayHelpdeskCloser(t *testing.T) {
	// 2026-08-26 hallway @Winston after 7*8: teammate answer plus a helpdesk closer
	// that landed on last_preview. Keep the math; drop the closer.
	const live = "The result of 7 * 8 is 56. If you have any other questions, feel free to ask!"
	got := StripResidualSpeechAfterTools(live)
	if got != "The result of 7 * 8 is 56." {
		t.Fatalf("got %q", got)
	}
	for _, keep := range []string{
		"If you have any other questions about the migration, ping Steve.",
		"Feel free to ask Steve for the hostname.",
	} {
		if got := StripResidualSpeechAfterTools(keep); got != keep {
			t.Errorf("prose rewritten: %q -> %q", keep, got)
		}
	}
}

// Exact 2026-08-26 bullet5 hallway persist after a REAL Steve hostname handoff.
const liveBullet5SpawnedWait = "Winston, please note that the delegate task to Steve has been spawned. Please use `wait_for_threads` to block until it finishes and collect the result. Since session history could not be loaded, please ensure to include all necessary context in the task description. The hostname of the machine is 'MJs-MacBook-Pro'. That completes the request. Is there anything else you need assistance with?"

const liveBullet5PleaseCallWait = "Please call wait_for_threads, with no additional arguments, to block until Steve's command has finished. Steve ran the 'hostname' command and received the output 'MJs-MacBook-Pro'."

const liveBullet5AssistanceCloser = "The hostname of the system is 'MJs-MacBook-Pro'. If you have any other questions or need further assistance, feel free to ask."

func TestVisibleAssistantContentAfterTools_LiveBullet5SpawnedWait(t *testing.T) {
	got := VisibleAssistantContentAfterTools(liveBullet5SpawnedWait)
	if !strings.Contains(got, "MJs-MacBook-Pro") {
		t.Fatalf("lost hostname: %q", got)
	}
	if got != "The hostname of the machine is 'MJs-MacBook-Pro'. That completes the request." {
		t.Fatalf("got %q", got)
	}
	for _, leak := range []string{"wait_for_threads", "spawned", "session history could not be loaded", "need assistance"} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
}

func TestVisibleAssistantContentAfterTools_LiveBullet5PleaseCallWait(t *testing.T) {
	got := VisibleAssistantContentAfterTools(liveBullet5PleaseCallWait)
	if got != "Steve ran the 'hostname' command and received the output 'MJs-MacBook-Pro'." {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "wait_for_threads") || strings.Contains(got, "Please call") {
		t.Fatalf("wait instruction leaked: %q", got)
	}
}

func TestVisibleAssistantContentAfterTools_LiveBullet5AssistanceCloser(t *testing.T) {
	got := VisibleAssistantContentAfterTools(liveBullet5AssistanceCloser)
	if got != "The hostname of the system is 'MJs-MacBook-Pro'." {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "further assistance") || strings.Contains(got, "feel free to ask") {
		t.Fatalf("closer leaked: %q", got)
	}
}

func TestStripResidualSpeechAfterTools_LiveBullet5KeepsPongAnd56(t *testing.T) {
	in := "Please call wait_for_threads, with no additional arguments, to block until Steve's command has finished. Reggie said PONG. 7 times 8 is 56."
	got := StripResidualSpeechAfterTools(in)
	if !strings.Contains(got, "PONG") || !strings.Contains(got, "56") {
		t.Fatalf("lost answer: %q", got)
	}
	if strings.Contains(got, "wait_for_threads") {
		t.Fatalf("wait instruction leaked: %q", got)
	}
	keep := "Steve spawned a worker to crunch the numbers."
	if got := StripResidualSpeechAfterTools(keep); got != keep {
		t.Fatalf("prose rewritten: %q -> %q", keep, got)
	}
}

// Exact 2026-08-26 Lab wall PASS speech from Winston after Steve deny.
const liveLabWinstonSteveDeny = `I apologize for any confusion. It seems that "Steve" isn't one of the available agents. Let's try a different approach. I'll have Sam gather the required information. Task delegated to Sam. I apologize, but there was an error when attempting to determine the hostname of this machine. The system encountered an API key resolution issue. If you have access to the API key, please provide it, and I can try again.`

func TestStripResidualSpeechAfterTools_LiveLabWinstonSteveDeny(t *testing.T) {
	got := StripResidualSpeechAfterTools(liveLabWinstonSteveDeny)
	if got != "Steve isn't in Lab. Sam is." {
		t.Fatalf("got %q", got)
	}
	for _, leak := range []string{
		"I apologize",
		"any confusion",
		"Let's try a different approach",
		"available agents",
		"Task delegated",
		"API key",
		"I can try again",
		"please provide it",
	} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
}

func TestStripResidualSpeechAfterTools_KeepsHonestMissingTeammate(t *testing.T) {
	for _, keep := range []string{
		"Steve isn't in this company.",
		"Steve isn't available here.",
		"Steve isn't in Lab. Sam is.",
	} {
		if got := StripResidualSpeechAfterTools(keep); got != keep {
			t.Errorf("honest line rewritten: %q -> %q", keep, got)
		}
	}
}

func TestStripResidualSpeechAfterTools_LabLeftoversKeepHostnameTimesPong(t *testing.T) {
	in := "I apologize for any confusion. The hostname of the machine is 'MJs-MacBook-Pro'. Reggie said PONG. 7 times 8 is 56. Let's try a different approach. If you have access to the API key, please provide it, and I can try again."
	got := StripResidualSpeechAfterTools(in)
	if !strings.Contains(got, "MJs-MacBook-Pro") {
		t.Fatalf("lost hostname: %q", got)
	}
	if !strings.Contains(got, "PONG") {
		t.Fatalf("lost PONG: %q", got)
	}
	if !strings.Contains(got, "56") {
		t.Fatalf("lost times: %q", got)
	}
	for _, leak := range []string{"I apologize", "different approach", "API key", "try again"} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
}

// 2026-08-26 11:20 PM ET Lab REST persist (no tools). VisibleAssistantContent
// used to leave this as helpdesk; persist must run AfterTools leftover strip.
const liveLabWinstonSteveDenyPersist = `I apologize for any confusion. It seems that "Steve" isn't one of the available agents. Let's try a different approach. Task delegated to Sam. Let me know if you have any other questions!`

func TestPersistVisibleAssistantContent_NoToolsLiveLabWinstonSteveDeny(t *testing.T) {
	assertLabWinstonPersist := func(t *testing.T, in string) {
		t.Helper()
		got := PersistVisibleAssistantContent(in)
		if got != "Steve isn't in Lab. Sam is." {
			t.Fatalf("got %q, want Steve isn't in Lab. Sam is.", got)
		}
		for _, leak := range []string{
			"I apologize",
			"any confusion",
			"try a different approach",
			"available agents",
			"Task delegated",
			"any other questions",
			"I'll have",
			"gather the required information",
		} {
			if strings.Contains(got, leak) {
				t.Errorf("leaked %q in %q", leak, got)
			}
		}
	}
	assertLabWinstonPersist(t, liveLabWinstonSteveDeny)
	assertLabWinstonPersist(t, liveLabWinstonSteveDenyPersist)
}

// 2026-08-26 11:31 PM ET Lab REST persist after Sam API-key fail.
const liveLabWinstonSamKeyFail = `I apologize for the inconvenience. It looks like there was an issue accessing the API key needed to determine the hostname. I'll have Sam try again to see if we can resolve the problem.`

func TestPersistVisibleAssistantContent_LiveLabWinstonSamKeyFail(t *testing.T) {
	got := PersistVisibleAssistantContent(liveLabWinstonSamKeyFail)
	if got != "Sam couldn't get the hostname." {
		t.Fatalf("got %q", got)
	}
	for _, leak := range []string{
		"I apologize",
		"inconvenience",
		"API key",
		"I'll have Sam try again",
		"resolve the problem",
	} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
}

func TestStripResidualSpeechAfterTools_SamKeyLeftoversKeepHostnameTimesPong(t *testing.T) {
	in := "I apologize for the inconvenience. The hostname of the machine is 'MJs-MacBook-Pro'. Reggie said PONG. 7 times 8 is 56. It looks like there was an issue accessing the API key. I'll have Sam try again to see if we can resolve the problem."
	got := StripResidualSpeechAfterTools(in)
	if !strings.Contains(got, "MJs-MacBook-Pro") {
		t.Fatalf("lost hostname: %q", got)
	}
	if !strings.Contains(got, "PONG") {
		t.Fatalf("lost PONG: %q", got)
	}
	if !strings.Contains(got, "56") {
		t.Fatalf("lost times: %q", got)
	}
	for _, leak := range []string{"I apologize", "inconvenience", "API key", "try again", "resolve the problem"} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
}

func TestStripResidualSpeechAfterTools_NewLeftoversEmptyWithoutHostnameFail(t *testing.T) {
	for _, in := range []string{
		"I apologize for the inconvenience.",
		"I'll have Sam try again.",
		"To see if we can resolve the problem.",
	} {
		if got := StripResidualSpeechAfterTools(in); got != "" {
			t.Errorf("leftover not empty: %q -> %q", in, got)
		}
	}
}

// 2026-08-26 11:39 PM ET Lab REST persist after Sam keyring fail (speech3 first POST).
const liveLabWinstonSamKeyFail2 = "Sam encountered an error while trying to determine the hostname of the machine. The error message indicates an issue with accessing the API key needed for the operation.\n\nLet me know if you have any other questions or if there's anything else I can assist with!"

func TestPersistVisibleAssistantContent_LiveLabWinstonSamKeyFail2(t *testing.T) {
	got := PersistVisibleAssistantContent(liveLabWinstonSamKeyFail2)
	if got != "Sam couldn't get the hostname." {
		t.Fatalf("got %q", got)
	}
	for _, leak := range []string{
		"I apologize",
		"API key",
		"error message",
		"Let me know",
		"assist with",
	} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
}

// 2026-08-26 11:43–11:44 PM ET Lab REST persist: Sam keyring failed; Winston
// leftover was only the helpdesk closer. All leftover lines strip. Live
// emptied first so the 11:31 rewrite never saw Sam/hostname. Persist with
// the hostname-style ask that involved Sam must keep the teammate line.
const liveLabWinstonSamKeyFail3 = "If you have any other questions or need further assistance, please let me know."

func TestPersistVisibleAssistantContent_LiveLabWinstonSamKeyFail3(t *testing.T) {
	if got := StripResidualSpeechAfterTools(liveLabWinstonSamKeyFail3); got != "" {
		t.Fatalf("want leftover-empty strip, got %q", got)
	}
	// No ask: leftover-empty stays empty — do not invent from a closer.
	if got := PersistVisibleAssistantContent(liveLabWinstonSamKeyFail3); got != "" {
		t.Fatalf("no-ask leftover-empty: %q", got)
	}
	got := PersistVisibleAssistantContent(liveLabWinstonSamKeyFail3, "Ask Sam for the hostname")
	if got != "Sam couldn't get the hostname." {
		t.Fatalf("got %q, want teammate line", got)
	}
	for _, leak := range []string{
		"I apologize",
		"API key",
		"further assistance",
		"please let me know",
	} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
}

// Exact leftover-empty: every leftover line strips, turn involved Sam on a
// hostname-style ask (tools or speech). Persist the teammate line, not "".
func TestPersistVisibleAssistantContent_LeftoverEmptyAllLinesStripped(t *testing.T) {
	// Closer-only / apology-only leftovers empty with no hostname in the
	// leftover itself. Persist/AfterTools must still recover from the ask.
	emptied := []string{
		liveLabWinstonSamKeyFail3,
		"I apologize for the inconvenience.\nI'll have Sam try again.\nTo see if we can resolve the problem.",
		"",
	}
	for _, leftover := range emptied {
		if got := StripResidualSpeechAfterTools(leftover); got != "" {
			t.Fatalf("want leftover-empty strip of %q, got %q", leftover, got)
		}
		got := PersistVisibleAssistantContent(leftover, "Ask Sam for the hostname")
		if got != "Sam couldn't get the hostname." {
			t.Fatalf("leftover %q -> %q, want teammate line", leftover, got)
		}
		if got := VisibleAssistantContentAfterTools(leftover, "Ask Sam for the hostname"); got != "Sam couldn't get the hostname." {
			t.Fatalf("AfterTools leftover %q -> %q", leftover, got)
		}
	}
	// Sam+hostname leftover that leftover-strip empties; rewrite must still fire
	// even without "API key" / "try again" (live emptied first).
	samHost := "Sam encountered an error while trying to determine the hostname of the machine."
	got := PersistVisibleAssistantContent(samHost)
	if got != "Sam couldn't get the hostname." {
		t.Fatalf("sam-host leftover -> %q", got)
	}
	// Hostname ask without Sam, leftover-empty: do not invent.
	if got := PersistVisibleAssistantContent(liveLabWinstonSamKeyFail3, "What is the hostname?"); got != "" {
		t.Fatalf("no-Sam hostname ask: %q", got)
	}
}

// 2026-08-27 8:20 AM ET Steve desk DM: Winston leftover after delegate+wait.
// 14b invented "no real-time data" even though last night it returned a clock.
const liveWinstonTimeExcuse = `Winston mentioned that he cannot directly provide the current time due to not having access to real-time data.`

func TestPersistVisibleAssistantContent_LiveWinstonTimeExcuse(t *testing.T) {
	ask := "ask Winston what time it is"
	if got := StripResidualSpeechAfterTools(liveWinstonTimeExcuse); got != liveWinstonTimeExcuse {
		// Excuse is not playbook glue; AfterTools leftover-strip leaves it
		// until the time-ask path drops it.
		t.Logf("after-tools leftover (pre time-ask drop): %q", got)
	}
	stamp := "Thursday, August 27, 2026, 8:20 AM ET"
	leftoverWithStamp := "Local time now: " + stamp + "\n" + liveWinstonTimeExcuse
	got := PersistVisibleAssistantContent(leftoverWithStamp, ask)
	want := "It's " + stamp + "."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	for _, leak := range []string{
		"cannot directly provide the current time",
		"not having access to real-time data",
		"I don't have access to real-time",
	} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
}

func TestPersistVisibleAssistantContent_LiveWinstonTimeExcuseLeftoverEmpty(t *testing.T) {
	ask := "ask Winston what time it is"
	got := PersistVisibleAssistantContent(liveWinstonTimeExcuse, ask)
	if got == "" {
		t.Fatal("leftover-empty time ask must persist teammate clock, not empty")
	}
	if !strings.HasPrefix(got, "It's ") || !strings.HasSuffix(got, " ET.") {
		t.Fatalf("want teammate It's {clock} ET., got %q", got)
	}
	for _, leak := range []string{
		"cannot directly provide",
		"real-time data",
		"I don't have access to real-time",
	} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
}

func TestPersistVisibleAssistantContent_TimeExcuseKeepsRealClockSpeech(t *testing.T) {
	in := "Winston said it is 8:20 AM ET. He cannot directly provide the current time due to not having access to real-time data."
	got := PersistVisibleAssistantContent(in, "ask Winston what time it is")
	if !strings.Contains(got, "8:20 AM ET") {
		t.Fatalf("lost real clock speech: %q", got)
	}
	if strings.Contains(got, "cannot directly provide") || strings.Contains(got, "real-time data") {
		t.Fatalf("excuse leaked: %q", got)
	}
}

func TestPersistVisibleAssistantContent_TimeExcuseNotTimeAsk(t *testing.T) {
	got := PersistVisibleAssistantContent(liveWinstonTimeExcuse, "hello")
	if got != liveWinstonTimeExcuse {
		t.Fatalf("non-time-ask leftover rewritten: %q", got)
	}
}

// Exact 2026-08-27 8:43 AM ET Steve hallway leftover after Winston said 8:42 AM ET.
const liveSteveHallwayClockRecap = `I apologize for the confusion earlier. It seems there was an issue with the previous response. Let me clarify: - The first response from Winston indicates that it is currently **Thursday, August 27, 2026, at 8:42 AM ET**. - The second response suggests that Winston cannot directly provide real-time data and offers to help set up a way to retrieve the current time if needed. If you need the current time again, please let me know how I can assist you further.`

// Post-bounce 2026-08-27 Steve hallway one-liner after v0.4.0-local-clock.
const liveSteveHallwayClockRecapOneLiner = `The first response from Winston indicates that it is currently Thursday, August 27, 2026, at 8:42 AM ET.`

func TestStripResidualSpeechAfterTools_LiveSteveHallwayKeepsClock(t *testing.T) {
	got := StripResidualSpeechAfterTools(liveSteveHallwayClockRecap)
	if !strings.Contains(got, "Thursday, August 27, 2026, at 8:42 AM ET") {
		t.Fatalf("residual ate clock: %q", got)
	}
}

func assertSteveHallwayClockPersist(t *testing.T, leftover string) {
	t.Helper()
	ask := "Can you ask Winston what time it is"
	got := PersistVisibleAssistantContent(leftover, ask)
	stamp := "Thursday, August 27, 2026, at 8:42 AM ET"
	if !strings.Contains(got, stamp) {
		t.Fatalf("lost clock: %q", got)
	}
	wantIts := "It's " + stamp + "."
	wantCurrent := "The current time is " + stamp + "."
	if got != wantIts && got != wantCurrent {
		t.Fatalf("got %q, want %q or %q", got, wantIts, wantCurrent)
	}
	for _, leak := range []string{
		"first response from Winston",
		"indicates that it is currently",
		"I apologize",
		"Let me clarify",
		"second response",
		"cannot directly provide",
		"real-time data",
		"assist you further",
	} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
}

func TestPersistVisibleAssistantContent_LiveSteveHallwayClockRecap(t *testing.T) {
	assertSteveHallwayClockPersist(t, liveSteveHallwayClockRecap)
}

func TestPersistVisibleAssistantContent_LiveSteveHallwayClockRecapOneLiner(t *testing.T) {
	assertSteveHallwayClockPersist(t, liveSteveHallwayClockRecapOneLiner)
}

func TestPersistVisibleAssistantContent_LiveSteveHallwayClockRecapNotTimeAsk(t *testing.T) {
	for _, leftover := range []string{liveSteveHallwayClockRecap, liveSteveHallwayClockRecapOneLiner} {
		got := PersistVisibleAssistantContent(leftover, "hello")
		if got == "It's Thursday, August 27, 2026, at 8:42 AM ET." {
			t.Fatalf("non-time-ask leftover rewritten: %q", got)
		}
		if !strings.Contains(got, "Thursday, August 27, 2026, at 8:42 AM ET") {
			t.Fatalf("non-time-ask ate clock: %q", got)
		}
	}
}

// Exact 2026-08-27 12:00 PM ET #Huginn hallway: Winston echoed the
// injected LocalClockLine as the whole assistant row.
const liveWinstonLocalTimeNow1200 = `Local time now: Thursday, August 27, 2026, 12:00 PM ET`

func TestPersistVisibleAssistantContent_LiveWinstonLocalTimeNow1200(t *testing.T) {
	ask := "@Winston what time is it"
	got := PersistVisibleAssistantContent(liveWinstonLocalTimeNow1200, ask)
	want := "It's Thursday, August 27, 2026, 12:00 PM ET."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if strings.Contains(got, "Local time now") || strings.Contains(got, "local time now") {
		t.Fatalf("harness clock label leaked: %q", got)
	}
	hello := PersistVisibleAssistantContent(liveWinstonLocalTimeNow1200, "hello")
	if hello != "" {
		t.Fatalf("non-time-ask leftover-clock-only: %q, want empty", hello)
	}
}

// Exact 2026-08-27 8:58 AM ET Steve hallway persist: Winston echoed the
// injected LocalClockLine label into speech. Keep the stamp; drop the label.
const liveSteveLocalTimeNowLeak = `Winston reported that the current time is local time now: Thursday, August 27, 2026, 8:58 AM ET.`

func TestPersistVisibleAssistantContent_LiveSteveLocalTimeNowLeak(t *testing.T) {
	ask := "Can you ask Winston what time it is"
	got := PersistVisibleAssistantContent(liveSteveLocalTimeNowLeak, ask)
	stamp := "Thursday, August 27, 2026, 8:58 AM ET"
	if !strings.Contains(got, stamp) {
		t.Fatalf("lost clock: %q", got)
	}
	wantIts := "It's " + stamp + "."
	wantCurrent := "The current time is " + stamp + "."
	if got != wantIts && got != wantCurrent {
		t.Fatalf("got %q, want %q or %q", got, wantIts, wantCurrent)
	}
	for _, leak := range []string{
		"local time now",
		"Local time now",
	} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
	hello := PersistVisibleAssistantContent(liveSteveLocalTimeNowLeak, "hello")
	if strings.Contains(hello, "local time now") || strings.Contains(hello, "Local time now") {
		t.Fatalf("non-time-ask leaked clock label: %q", hello)
	}
	if !strings.Contains(hello, stamp) {
		t.Fatalf("non-time-ask lost stamp: %q", hello)
	}
}

// Exact live miss: first-click Lab "Ask Steve for the hostname" persisted
// leftover-empty / stale Sam-hostname instead of the company wall.
// Do not rewrite old Lab channel history rows — persist of this turn only.
func TestPersistVisibleAssistantContent_LiveLabAskSteveHostnameWall(t *testing.T) {
	ask := "Ask Steve for the hostname"
	want := "Steve isn't in Lab. Sam is."
	cases := []struct {
		name     string
		leftover string
	}{
		{"closer-only leftover-empty", liveLabWinstonSamKeyFail3},
		{"empty leftover", ""},
		{"stale Sam hostname leftover", liveLabWinstonSamKeyFail},
		{"Sam try-again leftover-empty", "I apologize for the inconvenience.\nI'll have Sam try again.\nTo see if we can resolve the problem."},
		{"already wall", want},
		{"helpdesk Steve deny", liveLabWinstonSteveDenyPersist},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PersistVisibleAssistantContent(tc.leftover, ask)
			if got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
			if strings.Contains(got, "couldn't get the hostname") {
				t.Fatalf("Sam-hostname rewrite fired on Ask Steve: %q", got)
			}
		})
	}
	// Hostname rewrite still fires for Ask Sam leftover-empty.
	got := PersistVisibleAssistantContent(liveLabWinstonSamKeyFail3, "Ask Sam for the hostname")
	if got != "Sam couldn't get the hostname." {
		t.Fatalf("Ask Sam leftover-empty: %q", got)
	}
	// Real hostname answer is not eaten when the ask mentioned Steve.
	keep := "The hostname of the machine is 'MJs-MacBook-Pro'."
	if got := PersistVisibleAssistantContent(keep, ask); !strings.Contains(got, "MJs-MacBook-Pro") {
		t.Fatalf("real hostname eaten: %q", got)
	}
	// Non-Steve leftover-empty hostname ask still does not invent.
	if got := PersistVisibleAssistantContent(liveLabWinstonSamKeyFail3, "What is the hostname?"); got != "" {
		t.Fatalf("no-Steve hostname ask invented: %q", got)
	}
}

// Exact 2026-08-27 9:23 AM ET Lab persist after Ask Steve for the hostname.
// Wall line was present; leftover helpdesk + Winston narrator glue remained.
const liveLabAskSteveGlue = "Since Steve isn't available, I'll ask Sam again.\n\nWinston: Sam has been given the task to determine the hostname of this machine. Steve isn't in Lab. Sam is."

func TestPersistVisibleAssistantContent_LiveLabAskSteveGlue(t *testing.T) {
	ask := "Ask Steve for the hostname"
	got := PersistVisibleAssistantContent(liveLabAskSteveGlue, ask)
	if got != "Steve isn't in Lab. Sam is." {
		t.Fatalf("got %q", got)
	}
	for _, leak := range []string{
		"Since Steve isn't available",
		"I'll ask Sam again",
		"has been given the task",
		"Winston:",
		"couldn't get the hostname",
	} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %q in %q", leak, got)
		}
	}
	// Leftover strip must not eat the wall (and must not fire Sam-hostname).
	if stripped := StripResidualSpeechAfterTools(liveLabAskSteveGlue); stripped != "Steve isn't in Lab. Sam is." {
		t.Fatalf("AfterTools leftover strip: %q", stripped)
	}
}

// Exact leftover helpdesk closers from last Huginn hallway transcripts
// (2026-08-26/27). Would still persist on NEW AfterTools persist before this cut.
const liveHallwayQuestionsOrTasksCloser = "Steve calculated the result of the command `hostname + 7*8` and found that it is `MJs-MacBook-Pro. local 56`. If you have any other questions or tasks, feel free to ask!"

const liveHallwayNeedAssistanceCloser = "It seems there are no active threads to check. If you need assistance with any specific tasks or further help, please let me know!"

const liveHallwayFurtherInstructionsCloser = "Both Steve and Reggie have confirmed the response. If you have any further instructions or need assistance with additional tasks, please let me know."

const liveHallwayContactSupportCloser = "Reggie attempted to confirm his status by responding to a ping, but the 'PONG' response was not recognized as a valid tool. Please verify the tool usage or contact support for further assistance."

func TestPersistVisibleAssistantContent_LiveHallwayLeftoverHelpdeskClosers(t *testing.T) {
	got := PersistVisibleAssistantContent(liveHallwayQuestionsOrTasksCloser)
	if strings.Contains(got, "feel free to ask") || strings.Contains(got, "questions or tasks") {
		t.Fatalf("questions-or-tasks closer leaked: %q", got)
	}
	if !strings.Contains(got, "MJs-MacBook-Pro") && !strings.Contains(got, "56") {
		t.Fatalf("lost teammate answer: %q", got)
	}

	got = PersistVisibleAssistantContent(liveHallwayNeedAssistanceCloser)
	if strings.Contains(got, "need assistance") || strings.Contains(got, "please let me know") {
		t.Fatalf("need-assistance closer leaked: %q", got)
	}

	got = PersistVisibleAssistantContent(liveHallwayFurtherInstructionsCloser)
	if strings.Contains(got, "further instructions") || strings.Contains(got, "please let me know") {
		t.Fatalf("further-instructions closer leaked: %q", got)
	}
	if !strings.Contains(got, "Steve") || !strings.Contains(got, "Reggie") {
		t.Fatalf("lost teammate confirm: %q", got)
	}

	got = PersistVisibleAssistantContent(liveHallwayContactSupportCloser)
	if strings.Contains(got, "contact support") || strings.Contains(got, "further assistance") || strings.Contains(got, "verify the tool usage") || strings.Contains(got, "not a valid tool") || strings.Contains(got, "not recognized") {
		t.Fatalf("contact-support closer leaked: %q", got)
	}
	if got != "Reggie confirmed status: PONG." {
		t.Fatalf("want teammate rewrite, got %q", got)
	}

	// Existing teammate prose must stay.
	for _, keep := range []string{
		"If you have any other questions about the migration, ping Steve.",
		"Feel free to ask Steve for the hostname.",
	} {
		if got := PersistVisibleAssistantContent(keep); got != keep {
			t.Errorf("prose rewritten: %q -> %q", keep, got)
		}
	}
}

func TestPersistVisibleAssistantContent_LiveThreadWinstonInvalidToolPong(t *testing.T) {
	// Exact leftover Winston persisted in the #Huginn thread drawer.
	const live = "Reggie attempted to confirm his status by responding to a ping, but the 'PONG' response was not recognized as a valid tool. Please verify the tool usage or contact support for further assistance."
	got := PersistVisibleAssistantContent(live)
	if got != "Reggie confirmed status: PONG." {
		t.Fatalf("thread leftover rewrite: %q", got)
	}
	for _, leak := range []string{"contact support", "further assistance", "verify the tool usage", "not a valid tool", "not recognized"} {
		if strings.Contains(got, leak) {
			t.Fatalf("leaked %q in %q", leak, got)
		}
	}
	if keep := PersistVisibleAssistantContent("Reggie said PONG."); keep != "Reggie said PONG." {
		t.Fatalf("teammate PONG rewritten: %q", keep)
	}
}

func TestStripResidualSpeechAfterTools_LiveHallwayCloserOnlyEmpty(t *testing.T) {
	for _, closer := range []string{
		"If you have any other questions or tasks, feel free to ask!",
		"If you need assistance with any specific tasks or further help, please let me know!",
		"If you have any further instructions or need assistance with additional tasks, please let me know.",
		"Please verify the tool usage or contact support for further assistance.",
	} {
		if got := StripResidualSpeechAfterTools(closer); got != "" {
			t.Fatalf("closer-only leftover not empty: %q -> %q", closer, got)
		}
	}
}

// Exact 2026-08-27 12:00 PM ET persist: Winston echoed LocalClockLine as the
// entire hallway row. clock3 only rewrote time-asks; "what day" leaked the
// harness label again. Strip the label on every persist.
const liveWinstonLocalTimeNowBare = `Local time now: Thursday, August 27, 2026, 12:00 PM ET`

func TestPersistVisibleAssistantContent_LiveWinstonLocalTimeNowBare(t *testing.T) {
	want := "It's Thursday, August 27, 2026, 12:00 PM ET."
	for _, ask := range []string{
		"@Winston what time is it?",
		"@Winston what day is it?",
	} {
		got := PersistVisibleAssistantContent(liveWinstonLocalTimeNowBare, ask)
		if strings.Contains(got, "Local time now") || strings.Contains(got, "local time now") {
			t.Fatalf("ask %q leaked clock label: %q", ask, got)
		}
		if got != want {
			t.Fatalf("ask %q got %q, want %q", ask, got, want)
		}
	}
	hello := PersistVisibleAssistantContent(liveWinstonLocalTimeNowBare, "hello")
	if hello != "" {
		t.Fatalf("non-time-ask leftover-clock-only: %q, want empty", hello)
	}
	if got := StripResidualSpeechAfterTools(liveWinstonLocalTimeNowBare); strings.Contains(got, "Local time now") {
		t.Fatalf("AfterTools leftover strip leaked clock label: %q", got)
	}
}

// Exact live mesh leftover Winston persisted under double-at mesh2-4bc79c.
const liveWinstonSteveContextLeftover = "Please provide any necessary context for Steve to complete the task if needed. **Result from agent \"Steve\":**\n\n@Winston @Reggie pong"

func TestPersistVisibleAssistantContent_LiveWinstonSteveContextLeftover(t *testing.T) {
	got := PersistVisibleAssistantContent(liveWinstonSteveContextLeftover)
	if strings.Contains(got, "Please provide any necessary context") {
		t.Fatalf("steve-context leftover leaked: %q", got)
	}
	if strings.Contains(strings.ToLower(got), "result from agent") {
		t.Fatalf("result-from-agent wrapper leaked: %q", got)
	}
	if !strings.Contains(got, "@Winston @Reggie pong") {
		t.Fatalf("lost teammate body: %q", got)
	}
}

func TestStripResidualSpeechAfterTools_SteveContextAndResultWrapper(t *testing.T) {
	if got := StripResidualSpeechAfterTools("Please provide any necessary context for Steve to complete the task if needed."); got != "" {
		t.Fatalf("steve-context leftover: %q", got)
	}
	if got := StripResidualSpeechAfterTools("Result from agent 'Steve':"); got != "" {
		t.Fatalf("result wrapper leftover: %q", got)
	}
	if got := StripResidualSpeechAfterTools("It seems there are no active threads to check."); got != "" {
		t.Fatalf("no-active-threads leftover: %q", got)
	}
}

func TestStripLoadingModelAndThirdPersonNoted(t *testing.T) {
	if got := StripResidualSpeechAfterTools("Loading model, please wait..."); got != "" {
		t.Fatalf("loading-model leak: %q", got)
	}
	if got := PersistVisibleAssistantContent("Loading model, please wait...", "What's my dog's name?"); got != "" {
		t.Fatalf("loading-model persist leak: %q", got)
	}
	noted := "Understood, @Winston has noted your preferences for your dog Odin and your dietary choice of oat-milk lattes."
	got := PersistVisibleAssistantContent(noted, "heads up: my dog is named Odin")
	if strings.Contains(got, "has noted") || strings.Contains(got, "@Winston") {
		t.Fatalf("third-person noted persist leak: %q", got)
	}
}

func TestStripResidualSpeechAfterTools_SelfCorrectionGlue(t *testing.T) {
	// Live repro 2026-08-27: self-referential correction narration a small
	// model emits after a failed/aborted delegation attempt. Never teammate
	// speech — the human never asked "did you make a mistake?".
	literal := "It seems there was a mistake in my response. I will address the issue directly in this session without delegation."
	if got := StripResidualSpeechAfterTools(literal); got != "" {
		t.Fatalf("self-correction glue leaked: %q", got)
	}

	// Variants: "appears" instead of "seems", "previous response", filler
	// between "directly" and "without delegation", "this" instead of "the
	// issue".
	for _, glue := range []string{
		"It appears there was a mistake in my response.",
		"It seems there was a mistake in my previous response.",
		"It appears there was a mistake in my previous response!",
		"I will address the issue directly in this session without delegation.",
		"I will address this directly without delegation.",
		"I will address the issue directly, without further delegation.",
	} {
		if got := StripResidualSpeechAfterTools(glue); got != "" {
			t.Errorf("%q not treated as self-correction glue: %q", glue, got)
		}
	}

	// Glue followed by real content: the real sentence survives, the glue
	// does not appear in the result.
	withAnswer := "It seems there was a mistake in my response. I will address the issue directly in this session without delegation. The hostname is MJs-MacBook-Pro."
	got := StripResidualSpeechAfterTools(withAnswer)
	if strings.Contains(got, "mistake") || strings.Contains(got, "without delegation") {
		t.Fatalf("self-correction glue leaked alongside real answer: %q", got)
	}
	if !strings.Contains(got, "MJs-MacBook-Pro") {
		t.Fatalf("real answer stripped along with glue: %q", got)
	}

	// Legitimate sentences about mistakes/delegation in a different context
	// must not be stripped.
	for _, keep := range []string{
		"Delegation of authority was a common mistake in early management theory.",
		"The team reviewed past mistakes in delegation during the retro.",
		"I will address the budget concerns directly with finance.",
		// Contains the trigger phrase but continues past it — the glue
		// regex is end-anchored so this whole sentence survives.
		"I will address the issue directly in this session without delegation to a subcontractor, per the contract.",
		"There was a mistake in my previous calculation, the correct total is 42.",
		"It appears there was a mistake in the invoice, not in my response.",
	} {
		if got := StripResidualSpeechAfterTools(keep); got != keep {
			t.Errorf("legitimate sentence over-matched: %q -> %q", keep, got)
		}
	}
}
