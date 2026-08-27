package spaces_test

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/spaces"
)

func TestSpeechPreview_KeepsTeammateSpeech(t *testing.T) {
	got := spaces.SpeechPreview("Ship it after lunch.")
	if got != "Ship it after lunch." {
		t.Fatalf("got %q", got)
	}
}

func TestSpeechPreview_DropsTOOL_FAIL(t *testing.T) {
	if p := spaces.SpeechPreview("TOOL_FAIL: wait_for_threads exploded"); p != "" {
		t.Fatalf("TOOL_FAIL leaked: %q", p)
	}
}

func TestSpeechPreview_DropsDelegatedToChip(t *testing.T) {
	if p := spaces.SpeechPreview("Delegated to @Sam"); p != "" {
		t.Fatalf("delegation announcement leaked onto chip: %q", p)
	}
}

func TestSpeechPreview_DropsHarnessJSON(t *testing.T) {
	if p := spaces.SpeechPreview(`{"name":"wait_for_threads","arguments":{}}`); p != "" {
		t.Fatalf("harness JSON leaked: %q", p)
	}
	if p := spaces.SpeechPreview(`{"name":"delegate_to_agent","arguments":{"agent_name":"Sam"}}`); p != "" {
		t.Fatalf("delegate JSON leaked: %q", p)
	}
}

func TestLastSpeechPreview_WalksBackPastHarness(t *testing.T) {
	replies := []spaces.SpaceMessage{
		{Content: "first speech"},
		{Content: "TOOL_FAIL: boom"},
		{Content: `{"name":"wait_for_threads","arguments":{}}`},
	}
	got := spaces.LastSpeechPreview(replies)
	if got != "first speech" {
		t.Fatalf("got %q want first speech", got)
	}
}

func TestBuildThreadWakePrompt_IncludesParentAndPriorReply_ExcludesHarness(t *testing.T) {
	parent := &spaces.SpaceMessage{Role: "user", Content: "We should ship the hallway chip tonight"}
	replies := []spaces.SpaceMessage{
		{Role: "assistant", Agent: "Winston", Content: "I'll take the chip; Steve can review the wake path."},
		{Role: "assistant", Agent: "Winston", Content: "TOOL_FAIL: wait_for_threads exploded"},
		{Role: "assistant", Agent: "Winston", Content: `{"name":"wait_for_threads","arguments":{}}`},
		{Role: "user", Content: "@Steve can you look at the wake path?"},
	}
	got := spaces.BuildThreadWakePrompt(parent, replies, "@Steve can you look at the wake path?")
	if !strings.Contains(got, "We should ship the hallway chip tonight") {
		t.Fatalf("parent missing:\n%s", got)
	}
	if !strings.Contains(got, "Winston") || !strings.Contains(got, "I'll take the chip") {
		t.Fatalf("prior reply name+content missing:\n%s", got)
	}
	if !strings.Contains(got, "@Steve can you look at the wake path?") {
		t.Fatalf("mention missing:\n%s", got)
	}
	if strings.Contains(got, "TOOL_FAIL") {
		t.Fatalf("TOOL_FAIL leaked:\n%s", got)
	}
	if strings.Contains(got, "wait_for_threads") {
		t.Fatalf("harness JSON leaked:\n%s", got)
	}
	if strings.Count(got, "@Steve can you look at the wake path?") != 1 {
		t.Fatalf("mention should appear once (not inside transcript):\n%s", got)
	}
}

func TestReplySpeech_DropsHarnessKeepsProse(t *testing.T) {
	if p := spaces.ReplySpeech("TOOL_FAIL: boom"); p != "" {
		t.Fatalf("TOOL_FAIL leaked: %q", p)
	}
	if p := spaces.ReplySpeech(`{"name":"wait_for_threads","arguments":{}}`); p != "" {
		t.Fatalf("harness JSON leaked: %q", p)
	}
	if p := spaces.ReplySpeech("Ship it after lunch."); p != "Ship it after lunch." {
		t.Fatalf("got %q", p)
	}
}

