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
