package server

// Regression test for the @mention delegation bug:
//
// Before the fix, ws.go passed `userMsg` (the user's original message) to
// mentionDelegate instead of the assistant's response. This meant @AgentName
// mentions in the assistant's reply were never parsed, and no threads were
// spawned via the mention-based delegation path.
//
// The fix changes ws.go line ~937 to pass `assistantBuf.String()` (the
// accumulated assistant response) instead of `userMsg`.
//
// This test verifies that ParseMentions correctly extracts agent names from
// realistic assistant responses and would NOT extract them from the user
// messages that were previously (incorrectly) being passed.

import (
	"testing"

	"github.com/scrypster/huginn/internal/threadmgr"
)

func TestMentionDelegate_AssistantResponseContainsMentions(t *testing.T) {
	knownAgents := []string{"Sam", "Mike", "Adam", "Tom"}

	// The assistant response contains @mentions — this is what should be parsed.
	assistantResponse := "@Sam is on it — check the thread for his findings."
	requests := threadmgr.ParseMentions(assistantResponse, knownAgents)
	if len(requests) != 1 {
		t.Fatalf("expected 1 delegation from assistant response, got %d", len(requests))
	}
	if requests[0].AgentName != "Sam" {
		t.Errorf("expected Sam, got %q", requests[0].AgentName)
	}
}

func TestMentionDelegate_UserMessageDoesNotContainMentions(t *testing.T) {
	knownAgents := []string{"Sam", "Mike", "Adam", "Tom"}

	// The user message does NOT contain @mentions — parsing this should yield nothing.
	// This was the pre-fix behavior: mentionDelegate received userMsg, found no @mentions,
	// and no threads were created.
	userMsg := "Tom, please delegate a task to Sam: have him explain what applyToolbelt does."
	requests := threadmgr.ParseMentions(userMsg, knownAgents)
	if len(requests) != 0 {
		t.Errorf("user message should NOT trigger delegation (no @mention syntax), got %d requests", len(requests))
	}
}

func TestMentionDelegate_AssistantMultipleMentions(t *testing.T) {
	knownAgents := []string{"Sam", "Mike", "Adam", "Tom"}

	// Tom delegates to multiple agents in one response.
	assistantResponse := "I'll split this up: @Sam will handle the architecture review and @Mike will implement the fix. @Adam please prepare tests."
	requests := threadmgr.ParseMentions(assistantResponse, knownAgents)
	if len(requests) != 3 {
		t.Fatalf("expected 3 delegations, got %d", len(requests))
	}
	names := make(map[string]bool)
	for _, r := range requests {
		names[r.AgentName] = true
	}
	for _, expected := range []string{"Sam", "Mike", "Adam"} {
		if !names[expected] {
			t.Errorf("expected %q in delegations, got %v", expected, names)
		}
	}
}

func TestMentionDelegate_LeadAgentMentionedInOwnResponse_NotDelegated(t *testing.T) {
	// If Tom (lead) mentions himself, ParseMentions will return it, but
	// CreateFromMentions should skip it (self-delegation guard).
	// Here we just verify ParseMentions finds it — the guard is in CreateFromMentions.
	knownAgents := []string{"Sam", "Tom"}

	assistantResponse := "@Tom I'll handle the overview, @Sam please check the tests."
	requests := threadmgr.ParseMentions(assistantResponse, knownAgents)
	// Both Tom and Sam mentioned
	if len(requests) != 2 {
		t.Fatalf("expected 2 mentions parsed, got %d", len(requests))
	}
}

func TestMentionDelegate_EmptyAssistantResponse_NoDelegation(t *testing.T) {
	knownAgents := []string{"Sam", "Mike"}
	requests := threadmgr.ParseMentions("", knownAgents)
	if len(requests) != 0 {
		t.Errorf("empty response should yield 0 requests, got %d", len(requests))
	}
}
