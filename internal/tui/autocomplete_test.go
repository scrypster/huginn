package tui_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/tui"
)

func TestExtractAtPrefix_MatchesAfterSpace(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"hello @Sta", "Sta"},
		{"@Al", "Al"},
		{" @Ch", "Ch"},
		{"no match", ""},
		{"email@domain.com", ""},  // email-style: no whitespace before @
		{"@", ""},                 // too short (0 chars after @)
		{"@S", ""},                // 1 char — below 2-char threshold
		{"@St", "St"},             // exactly 2 chars → trigger
		{"foo @", ""},             // @ at end, 0 chars after
		{"foo @A", ""},            // 1 char after @
	}
	for _, tc := range cases {
		got := tui.ExtractAtPrefix(tc.input)
		if got != tc.want {
			t.Errorf("ExtractAtPrefix(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestFilterAgentNames_CaseInsensitive(t *testing.T) {
	names := []string{"Stacy", "Sam", "Steve"}
	got := tui.FilterAgentNames(names, "st")
	if len(got) != 2 {
		t.Errorf("expected 2 matches (Stacy, Steve), got %v", got)
	}
}

func TestFilterAgentNames_EmptyPrefix_ReturnsAll(t *testing.T) {
	names := []string{"Stacy", "Sam"}
	got := tui.FilterAgentNames(names, "")
	if len(got) != 2 {
		t.Errorf("expected all 2 agents, got %v", got)
	}
}

func TestFilterAgentNames_NoMatch_ReturnsEmpty(t *testing.T) {
	names := []string{"Stacy", "Sam"}
	got := tui.FilterAgentNames(names, "xyz")
	if len(got) != 0 {
		t.Errorf("expected no matches, got %v", got)
	}
}

func TestFilterAgentNames_ExactMatch(t *testing.T) {
	names := []string{"Stacy", "Sam"}
	got := tui.FilterAgentNames(names, "Stacy")
	if len(got) != 1 || got[0] != "Stacy" {
		t.Errorf("expected [Stacy], got %v", got)
	}
}

func TestExtractAtPrefix_NoAt_ReturnsEmpty(t *testing.T) {
	if got := tui.ExtractAtPrefix("hello world"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestExtractAtPrefix_AtBetweenWords_NoSpace_IsEmail(t *testing.T) {
	// "user@host" — @ not preceded by whitespace → treat as email, no trigger
	if got := tui.ExtractAtPrefix("user@host"); got != "" {
		t.Errorf("expected empty for email-style @, got %q", got)
	}
}
