package agent

import (
	"regexp"
	"strings"

	"github.com/scrypster/huginn/internal/backend"
)

// IsTrivialAsk reports short hallway asks that need no hire, no delegate,
// and no tools. Persona, roster, space/company context, and the local clock
// stay; the tool belt, vault prefetch, and wait_for_threads do not.
//
// TRUE: time/clock/date, exact ping/pong, thanks/ok/acks, who-is-here /
// roster, headcount.
// FALSE: hire/create/add teammate, mesh, company wall, ask Steve,
// "create an agent".
func IsTrivialAsk(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	if isNonTrivialAsk(s) {
		return false
	}
	norm := normalizeTrivialAsk(s)
	if norm == "" {
		return false
	}
	return isTrivialTimeAsk(s, norm) ||
		isTrivialPing(norm) ||
		isTrivialAck(norm) ||
		isTrivialRoster(norm) ||
		isTrivialHeadcount(norm)
}

var (
	mentionRE = regexp.MustCompile(`(?i)@[\p{L}\p{N}_.-]+`)
	hireAskRE = regexp.MustCompile(`(?i)\b(?:hire|create(?:\s+an)?\s+agent|add(?:\s+a)?\s+teammate|create(?:\s+a)?\s+teammate|create_agent)\b`)
	meshAskRE = regexp.MustCompile(`(?i)\bmesh\b`)
	wallAskRE = regexp.MustCompile(`(?i)\b(?:company wall|isn'?t in (?:this )?company|isn'?t in lab)\b`)
	// Hallway addressee plus a second @name is mesh, not a one-line ping.
	trivialTimeRE      = regexp.MustCompile(`(?i)\b(?:what time(?: is it)?|current time|time is it|time it is|what day(?: is it)?|current date|what(?:'s| is) the date|date is it)\b`)
	trivialRosterRE    = regexp.MustCompile(`(?i)^(?:who(?:'s| is) here|who(?:'s| is) on the team|who(?:'s| is) on the roster|roster)$`)
	trivialHeadcountRE = regexp.MustCompile(`(?i)^(?:how many people(?: are(?: in this channel| here)?)?|who(?:'s| is) in this channel)$`)
)

func normalizeTrivialAsk(s string) string {
	s = mentionRE.ReplaceAllString(s, " ")
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, " \t.!?…")
	return s
}

func isNonTrivialAsk(s string) bool {
	if hireAskRE.MatchString(s) || meshAskRE.MatchString(s) || wallAskRE.MatchString(s) {
		return true
	}
	if backend.IsAskSteve(s) {
		return true
	}
	// Two distinct @mentions is mesh / double-at, not a trivial one-liner.
	if len(mentionRE.FindAllString(s, -1)) >= 2 {
		return true
	}
	return false
}

func isTrivialTimeAsk(raw, norm string) bool {
	return trivialTimeRE.MatchString(raw) || trivialTimeRE.MatchString(norm)
}

func isTrivialPing(norm string) bool {
	return norm == "ping" || norm == "pong"
}

func isTrivialAck(norm string) bool {
	switch norm {
	case "thanks", "thank you", "thx", "ty", "ok", "okay", "k", "got it",
		"cool", "cheers", "np", "no problem", "sounds good", "roger", "ack":
		return true
	default:
		return false
	}
}

func isTrivialRoster(norm string) bool {
	return trivialRosterRE.MatchString(norm)
}

func isTrivialHeadcount(norm string) bool {
	return trivialHeadcountRE.MatchString(norm)
}

// stripTrivialAskDelegationTools is the last-chance belt: 14b will call
// Steve if wait_for_threads / delegate_to_agent / consult_agent remain.
func stripTrivialAskDelegationTools(schemas []backend.Tool) []backend.Tool {
	if len(schemas) == 0 {
		return schemas
	}
	deny := map[string]bool{
		"wait_for_threads":  true,
		"delegate_to_agent": true,
		"consult_agent":     true,
	}
	out := schemas[:0]
	for _, s := range schemas {
		if deny[s.Function.Name] {
			continue
		}
		out = append(out, s)
	}
	return out
}
