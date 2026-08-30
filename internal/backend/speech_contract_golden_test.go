package backend

import "testing"

// This file is the human-readable contract table: one place to see, at a
// glance, exactly what raw assistant content collapses to once it goes
// through the output contract. Every input here is a live repro string
// already exercised elsewhere in this package (residual_speech_test.go,
// tool_narration_test.go, relay_frame_test.go, echo_guard_test.go,
// local_clock_test.go, leftover_persist_test.go) — this table does not add
// new behavior, it documents the behavior that already exists.
func TestFinalizeSpeech_ContractGoldenTable(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		userAsk    string
		afterTools bool
		persist    bool
		want       string
	}{
		{
			name:       "bare clock leak on a non-time ask is dropped",
			raw:        "Local time now: Thursday, August 27, 2026, 12:00 PM ET",
			userAsk:    "hey",
			afterTools: true,
			persist:    true,
			want:       "",
		},
		{
			name:       "bare clock leak on a time ask becomes teammate speech",
			raw:        "Local time now: Thursday, August 27, 2026, 12:00 PM ET",
			userAsk:    "what time is it?",
			afterTools: true,
			persist:    true,
			want:       "It's Thursday, August 27, 2026, 12:00 PM ET.",
		},
		{
			name:       "relay frame 'X reported: Y' rewritten with the clock stamp stripped",
			raw:        liveDelegatedRecapClockStamp,
			userAsk:    "say DELTA",
			afterTools: true,
			persist:    true,
			want:       "DELTA — via Steve.",
		},
		{
			name:       "tool-plan narration stripped, real answer kept",
			raw:        liveWinstonMuninnNarration,
			userAsk:    "what is our production database named?",
			afterTools: true,
			persist:    true,
			want:       "Our production database is named yggdrasil.",
		},
		{
			name:       "self-correction glue stripped, real answer kept",
			raw:        "It seems there was a mistake in my response. I will address the issue directly in this session without delegation. The hostname is MJs-MacBook-Pro.",
			userAsk:    "what's the hostname?",
			afterTools: true,
			persist:    true,
			want:       "The hostname is MJs-MacBook-Pro.",
		},
		{
			name:       "prose wait glue with no real answer collapses to empty",
			raw:        "Waiting for the recall task to complete.",
			userAsk:    "hey",
			afterTools: true,
			persist:    true,
			want:       "",
		},
		{
			name:       "verbatim echo of a statement ask rewritten to a short ack",
			raw:        liveWinstonEcho,
			userAsk:    liveWinstonEchoAsk,
			afterTools: true,
			persist:    true,
			want:       "Noted.",
		},
		{
			name:       "bare fragment ack on a statement turn rewritten to a short ack",
			raw:        "Not helpful",
			userAsk:    liveWinstonNotHelpfulAsk,
			afterTools: true,
			persist:    true,
			want:       "Noted.",
		},
		{
			name:       "trivial ping ask fills Pong. when speech is empty",
			raw:        "",
			userAsk:    "ping",
			afterTools: true,
			persist:    true,
			want:       "Pong.",
		},
		{
			name:       "leftover Pong. dropped on a non-ping ask",
			raw:        "Pong.",
			userAsk:    "what time is it?",
			afterTools: true,
			persist:    true,
			want:       "",
		},
		{
			name:       "relay frame 'X said: Y' rewritten",
			raw:        "Reggie said: PONG.",
			userAsk:    "ping reggie",
			afterTools: true,
			persist:    true,
			want:       "PONG — via Reggie.",
		},
		{
			name:       "delegation ack dropped once the result is present",
			raw:        "I have delegated the task to Steve. Steve reported: DELTA.",
			userAsk:    "say DELTA",
			afterTools: true,
			persist:    true,
			want:       "DELTA — via Steve.",
		},
		{
			name:       "tool-plan narration variant ('Let me check the X tool') stripped",
			raw:        "Let me check the muninn_recall tool for that. Our staging server is valkyrie.",
			userAsk:    "what's the staging server?",
			afterTools: true,
			persist:    true,
			want:       "Our staging server is valkyrie.",
		},
		{
			name:       "empty speech on a trivial thanks ask fills a short ack",
			raw:        "",
			userAsk:    "thanks",
			afterTools: true,
			persist:    true,
			want:       "You're welcome.",
		},
		{
			name:       "leftover-empty Sam/hostname fail rewritten to honest teammate speech",
			raw:        "",
			userAsk:    "Sam what's the hostname?",
			afterTools: true,
			persist:    true,
			want:       "Sam couldn't get the hostname.",
		},
		{
			name:       "display stage: tool-plan narration stripped without persist fill-ins",
			raw:        liveWinstonMuninnNarration,
			userAsk:    "what is our production database named?",
			afterTools: true,
			persist:    false,
			want:       "Our production database is named yggdrasil.",
		},
		{
			name:       "display stage: relay frame rewritten same as persist",
			raw:        "Sam returned: 56.",
			userAsk:    "what's 7 times 8",
			afterTools: true,
			persist:    false,
			want:       "56 — via Sam.",
		},
		{
			name: "stream stage: tool-plan narration hidden before tools ran",
			raw:  liveWinstonMuninnNarration,
			want: "Our production database is named yggdrasil.",
		},
		{
			name: "stream stage: ordinary prose passes through untouched",
			raw:  "I checked the logs and everything looks fine.",
			want: "I checked the logs and everything looks fine.",
		},
		{
			name: "stream stage: leading tool-call JSON stripped, trailing prose kept",
			raw:  liveMixedJSONProse,
			want: "PONG",
		},
		{
			name: "stream stage: pure tool-call JSON hidden entirely",
			raw:  qwen14bContentJSON,
			want: "",
		},
		{
			name: "stream stage: harness clock label stripped, bare stamp becomes teammate speech",
			raw:  "Local time now: Thursday, August 27, 2026, 12:00 PM ET",
			want: "It's Thursday, August 27, 2026, 12:00 PM ET.",
		},
		{
			name:       "ordinary teammate prose is never touched by the contract",
			raw:        "The build passed and I merged the branch.",
			userAsk:    "how'd the build go?",
			afterTools: true,
			persist:    true,
			want:       "The build passed and I merged the branch.",
		},
		{
			name:       "dotted filename survives sentence-boundary fixup",
			raw:        "See mathutil.go for details.",
			userAsk:    "where's the code?",
			afterTools: true,
			persist:    true,
			want:       "See mathutil.go for details.",
		},
		{
			name:       "dotdir path survives sentence-boundary fixup",
			raw:        "Check the .tmp-t5sandbox directory for details.",
			userAsk:    "where's the code?",
			afterTools: true,
			persist:    true,
			want:       "Check the .tmp-t5sandbox directory for details.",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := FinalizeSpeech(SpeechInput{
				Raw:        c.raw,
				UserAsk:    c.userAsk,
				AfterTools: c.afterTools,
				Persist:    c.persist,
			}).Speech
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestFinalizeSpeech_AttributionOnMixedNastyInput mixes several rule
// triggers in one message and asserts every drop/rewrite that shaped the
// final speech is attributed to a named rule — the observability the old
// ad-hoc pipeline never had.
func TestFinalizeSpeech_AttributionOnMixedNastyInput(t *testing.T) {
	raw := "Local time now: Thursday, August 27, 2026, 12:00 PM ET\n" +
		"I'll use the muninn_recall function to search for information about our production database. " +
		"It seems there was a mistake in my response. I will address the issue directly in this session without delegation. " +
		"Our production database is named yggdrasil. " +
		"Steve reported: DELTA."

	result := FinalizeSpeech(SpeechInput{
		Raw:        raw,
		UserAsk:    "what is our production database named, and say DELTA",
		AfterTools: true,
		Persist:    true,
	})

	if result.Speech == "" {
		t.Fatalf("expected non-empty speech, got empty")
	}
	if len(result.Dropped) == 0 && len(result.Rewrites) == 0 {
		t.Fatalf("expected at least one drop or rewrite to be recorded for nasty input, got none")
	}
	for _, d := range result.Dropped {
		if d.Rule == "" {
			t.Errorf("dropped span %q has no rule attribution", d.Text)
		}
		if d.Text == "" {
			t.Errorf("dropped span under rule %q has empty text", d.Rule)
		}
	}
	for _, rw := range result.Rewrites {
		if rw.Rule == "" {
			t.Errorf("rewrite %q -> %q has no rule attribution", rw.From, rw.To)
		}
	}

	// Every named rule that fired is a rule documented in the FinalizeSpeech
	// spec above — no rule fires unannounced.
	known := map[string]bool{
		"visible-assistant-content":      true,
		"embedded-tool-json":             true,
		"residual-speech-after-tools":    true,
		"harness-visible-tokens":         true,
		"harness-clock-label":            true,
		"time-excuse-sentences":          true,
		"company-wall-rewrite":           true,
		"hostname-fail-rewrite":          true,
		"time-fail-rewrite":              true,
		"invalid-tool-pong-rewrite":      true,
		"leading-bare-clock-sentence":    true,
		"leftover-clock-not-time-ask":    true,
		"leftover-hire-ghost":            true,
		"leftover-delegated-hire":        true,
		"echo-ack-rewrite":               true,
		"statement-fragment-ack-rewrite": true,
		"trivial-ping-fill":              true,
		"trivial-ack-fill":               true,
		"close-incomplete-persist":       true,
		"residual-speech":                true,
	}
	for _, d := range result.Dropped {
		if !known[d.Rule] {
			t.Errorf("dropped span attributed to unknown rule %q", d.Rule)
		}
	}
	for _, rw := range result.Rewrites {
		if !known[rw.Rule] {
			t.Errorf("rewrite attributed to unknown rule %q", rw.Rule)
		}
	}
}

// TestFinalizeSpeech_DebugHookLogsAttribution exercises the
// HUGINN_SPEECH_DEBUG hook without asserting on log output (slog.Debug has
// no return value to assert on) — it only proves SetSpeechDebug/FinalizeSpeech
// do not panic with debug logging enabled, and restores the prior state.
func TestFinalizeSpeech_DebugHookLogsAttribution(t *testing.T) {
	restore := SetSpeechDebug(true)
	defer restore()

	FinalizeSpeech(SpeechInput{
		Raw:        "Local time now: Thursday, August 27, 2026, 12:00 PM ET",
		UserAsk:    "hey",
		AfterTools: true,
		Persist:    true,
	})
}
