package backend

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Contract-layer guards for internal/backend/speech_contract.go:
//   - diffMiddle must never panic and must never hand back an attribution
//     span that is invalid UTF-8 (it slices byte-wise, so rune boundaries
//     have to be snapped explicitly).
//   - FinalizeSpeech must survive pathological raw content.
//   - The thin adapters (VisibleAssistantContentAfterTools /
//     PersistVisibleAssistantContent) must stay BYTE-IDENTICAL to the
//     hand-ordered pipelines they replaced. The old pipelines are restated
//     verbatim in the last test as the oracle, so any future reordering
//     inside FinalizeSpeech that changes observable output fails here
//     rather than silently changing what teammates see.
func TestDiffMiddle_PathologicalInputs(t *testing.T) {
	cases := [][2]string{
		{"", ""},
		{"", "abc"},
		{"abc", ""},
		{"a", "a"},
		{"aaa", "aa"},
		{"aa", "aaa"},
		{"héllo wörld", "héllo wrld"},
		{"日本語テキスト", "日本語スト"},
		{"🙂🙂🙂", "🙂🙂"},
		{"🙂a🙂", "🙂b🙂"},
		{strings.Repeat("x", 5000), strings.Repeat("x", 4999)},
		{"\x00\xff\xfe", "\x00"},
		{"ab", "ba"},
	}
	for _, c := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic on %q/%q: %v", c[0], c[1], r)
				}
			}()
			b, a := diffMiddle(c[0], c[1])
			// Attribution spans reach slog and any caller marshalling
			// SpeechResult, so they must stay valid UTF-8 — but only when
			// the inputs themselves were valid to begin with.
			if !utf8.ValidString(c[0]) || !utf8.ValidString(c[1]) {
				return
			}
			if !utf8.ValidString(b) || !utf8.ValidString(a) {
				t.Errorf("NON-UTF8 attribution span for %q->%q: before=%q after=%q", c[0], c[1], b, a)
			}
		}()
	}
}

// A rule that returns identical text must record nothing.
func TestSpeechResultStep_IdentityRuleRecordsNothing(t *testing.T) {
	r := &SpeechResult{}
	out := r.step("hello", "noop", func(s string) string { return s })
	if out != "hello" || len(r.Dropped) != 0 || len(r.Rewrites) != 0 {
		t.Fatalf("identity rule recorded something: %+v", r)
	}
}

// FinalizeSpeech must not panic on pathological raw input.
func TestFinalizeSpeech_PathologicalInputsDoNotPanic(t *testing.T) {
	raws := []string{"", " ", "\n\n", "🙂", "日本語", strings.Repeat("a", 100000), "\x00\xff"}
	asks := []string{"", "ping", "what time is it?", "🙂"}
	for _, raw := range raws {
		for _, ask := range asks {
			for _, at := range []bool{false, true} {
				for _, p := range []bool{false, true} {
					func() {
						defer func() {
							if r := recover(); r != nil {
								t.Errorf("panic raw=%q ask=%q after=%v persist=%v: %v", raw, ask, at, p, r)
							}
						}()
						_ = FinalizeSpeech(SpeechInput{Raw: raw, UserAsk: ask, AfterTools: at, Persist: p})
					}()
				}
			}
		}
	}
}

// Adapters must be byte-identical to a re-implementation of the OLD pipelines.
func TestSpeechContractAdapters_ByteIdenticalToPreContractPipelines(t *testing.T) {
	oldAfterTools := func(content, ask string) string {
		visible := VisibleAssistantContent(content)
		visible = stripEmbeddedHarnessToolJSON(visible)
		visible = StripResidualSpeechAfterTools(visible)
		visible = stripHarnessVisibleTokens(visible)
		visible = stripHarnessClockLabel(visible)
		if isTimeAsk(ask) {
			visible = dropTimeExcuseSentences(visible)
		}
		if rw := teammateCompanyWallRewrite(visible, content, ask); rw != "" {
			return stripHarnessClockLabel(rw)
		}
		if rw := teammateHostnameFailRewrite(visible, content, ask); rw != "" {
			return stripHarnessClockLabel(rw)
		}
		if rw := teammateTimeFailRewrite(visible, content, ask); rw != "" {
			return stripHarnessClockLabel(rw)
		}
		if rw := teammateInvalidToolPongRewrite(visible, content, ask); rw != "" {
			return stripHarnessClockLabel(rw)
		}
		return stripHarnessClockLabel(visible)
	}
	oldPersist := func(content, ask string) string {
		visible := oldAfterTools(content, ask)
		visible = stripLeadingBareClockSentence(visible, ask)
		visible = dropLeftoverClockWhenNotTimeAsk(visible, ask)
		visible = dropLeftoverHireGhost(visible, ask)
		visible = dropLeftoverDelegatedHire(visible, ask)
		visible = EchoAckRewrite(visible, ask)
		visible = StatementFragmentAckRewrite(visible, ask)
		return closeIncompletePersist(fillTrivialAckPersist(fillTrivialPingPersist(visible, ask), ask))
	}

	raws := []string{
		"", "Pong.", "Local time now: Thursday, August 27, 2026, 12:00 PM ET",
		"Reggie said: PONG.", "I have delegated the task to Steve. Steve reported: DELTA.",
		"It seems there was a mistake in my response. I will address the issue directly in this session without delegation. The hostname is MJs-MacBook-Pro.",
		"Waiting for the recall task to complete.",
		"Let me check the muninn_recall tool for that. Our staging server is valkyrie.",
		"{\"name\":\"muninn_recall\",\"arguments\":{}} I checked.",
		"TOOL_FAIL: boom", "DELEGATE_FAIL", "Not helpful", "I checked the logs",
		"Loading model, please wait…", "🙂 done", "日本語 done",
	}
	asks := []string{"", "hey", "ping", "ping reggie", "what time is it?", "say DELTA", "what's the hostname?", "hire a designer"}
	for _, raw := range raws {
		for _, ask := range asks {
			if got, want := VisibleAssistantContentAfterTools(raw, ask), oldAfterTools(raw, ask); got != want {
				t.Errorf("AfterTools DRIFT raw=%q ask=%q: new=%q old=%q", raw, ask, got, want)
			}
			if got, want := PersistVisibleAssistantContent(raw, ask), oldPersist(raw, ask); got != want {
				t.Errorf("Persist DRIFT raw=%q ask=%q: new=%q old=%q", raw, ask, got, want)
			}
		}
	}
}
