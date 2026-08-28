package backend

import (
	"strings"
	"testing"
	"time"
)

func TestLocalClockLine_TimezoneLabeledET(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("America/New_York: %v", err)
	}
	now := time.Date(2026, 8, 27, 8, 20, 0, 0, loc)
	got := LocalClockLine(now)
	want := "Local time now: Thursday, August 27, 2026, 8:20 AM ET"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if !strings.Contains(got, " ET") {
		t.Fatalf("clock must be timezone-labeled ET: %q", got)
	}
	if strings.Contains(got, "EDT") || strings.Contains(got, "EST") || strings.Contains(got, "UTC") {
		t.Fatalf("label must be ET, not EDT/EST/UTC: %q", got)
	}
}

func TestLocalClockStamp_Afternoon(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("America/New_York: %v", err)
	}
	now := time.Date(2026, 8, 27, 14, 5, 0, 0, loc)
	got := LocalClockStamp(now)
	want := "Thursday, August 27, 2026, 2:05 PM ET"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAppendLocalClock_Once(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("America/New_York: %v", err)
	}
	now := time.Date(2026, 8, 27, 8, 20, 0, 0, loc)
	line := LocalClockLine(now)
	got := AppendLocalClock("You are Steve.", now)
	if !strings.Contains(got, "You are Steve.") {
		t.Fatalf("lost persona: %q", got)
	}
	if !strings.Contains(got, line) {
		t.Fatalf("missing clock line: %q", got)
	}
	again := AppendLocalClock(got, now)
	if strings.Count(again, "Local time now:") != 1 {
		t.Fatalf("clock line injected twice: %q", again)
	}
}

func TestExtractClockStamp(t *testing.T) {
	s := "Local time now: Thursday, August 27, 2026, 8:20 AM ET\nMore context."
	if got := extractClockStamp(s); got != "Thursday, August 27, 2026, 8:20 AM ET" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractClockStamp_SpeechWithAtAndMarkdown(t *testing.T) {
	s := "Let me clarify: it is currently **Thursday, August 27, 2026, at 8:42 AM ET**."
	if got := extractClockStamp(s); got != "Thursday, August 27, 2026, at 8:42 AM ET" {
		t.Fatalf("got %q", got)
	}
}

func TestAppendLocalClock_RefreshesStale(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("America/New_York: %v", err)
	}
	morning := time.Date(2026, 8, 27, 9, 11, 0, 0, loc)
	afternoon := time.Date(2026, 8, 27, 14, 23, 0, 0, loc)
	got := AppendLocalClock("You are Winston.\n"+LocalClockLine(morning), afternoon)
	if strings.Contains(got, "9:11 AM") {
		t.Fatalf("stale morning clock stayed: %q", got)
	}
	if !strings.Contains(got, LocalClockLine(afternoon)) {
		t.Fatalf("missing afternoon clock: %q", got)
	}
	if strings.Count(got, "Local time now:") != 1 {
		t.Fatalf("want one clock line: %q", got)
	}
}

func TestExtractClockStamp_LatestWins(t *testing.T) {
	s := "## Finished threads (2)\n" +
		"Local time now: Thursday, August 27, 2026, 9:11 AM ET\n" +
		"Local time now: Thursday, August 27, 2026, 2:23 PM ET\n"
	got := extractClockStamp(s)
	want := "Thursday, August 27, 2026, 2:23 PM ET"
	if got != want {
		t.Fatalf("got %q, want latest %q", got, want)
	}
}

// Live 2026-08-28 8:57 AM ET Winston->Steve delegated-recap hallway reply:
// a leading bare clock stamp sentence nobody asked for, glued onto the
// relay frame. Not a time ask ("say DELTA") — the stamp sentence must go,
// the answer sentence must stay untouched for the relay-frame rewrite.
const liveDelegatedRecapClockStamp = `Friday, August 28, 2026, 8:57 AM ET. Steve reported: DELTA.`

func TestStripLeadingBareClockSentence_NotTimeAskDropsStamp(t *testing.T) {
	got := stripLeadingBareClockSentence(liveDelegatedRecapClockStamp, "say DELTA")
	if strings.Contains(got, "8:57 AM ET") {
		t.Fatalf("leading clock stamp not stripped: %q", got)
	}
	want := "Steve reported: DELTA."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStripLeadingBareClockSentence_TimeAskKeepsStamp(t *testing.T) {
	got := stripLeadingBareClockSentence(liveDelegatedRecapClockStamp, "what time is it")
	if !strings.Contains(got, "8:57 AM ET") {
		t.Fatalf("time-ask stamp was stripped: %q", got)
	}
	if got != liveDelegatedRecapClockStamp {
		t.Fatalf("time-ask content mutated: %q", got)
	}
}

func TestStripLeadingBareClockSentence_SoleContentUntouched(t *testing.T) {
	stamp := "Friday, August 28, 2026, 8:57 AM ET."
	got := stripLeadingBareClockSentence(stamp, "say DELTA")
	if got != stamp {
		t.Fatalf("fall back to existing leftover handling: got %q, want unchanged %q", got, stamp)
	}
}

func TestStripLeadingBareClockSentence_EmbeddedStampNotLeading(t *testing.T) {
	// Stamp embedded in prose (not its own leading sentence) is untouched —
	// existing embedded-clock behavior (TestPersistVisibleAssistantContent_LiveSteveHallwayClockRecapNotTimeAsk)
	// must not regress.
	s := "The first response from Winston indicates that it is currently Thursday, August 27, 2026, at 8:42 AM ET."
	got := stripLeadingBareClockSentence(s, "hello")
	if got != s {
		t.Fatalf("embedded stamp sentence mutated: %q", got)
	}
}

// "what's today's date" is an ordinary date ask. It must count as a time ask,
// or stripLeadingBareClockSentence deletes the stamp sentence that answers it.
func TestIsTimeAsk_TodaysDatePhrasings(t *testing.T) {
	for _, ask := range []string{
		"what's today's date",
		"whats todays date?",
		"can you tell me the date today",
	} {
		if !isTimeAsk(ask) {
			t.Errorf("isTimeAsk(%q) = false, want true", ask)
		}
	}
	got := stripLeadingBareClockSentence("Friday, August 28, 2026, 8:57 AM ET. Anything else?", "what's today's date")
	if !strings.Contains(got, "8:57 AM ET") {
		t.Errorf("date-ask stamp was stripped: %q", got)
	}
}
