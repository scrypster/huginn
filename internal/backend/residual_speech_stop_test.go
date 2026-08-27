package backend

import (
	"strings"
	"testing"
)

func TestWaitHasSpecialistAnswer(t *testing.T) {
	if !WaitHasSpecialistAnswer("## Finished threads (1)\nPONG") {
		t.Fatal("PONG wait must count")
	}
	if !WaitHasSpecialistAnswer("The hostname of the machine is 'MJs-MacBook-Pro'.") {
		t.Fatal("hostname wait must count")
	}
	stamp := "Thursday, August 27, 2026, 9:23 AM ET"
	if !WaitHasSpecialistAnswer("## Finished threads (1)\nLocal time now: " + stamp) {
		t.Fatal("clock wait must count")
	}
	if WaitHasSpecialistAnswer("## Finished threads (1)") {
		t.Fatal("bare finished header is not a specialist answer")
	}
	if WaitHasSpecialistAnswer("## Still running (1) — timed out") {
		t.Fatal("timeout must not count")
	}
}

func TestIsCompanyWallDeny(t *testing.T) {
	if !IsCompanyWallDeny("error: Steve isn't in this company.") {
		t.Fatal("prefixed wall deny")
	}
	if !IsCompanyWallDeny("Steve isn't in this company.") {
		t.Fatal("bare wall deny")
	}
	if !IsCompanyWallDeny("Steve isn't in Lab. Sam is.") {
		t.Fatal("Lab wall line")
	}
	if IsCompanyWallDeny("error: agent \"Steve\" is not a member of this space") {
		t.Fatal("space-member deny is not the company wall")
	}
	if IsCompanyWallDeny("Delegated to Sam") {
		t.Fatal("success is not a wall")
	}
}

func TestPersistStopTurn_AskSteveWall(t *testing.T) {
	got := PersistStopTurn("", "error: Steve isn't in this company.", "Ask Steve for the hostname")
	if got != "Steve isn't in Lab. Sam is." {
		t.Fatalf("got %q, want Lab wall", got)
	}
	if strings.Contains(got, "I'll ask Sam") || strings.Contains(got, "has been given the task") {
		t.Fatalf("glue leaked: %q", got)
	}
}

func TestPersistStopTurn_WaitPONG(t *testing.T) {
	wait := "## Finished threads (1)\n\n## Result from agent \"Reggie\"\n\nPONG\n\nThread ID: `th_1`\n"
	got := PersistStopTurn("I'll ask Sam again.", wait, "ping Reggie")
	if got != "PONG" {
		t.Fatalf("got %q, want PONG", got)
	}
}

func TestPersistStopTurn_WaitClock(t *testing.T) {
	stamp := "Thursday, August 27, 2026, 9:23 AM ET"
	wait := "## Finished threads (1)\nLocal time now: " + stamp
	got := PersistStopTurn("", wait, "ask Winston what time it is")
	if got != "It's "+stamp+"." {
		t.Fatalf("got %q", got)
	}
}

func TestPersistStopTurn_DoesNotRegressLiveLabAskSteveGlue(t *testing.T) {
	// Existing persist path still collapses leftover glue. Stop-turn must
	// not change that when speech already has the wall+glue.
	got := PersistVisibleAssistantContent(liveLabAskSteveGlue, "Ask Steve for the hostname")
	if got != "Steve isn't in Lab. Sam is." {
		t.Fatalf("persist regress: %q", got)
	}
}

func TestPersistStopTurn_WaitClockLatestOfMany(t *testing.T) {
	// Afternoon drive 2026-08-27: session-wide wait re-dumped 9:11 AM then
	// 9:50 AM. Steve persist took the first stamp. Latest must win.
	wait := "## Finished threads (2)\n\n" +
		"## Result from agent \"Winston\"\n\n**Summary:** Local time now: Thursday, August 27, 2026, 9:11 AM ET\n\n**Status:** completed-with-timeout\n\nThread ID: `old`\n\n" +
		"## Result from agent \"Winston\"\n\n**Summary:** Local time now: Thursday, August 27, 2026, 2:23 PM ET\n\n**Status:** completed-with-timeout\n\nThread ID: `new`\n"
	got := PersistStopTurn("", wait, "Can you ask Winston what time it is")
	if got != "It's Thursday, August 27, 2026, 2:23 PM ET." {
		t.Fatalf("got %q, want afternoon stamp", got)
	}
}