func TestSpeechPreview_KeepsPlaybookLookingProse(t *testing.T) {
	prose := `Don't paste {"name":"wait_for_threads"} into the chip; just say done.`
	got := spaces.SpeechPreview(prose)
	if got == "" {
		t.Fatal("legit prose mentioning playbook JSON must stay on last_preview")
	}
	if !strings.Contains(got, "just say done") {
		t.Fatalf("prose was stripped: %q", got)
	}
	if p := spaces.LastSpeechPreview([]spaces.SpaceMessage{{Content: prose}}); !strings.Contains(p, "just say done") {
		t.Fatalf("last_preview lost prose: %q", p)
	}
}

func TestSpeechPreview_DropsLiveHallwayHelpdeskCloser(t *testing.T) {
	live := "The result of 7 * 8 is 56. If you have any other questions, feel free to ask!"
	got := spaces.SpeechPreview(live)
	if got != "The result of 7 * 8 is 56." {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "feel free to ask") {
		t.Fatalf("closer leaked onto last_preview: %q", got)
	}
}

func TestSpeechPreview_LiveBullet5DropsWaitPlaybookKeepsHostname(t *testing.T) {
	const spawned = "Winston, please note that the delegate task to Steve has been spawned. Please use `wait_for_threads` to block until it finishes and collect the result. Since session history could not be loaded, please ensure to include all necessary context in the task description. The hostname of the machine is 'MJs-MacBook-Pro'. That completes the request. Is there anything else you need assistance with?"
	got := spaces.SpeechPreview(spawned)
	if got == "" {
		t.Fatal("hostname speech was blanked")
	}
	if strings.HasPrefix(got, "Please") || strings.HasPrefix(got, "Winston") || strings.Contains(got, "wait_for_threads") {
		t.Fatalf("chip started with wait instruction: %q", got)
	}
	if !strings.Contains(got, "MJs-MacBook-Pro") {
		t.Fatalf("lost hostname on chip: %q", got)
	}

	const pleaseCall = "Please call wait_for_threads, with no additional arguments, to block until Steve's command has finished. Steve ran the 'hostname' command and received the output 'MJs-MacBook-Pro'."
	got = spaces.SpeechPreview(pleaseCall)
	if strings.Contains(got, "wait_for_threads") || strings.HasPrefix(got, "Please call") {
		t.Fatalf("chip started with wait instruction: %q", got)
	}
	if !strings.Contains(got, "MJs-MacBook-Pro") {
		t.Fatalf("lost hostname on chip: %q", got)
	}

	const closer = "The hostname of the system is 'MJs-MacBook-Pro'. If you have any other questions or need further assistance, feel free to ask."
	got = spaces.SpeechPreview(closer)
	if got != "The hostname of the system is 'MJs-MacBook-Pro'." {
		t.Fatalf("got %q", got)
	}
	if p := spaces.ReplySpeech(closer); p != "The hostname of the system is 'MJs-MacBook-Pro'." {
		t.Fatalf("ReplySpeech got %q", p)
	}
}

func TestSpeechPreview_DropsLoadingModelStatus(t *testing.T) {
	for _, in := range []string{
		"Loading model, please wait...",
		"Loading model, pleas",
		"Loading model, pleas…",
	} {
		if p := spaces.SpeechPreview(in); p != "" {
			t.Fatalf("loading model leaked onto last_preview: %q -> %q", in, p)
		}
		if p := spaces.ReplySpeech(in); p != "" {
			t.Fatalf("loading model leaked into ReplySpeech: %q -> %q", in, p)
		}
	}
	got := spaces.LastSpeechPreview([]spaces.SpaceMessage{
		{Content: "Pong."},
		{Content: "Loading model, please wait..."},
	})
	if got != "Pong." {
		t.Fatalf("walk-back past loading model: %q", got)
	}
}
