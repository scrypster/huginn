package threadmgr

import (
	"testing"
)

func TestParseMentions_SingleMention(t *testing.T) {
	requests := ParseMentions("@Stacy fix the login bug", []string{"Stacy", "Bob"})
	if len(requests) != 1 {
		t.Fatalf("expected 1 DelegationRequest, got %d", len(requests))
	}
	if requests[0].AgentName != "Stacy" {
		t.Errorf("expected AgentName 'Stacy', got %q", requests[0].AgentName)
	}
	if requests[0].Task != "@Stacy fix the login bug" {
		t.Errorf("task should be full message, got %q", requests[0].Task)
	}
}

func TestParseMentions_MultipleMentions(t *testing.T) {
	requests := ParseMentions("@Stacy and @Bob work together", []string{"Stacy", "Bob"})
	if len(requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(requests))
	}
	names := map[string]bool{requests[0].AgentName: true, requests[1].AgentName: true}
	if !names["Stacy"] || !names["Bob"] {
		t.Errorf("expected Stacy and Bob, got %v", requests)
	}
}

func TestParseMentions_UnknownAgentIgnored(t *testing.T) {
	requests := ParseMentions("@Unknown do something", []string{"Stacy", "Bob"})
	if len(requests) != 0 {
		t.Errorf("unknown agent should be ignored, got: %v", requests)
	}
}

func TestParseMentions_CaseInsensitiveMatch(t *testing.T) {
	requests := ParseMentions("@stacy fix it", []string{"Stacy"})
	if len(requests) != 1 {
		t.Fatalf("expected 1 request (case-insensitive), got %d", len(requests))
	}
	if requests[0].AgentName != "Stacy" {
		t.Errorf("expected canonical name 'Stacy', got %q", requests[0].AgentName)
	}
}

func TestParseMentions_NoMentions(t *testing.T) {
	requests := ParseMentions("just a regular message", []string{"Stacy"})
	if len(requests) != 0 {
		t.Errorf("expected 0 requests for no mentions, got %d", len(requests))
	}
}

func TestParseMentions_EscapedDoubleat(t *testing.T) {
	// @@AgentName should NOT trigger delegation
	requests := ParseMentions("email me at user@@domain.com", []string{"domain"})
	if len(requests) != 0 {
		t.Errorf("@@ prefix should not trigger delegation, got: %v", requests)
	}
}

func TestParseMentions_WordBoundary(t *testing.T) {
	// @StacyFoo should not match agent "Stacy"
	requests := ParseMentions("@StacyFoo fix it", []string{"Stacy"})
	if len(requests) != 0 {
		t.Errorf("@StacyFoo should not match agent 'Stacy', got: %v", requests)
	}
}

func TestParseMentions_EmptyMessage(t *testing.T) {
	requests := ParseMentions("", []string{"Stacy"})
	if len(requests) != 0 {
		t.Errorf("empty message should produce 0 requests, got %d", len(requests))
	}
}

func TestParseMentions_EmailStyleNotMatched(t *testing.T) {
	requests := ParseMentions("send to alice@Bob for review", []string{"Bob"})
	if len(requests) != 0 {
		t.Errorf("email-style address should not trigger delegation, got: %v", requests)
	}
}

func TestParseMentions_HyphenatedName(t *testing.T) {
	// Hyphenated agent names e.g. @Sam-Johnson are supported by MentionRe [\w-]+.
	requests := ParseMentions("@Sam-Johnson please fix the auth service", []string{"Sam-Johnson"})
	if len(requests) != 1 {
		t.Fatalf("expected 1 request for hyphenated name, got %d", len(requests))
	}
	if requests[0].AgentName != "Sam-Johnson" {
		t.Errorf("expected canonical name 'Sam-Johnson', got %q", requests[0].AgentName)
	}
}

func TestParseMentions_HyphenatedNameCaseInsensitive(t *testing.T) {
	requests := ParseMentions("@sam-johnson look into this", []string{"Sam-Johnson"})
	if len(requests) != 1 {
		t.Fatalf("expected 1 request (case-insensitive hyphenated), got %d", len(requests))
	}
	if requests[0].AgentName != "Sam-Johnson" {
		t.Errorf("expected canonical name 'Sam-Johnson', got %q", requests[0].AgentName)
	}
}

func TestParseMentions_MentionReExported(t *testing.T) {
	// MentionRe is exported so ws.go can build dedup exclusion sets with the same
	// regex. Verify it matches the expected pattern.
	if MentionRe == nil {
		t.Fatal("MentionRe should be non-nil exported var")
	}
	matches := MentionRe.FindAllStringSubmatch("Hello @Sam please help", -1)
	if len(matches) != 1 || matches[0][1] != "Sam" {
		t.Errorf("MentionRe should extract 'Sam', got %v", matches)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Hardening: Self-delegation guard in CreateFromMentions (review/agents-channels-dm-hardening)
// ────────────────────────────────────────────────────────────────────────────

// TestParseMentions_SelfDelegationGuard verifies that ParseMentions correctly
// returns mentions even when the caller is in the mention list, since
// the self-delegation filtering happens in CreateFromMentions (not ParseMentions).
// ParseMentions is a stateless utility that just returns all matches.
func TestParseMentions_SelfDelegationGuard(t *testing.T) {
	// This test verifies that ParseMentions returns all matches including the caller.
	// The filtering is done by CreateFromMentions using the callerAgent parameter.
	names := []string{"Tom", "Sam"}
	reqs := ParseMentions("@Tom and @Sam work together", names)

	// ParseMentions should return both mentions (the filtering is elsewhere).
	if len(reqs) != 2 {
		t.Fatalf("expected 2 requests from ParseMentions, got %d", len(reqs))
	}

	agentSet := make(map[string]bool)
	for _, r := range reqs {
		agentSet[r.AgentName] = true
	}
	if !agentSet["Tom"] || !agentSet["Sam"] {
		t.Errorf("expected both Tom and Sam in mentions, got: %v", agentSet)
	}
}
