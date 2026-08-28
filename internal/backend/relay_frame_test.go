package backend

import "testing"

// Live 2026-08-28 8:57 AM ET Winston->Steve delegated-recap hallway reply,
// full pipeline: leading clock stamp stripped, "X reported: Y" rewritten to
// "Y — via X." keeping Y verbatim.
func TestPersistVisibleAssistantContent_LiveDelegatedRecapRelayFrame(t *testing.T) {
	got := PersistVisibleAssistantContent(liveDelegatedRecapClockStamp, "say DELTA")
	want := "DELTA — via Steve."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRewriteRelayFrameSentences_Reported(t *testing.T) {
	got := StripResidualSpeechAfterTools("Steve reported: DELTA.")
	want := "DELTA — via Steve."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRewriteRelayFrameSentences_Said(t *testing.T) {
	got := StripResidualSpeechAfterTools("Reggie said: PONG.")
	want := "PONG — via Reggie."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRewriteRelayFrameSentences_Returned(t *testing.T) {
	got := StripResidualSpeechAfterTools("Sam returned: 56.")
	want := "56 — via Sam."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRewriteRelayFrameSentences_KeepsOrdinaryProse(t *testing.T) {
	// "reported" mid-sentence, not the frame shape — untouched.
	s := "Sam reported that the build is green."
	got := StripResidualSpeechAfterTools(s)
	if got != s {
		t.Fatalf("ordinary prose rewritten: got %q, want %q", got, s)
	}
}

// Live: "I have delegated the task to X." persisted as a Steve DM preview —
// terminal speech even though it is not the answer. When a result followed
// in the same turn, the ack must not be the terminal speech; when it is the
// only content, it is honest and stays.
func TestDropDelegatedAckWhenResultFollows_DropsWhenResultPresent(t *testing.T) {
	got := StripResidualSpeechAfterTools("I have delegated the task to Steve. Steve reported: DELTA.")
	want := "DELTA — via Steve."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDropDelegatedAckWhenResultFollows_KeepsWhenSoleContent(t *testing.T) {
	s := "I have delegated the task to Steve."
	got := StripResidualSpeechAfterTools(s)
	if got != s {
		t.Fatalf("sole delegation ack must stay honest: got %q, want %q", got, s)
	}
}

func TestDropDelegatedAckWhenResultFollows_ContractionVariant(t *testing.T) {
	got := StripResidualSpeechAfterTools("I've delegated the task to Reggie. PONG.")
	want := "PONG."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Adversarial: the relay frame must not fire on ordinary prose that merely
// quotes a speaker with a colon. "He said: no." is an answer, not a relay
// frame, and must never become "no — via He."
func TestRewriteRelayFrameSentences_PronounSubjectIsNotATeammate(t *testing.T) {
	for _, in := range []string{
		"He said: no.",
		"She said: the meeting moved.",
		"They reported: two failures.",
		"It returned: 404.",
		"The reported: value is stale.",
	} {
		if got := rewriteRelayFrameSentences(in); got != in {
			t.Errorf("pronoun subject mangled: %q -> %q", in, got)
		}
	}
	// Lowercase subject (a log line, not a name) is also left alone.
	if got := rewriteRelayFrameSentences("user said: hello"); got != "user said: hello" {
		t.Errorf("lowercase subject mangled: %q", got)
	}
}

// A promoted answer must not start mid-sentence in lowercase.
func TestRewriteRelayFrameSentences_CapitalizesPromotedAnswer(t *testing.T) {
	got := rewriteRelayFrameSentences("MJ said: ship it on Friday.")
	if got != "Ship it on Friday — via MJ." {
		t.Errorf("got %q, want %q", got, "Ship it on Friday — via MJ.")
	}
	// An intentionally lowercase identifier keeps its casing.
	got = rewriteRelayFrameSentences("Steve reported: iPhone builds are green.")
	if got != "iPhone builds are green — via Steve." {
		t.Errorf("got %q, want iPhone casing preserved", got)
	}
}
