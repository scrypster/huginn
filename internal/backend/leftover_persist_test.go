package backend

import (
	"strings"
	"testing"
)

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
	if got := PersistVisibleAssistantContent("", "@Winston ping one"); got != "Pong." {
		t.Fatalf("empty ping one persist: %q, want Pong.", got)
	}
	if got := PersistVisibleAssistantContent("", "@Winston ping three"); got != "Pong." {
		t.Fatalf("empty ping three persist: %q, want Pong.", got)
	}
	for _, ask := range []string{
		"how many people",
		"hire Steve",
		"hire",
		"thanks",
		"who is here",
		"who's on the team",
		"who is on the team",
		"ask Steve",
		"Ask Steve for the hostname",
		"what company",
		"what company is this channel in?",
		"France",
		"what's the capital of France",
	} {
		if got := PersistVisibleAssistantContent(leftoverClockOnly, ask); isLeftoverPongOnly(got) {
			t.Errorf("ask %q leftover-clock filled persist %q, want not Pong.", ask, got)
		}
		if got := PersistVisibleAssistantContent("", ask); isLeftoverPongOnly(got) {
			t.Errorf("empty persist on %q filled %q, want not Pong.", ask, got)
		}
	}
}

func TestFillTrivialPingPersist_LeftoverPongDoesNotLeak(t *testing.T) {
	for _, leftover := range []string{"Pong.", "Pong", "pong.", "pong"} {
		for _, ask := range []string{
			"ask Steve",
			"Ask Steve for the hostname",
			"hire",
			"hire a teammate",
			"what company",
			"what company is this channel in?",
			"who is on the team",
			"France",
			"what's the capital of France",
		} {
			if got := PersistVisibleAssistantContent(leftover, ask); got != "" {
				t.Errorf("leftover %q on %q: %q, want empty (not Pong.)", leftover, ask, got)
			}
		}
	}
	if got := PersistVisibleAssistantContent("Pong.", "ping"); got != "Pong." {
		t.Fatalf("ping leftover-pong: %q, want Pong.", got)
	}
	if got := PersistVisibleAssistantContent("Pong.", "@Winston ping"); got != "Pong." {
		t.Fatalf("mention ping leftover-pong: %q, want Pong.", got)
	}
	if got := PersistVisibleAssistantContent("Pong.", "pong"); got != "Pong." {
		t.Fatalf("pong leftover-pong: %q, want Pong.", got)
	}
	mixed := "Pong.\n\nSteve isn't in Lab. Sam is."
	got := PersistVisibleAssistantContent(mixed, "Ask Steve for the hostname")
	if got != "Steve isn't in Lab. Sam is." {
		t.Fatalf("mixed leftover-pong+wall: %q", got)
	}
	if keep := PersistVisibleAssistantContent("Reggie said PONG.", "what company"); keep != "Reggie said PONG." {
		t.Fatalf("teammate PONG dropped: %q", keep)
	}
}

func containsClockLabel(s string) bool {
	return hasHarnessClockLabel(s)
}

func TestFillTrivialHeadcountPersist_OnlyWhenEmpty(t *testing.T) {
	names := []string{"Winston", "Steve", "Reggie"}
	if got := FillTrivialHeadcountPersist("", "@Winston how many people are in this channel?", names); !containsAll(got, "3", "Winston", "Steve", "Reggie") {
		t.Fatalf("headcount fill: %q", got)
	}
	if got := FillTrivialHeadcountPersist(leftoverClockSpeech, "how many people", names); got != leftoverClockSpeech {
		t.Fatalf("non-empty should stay: %q", got)
	}
	if got := FillTrivialHeadcountPersist("", "ping", names); got != "" {
		t.Fatalf("ping must not get headcount fill: %q", got)
	}
	if got := FillTrivialHeadcountPersist("", "how many people", nil); got != "" {
		t.Fatalf("no names should stay empty: %q", got)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if p != "" && !containsFold(s, p) {
			return false
		}
	}
	return s != ""
}

func containsFold(s, n string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(n))
}

func TestDropLeftoverClockWhenNotTimeAsk_MixedHeadcountKeepsCount(t *testing.T) {
	mixed := leftoverClockSpeech + "\n\nThere are 9 people in this channel."
	got := PersistVisibleAssistantContent(mixed, "how many people are in this channel?")
	if got != "There are 9 people in this channel." {
		t.Fatalf("mixed leftover+headcount: %q", got)
	}
	if containsClockLabel(got) || strings.Contains(got, "ET") {
		t.Fatalf("leftover clock leaked: %q", got)
	}
}

