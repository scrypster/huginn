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
