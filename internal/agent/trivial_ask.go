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
	if _, ok := TrivialSayEchoWord(s); ok {
		return true
	}
	return isTrivialTimeAsk(s, norm) ||
		isTrivialPing(norm) ||
		backend.IsTrivialAckAsk(s) ||
		isTrivialRoster(norm) ||
		isTrivialHeadcount(norm) ||
		backend.IsNamedCompanyRosterAsk(s)
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
	if norm == "ping" || norm == "pong" {
		return true
	}
	// Burst prove: "@Winston ping one" is still a ping (SNAP-0.8).
	if strings.HasPrefix(norm, "ping ") {
		rest := strings.TrimSpace(strings.TrimPrefix(norm, "ping "))
		return rest == "one" || rest == "two" || rest == "three" || (len(rest) <= 2 && rest != "")
	}
	return false
}

// IsTrivialPingAsk is ping/pong including burst "ping one".
func IsTrivialPingAsk(s string) bool {
	return isTrivialPing(normalizeTrivialAsk(s))
}

func isTrivialAck(norm string) bool {
	switch norm {
	case "thanks", "thank you", "thx", "ty", "ok", "okay", "k", "got it",
		"cool", "cheers", "np", "no problem", "sounds good", "roger", "ack",
		"good morning", "morning", "gm":
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

var (
	// trivialSayEchoRE finds a trailing "say/repeat/echo <tail>" clause.
	// Anchored to end-of-string so a "burst two-B:" style prefix ahead of
	// the verb does not stop the match, but any real content the verb
	// introduces (a full sentence, not a bare word) still shows up in the
	// captured tail for the word-count/stopword checks below to reject.
	trivialSayEchoRE = regexp.MustCompile(`(?i)\b(?:say|repeat|echo)\s+(.+)$`)
	// trivialWordRE requires each tail token to look like a single bare
	// word (letters/digits/hyphen/apostrophe only) — no spaces-within-token
	// punctuation that would signal real sentence content.
	trivialWordRE = regexp.MustCompile(`^[\p{L}\p{N}'-]+$`)
)

// trivialSayStopWords are common connector/function words that, if present
// in the tail, mean this is a real sentence ("say something about our
// roadmap"), not a bare word to echo back.
var trivialSayStopWords = map[string]bool{
	"about": true, "regarding": true, "on": true, "to": true, "that": true,
	"this": true, "what": true, "how": true, "why": true, "please": true,
	"something": true, "someone": true, "anything": true, "everything": true,
	"nothing": true, "your": true, "my": true, "our": true, "the": true,
	"a": true, "an": true, "is": true, "are": true, "was": true, "were": true,
	"and": true, "or": true, "but": true, "if": true, "when": true, "then": true,
	"so": true, "because": true, "of": true, "for": true, "with": true,
}

// TrivialSayEchoWord reports whether userMsg is a trivial "say <WORD>" /
// "repeat <WORD>" / "echo <WORD>" ask — a bare word (up to 3 tokens) with
// no other content after the verb, once @mentions and a leading
// "burst ...:" style prefix are set aside. When it matches, the tail is
// exactly what should be spoken back — zero delegation, zero LLM.
// A real sentence ("say something about our roadmap") must NOT match: the
// word-count cap and stopword list both guard against that.
func TrivialSayEchoWord(s string) (string, bool) {
	stripped := mentionRE.ReplaceAllString(s, " ")
	stripped = strings.TrimSpace(stripped)
	m := trivialSayEchoRE.FindStringSubmatch(stripped)
	if m == nil {
		return "", false
	}
	tail := strings.Trim(strings.TrimSpace(m[1]), " \t.!?…")
	if tail == "" {
		return "", false
	}
	words := strings.Fields(tail)
	if len(words) == 0 || len(words) > 3 {
		return "", false
	}
	for _, w := range words {
		lw := strings.ToLower(strings.Trim(w, ".,!?;:'\""))
		if trivialSayStopWords[lw] {
			return "", false
		}
		if !trivialWordRE.MatchString(w) {
			return "", false
		}
	}
	return tail, true
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

// channelMembersLine is the one-line injected roster for trivial
// headcount / who-is-here so 14b answers from THIS channel, not the desk.
func channelMembersLine(userMsg string, names []string) string {
	norm := normalizeTrivialAsk(userMsg)
	if !isTrivialRoster(norm) && !isTrivialHeadcount(norm) {
		return ""
	}
	seen := map[string]bool{}
	var uniq []string
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[strings.ToLower(n)] {
			continue
		}
		seen[strings.ToLower(n)] = true
		uniq = append(uniq, n)
	}
	if len(uniq) == 0 {
		return ""
	}
	return "this channel members: " + strings.Join(uniq, ", ")
}

func namedCompanyMembersLine(company string, names []string) string {
	company = strings.TrimSpace(company)
	if company == "" {
		return ""
	}
	seen := map[string]bool{}
	var uniq []string
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[strings.ToLower(n)] {
			continue
		}
		seen[strings.ToLower(n)] = true
		uniq = append(uniq, n)
	}
	if len(uniq) == 0 {
		return ""
	}
	return company + " members: " + strings.Join(uniq, ", ")
}