func TestDropLeftoverDateLeadOnHeadcount(t *testing.T) {
	in := "It's Thursday, August 27, 2026, and there are 9 people in the channel: Nova and Steve."
	got := PersistVisibleAssistantContent(in, "how many people are in this channel?")
	if strings.Contains(got, "Thursday") || strings.Contains(got, "2026") {
		t.Fatalf("date lead leaked: %q", got)
	}
	if !strings.Contains(got, "9 people") {
		t.Fatalf("lost headcount: %q", got)
	}
}

func TestDropLeftoverTimeLeadOnHeadcount(t *testing.T) {
	in := "6:45 PM ET. There are 9 people in this channel."
	got := PersistVisibleAssistantContent(in, "how many people are in this channel?")
	if strings.Contains(got, "6:45") || strings.Contains(strings.ToUpper(got), "ET.") {
		t.Fatalf("time lead leaked: %q", got)
	}
	if !strings.Contains(got, "9 people") {
		t.Fatalf("lost headcount: %q", got)
	}
}

func TestDropLeftoverDelegatedHireOnHireTurn(t *testing.T) {
	got := PersistVisibleAssistantContent("Delegated to @Reggie: Create an agent named driveprobe who researches.", "hire a teammate named driveprobe who researches the web")
	if got != "" {
		t.Fatalf("delegated leftover on hire: %q", got)
	}
	keep := PersistVisibleAssistantContent("driveprobe is on the roster as researches. No vault yet. They're seated in Huginn.", "create an agent named driveprobe who researches")
	if !strings.Contains(keep, "driveprobe is on the roster") {
		t.Fatalf("lost hire speech: %q", keep)
	}
	keep2 := PersistVisibleAssistantContent("Delegated to @Steve: ping", "what time is it")
	if keep2 == "" {
		t.Fatal("non-hire delegated speech should not use hire drop")
	}
}

func TestRewriteThirdPersonNotedToFirstPerson(t *testing.T) {
	got := PersistVisibleAssistantContent("@Winston has noted your preferences for your dog Odin and your dietary choice of oat-milk lattes.", "heads up: my dog is named Odin")
	if got != "I've noted that." {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "@Winston") || strings.Contains(strings.ToLower(got), "has noted") {
		t.Fatalf("third person leaked: %q", got)
	}
}

func TestPersistVisibleAssistantContent_ClosesMidClause(t *testing.T) {
	got := PersistVisibleAssistantContent("I'm unable to recall your dog's name from the available")
	if !strings.HasSuffix(got, ".") {
		t.Fatalf("mid-clause persist not closed: %q", got)
	}
	if strings.HasSuffix(got, "..") {
		t.Fatalf("double period: %q", got)
	}
	keep := PersistVisibleAssistantContent("I can't make images.")
	if keep != "I can't make images." {
		t.Fatalf("complete sentence changed: %q", keep)
	}
	q := PersistVisibleAssistantContent("Could you please provide more context or ask a different question?")
	if q != "Could you please provide more context or ask a different question?" {
		t.Fatalf("question closer changed: %q", q)
	}
}

func TestBindPersistToThisTurn_SessionLeftoverPong(t *testing.T) {
	// Session: prior ping persist, then a non-ping ask. Leftover Pong
	// must not write onto the later user row.
	for _, ask := range []string{
		"what company is this channel in?",
		"hire a teammate",
		"Ask Steve for the hostname",
		"thanks",
	} {
		content, write := BindPersistToThisTurn("u-ping", "ping", "u-next", ask, "Pong.")
		if write || content != "" {
			t.Errorf("leftover ping persist onto %q: write=%v content=%q", ask, write, content)
		}
		content, write = BindPersistToThisTurn("u-next", ask, "u-next", ask, "Pong.")
		if !write {
			t.Errorf("same-turn %q should still write (empty after drop)", ask)
		}
		if content != "" {
			t.Errorf("same-turn leftover Pong on %q: %q, want empty", ask, content)
		}
	}
	content, write := BindPersistToThisTurn("u-ping", "ping", "u-ping", "ping", "Pong.")
	if !write || content != "Pong." {
		t.Fatalf("isolated ping: write=%v content=%q, want Pong.", write, content)
	}
	content, write = BindPersistToThisTurn("u1", "ping", "u2", "@Winston ping two", "Pong.")
	if !write || content != "Pong." {
		t.Fatalf("FIFO ping burst: write=%v content=%q, want Pong.", write, content)
	}
}

func TestFillTrivialHeadcountPersist_WhoIsHere(t *testing.T) {
	names := []string{"Winston", "Reggie", "Steve"}
	for _, ask := range []string{"who is here", "who's here", "@Winston who is here"} {
		got := FillTrivialHeadcountPersist("", ask, names)
		if !containsAll(got, "3", "Winston", "Reggie", "Steve") {
			t.Errorf("who-is-here fill %q: %q", ask, got)
		}
	}
	if got := FillTrivialHeadcountPersist("", "who is in Lab", names); got != "" {
		t.Fatalf("Lab roster must not headcount-fill: %q", got)
	}
}
