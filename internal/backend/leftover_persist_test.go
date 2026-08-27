package backend

import "testing"

const leftoverClockOnly = "Local time now: Thursday, August 27, 2026, 12:00 PM ET"
const leftoverClockSpeech = "It's Thursday, August 27, 2026, 12:00 PM ET."

func TestDropLeftoverClockWhenNotTimeAsk_HeadcountAndHireEmpty(t *testing.T) {
	for _, ask := range []string{
		"how many people are in this channel",
		"who is in this channel",
		"how many people",
		"hire Steve",
		"create an agent",
		"add a teammate",
	} {
		got := PersistVisibleAssistantContent(leftoverClockOnly, ask)
		if got != "" {
			t.Errorf("leftover-clock-only on %q: got %q, want empty", ask, got)
		}
	}
}

func TestDropLeftoverClockWhenNotTimeAsk_TimeAskKeepsClock(t *testing.T) {
	for _, ask := range []string{
		"what time is it",
		"@Winston what time is it",
		"time is it",
		"@Winston what day is it?",
		"current date",
	} {
		got := PersistVisibleAssistantContent(leftoverClockOnly, ask)
		if got != leftoverClockSpeech {
			t.Errorf("time ask %q: got %q, want %q", ask, got, leftoverClockSpeech)
		}
		if containsClockLabel(got) {
			t.Errorf("time ask %q leaked harness label: %q", ask, got)
		}
	}
}

func TestDropLeftoverHireGhost_NonHireTurnDropped(t *testing.T) {
	got := PersistVisibleAssistantContent("They're here.", "how many people")
	if got != "" {
		t.Fatalf("leftover hire-ghost on non-hire turn: %q", got)
	}
	got = PersistVisibleAssistantContent("They're here.", "what time is it")
	if got != "" {
		t.Fatalf("leftover hire-ghost on time ask: %q", got)
	}
	keep := PersistVisibleAssistantContent("They're here.", "hire a teammate")
	if keep != "They're here." {
		t.Fatalf("hire turn dropped ghost: %q", keep)
	}
}

func TestFillTrivialPingPersist_PingOnly(t *testing.T) {
	if got := PersistVisibleAssistantContent(leftoverClockOnly, "@Winston ping"); got != "Pong." {
		t.Fatalf("ping leftover-clock: %q, want Pong.", got)
	}
	if got := PersistVisibleAssistantContent("", "ping"); got != "Pong." {
		t.Fatalf("empty ping persist: %q, want Pong.", got)
	}
	if got := PersistVisibleAssistantContent("", "pong"); got != "Pong." {
		t.Fatalf("empty pong persist: %q, want Pong.", got)
	}
	for _, ask := range []string{
		"how many people",
		"hire Steve",
		"thanks",
		"who is here",
		"who's on the team",
	} {
		if got := PersistVisibleAssistantContent(leftoverClockOnly, ask); got != "" {
			t.Errorf("ask %q filled persist %q, want empty (not Pong.)", ask, got)
		}
		if got := PersistVisibleAssistantContent("", ask); got != "" {
			t.Errorf("empty persist on %q filled %q, want empty", ask, got)
		}
	}
}

func containsClockLabel(s string) bool {
	return hasHarnessClockLabel(s)
}
