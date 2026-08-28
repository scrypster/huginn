package backend

import (
	"regexp"
	"strings"
	"unicode"
)

// echoAckReply is the harness-fill for a turn that is otherwise a
// near-verbatim echo of what the user just said. Short and in-voice, not a
// repeat of the user's own words.
const echoAckReply = "Noted."

// qualityFragmentRE matches a bare <=2-word quality-judgment fragment
// ("Not helpful", "helpful", "Not useful.") — not an answer to anything,
// just a judgment word a weak model echoed back on its own after tools
// stripped everything else away.
var qualityFragmentRE = regexp.MustCompile(`(?i)^(?:not\s+)?(?:helpful|useful|good|great|nice|accurate|correct|right|wrong|bad)[.!]?$`)

// EchoAckRewrite rewrites persisted assistant speech that is a near-verbatim
// echo of the user's own message into a short in-voice acknowledgment.
//
// A statement-shaped ask ("for the record: our staging server is called
// valkyrie") sometimes gets a tool-thin/empty turn from a weak model, and
// what comes back is the user's own sentence parroted as if it were the
// model's reply. That reads as no acknowledgment at all — worse than
// silence, because it looks like a reply. Persisting it verbatim is never
// correct: the user already knows what they said.
func EchoAckRewrite(visible, userAsk string) string {
	v := strings.TrimSpace(visible)
	ask := strings.TrimSpace(userAsk)
	if v == "" || ask == "" {
		return visible
	}
	if !isNearVerbatimEcho(v, ask) {
		return visible
	}
	return echoAckReply
}

// isNearVerbatimEcho reports whether visible is (after normalizing case,
// punctuation, whitespace, and a leading @mention) essentially the same
// text as the user's ask. Exact match after normalizing catches the common
// case (mention stripped, first letter re-cased); a token-level Jaccard
// fallback catches minor rephrasing/reordering. Short strings are excluded
// so ordinary brief replies ("yes", "got it") are never mistaken for echo.
func isNearVerbatimEcho(visible, ask string) bool {
	nv := normalizeForEchoCompare(visible)
	na := normalizeForEchoCompare(ask)
	vt := strings.Fields(nv)
	at := strings.Fields(na)
	if len(vt) < 3 || len(at) < 3 {
		return false
	}
	if nv == na {
		return true
	}
	vset := make(map[string]struct{}, len(vt))
	for _, w := range vt {
		vset[w] = struct{}{}
	}
	aset := make(map[string]struct{}, len(at))
	for _, w := range at {
		aset[w] = struct{}{}
	}
	inter := 0
	for w := range vset {
		if _, ok := aset[w]; ok {
			inter++
		}
	}
	union := len(vset) + len(aset) - inter
	if union == 0 {
		return false
	}
	sim := float64(inter) / float64(union)
	lenRatio := float64(len(vt)) / float64(len(at))
	if lenRatio < 1 {
		lenRatio = 1 / lenRatio
	}
	return sim >= 0.85 && lenRatio <= 1.3
}

// StatementFragmentAckRewrite rewrites persisted assistant speech that is a
// bare quality-judgment fragment ("Not helpful", "helpful") into the
// in-voice ack, but only for a statement turn — the user was not asking a
// question. A question turn ("was that helpful?") can legitimately get a
// real short one-word answer, so it is left alone; only a statement turn
// ("that answer wasn't helpful.") has no question for a one-word fragment
// to be answering, so it reads as leftover echo rather than a reply.
func StatementFragmentAckRewrite(visible, userAsk string) string {
	v := strings.TrimSpace(visible)
	if v == "" || isQuestionAsk(userAsk) {
		return visible
	}
	if !qualityFragmentRE.MatchString(v) {
		return visible
	}
	return echoAckReply
}

// isQuestionAsk reports whether the user's line is phrased as a question:
// ends in "?" or opens with a common question word/auxiliary.
func isQuestionAsk(ask string) bool {
	a := strings.TrimSpace(mentionOnlyRE.ReplaceAllString(ask, " "))
	a = strings.TrimSpace(a)
	if a == "" {
		return false
	}
	if strings.HasSuffix(a, "?") {
		return true
	}
	fields := strings.Fields(a)
	if len(fields) == 0 {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "what", "why", "how", "who", "whom", "whose", "when", "where", "which",
		"is", "are", "was", "were", "am", "can", "could", "would", "will", "shall",
		"do", "does", "did", "should", "isn't", "aren't", "wasn't", "weren't":
		return true
	}
	return false
}

// normalizeForEchoCompare lowercases, drops a leading @mention, and
// collapses everything that is not a letter or digit to single spaces.
func normalizeForEchoCompare(s string) string {
	s = mentionOnlyRE.ReplaceAllString(s, " ")
	s = strings.ToLower(s)
	var b strings.Builder
	lastSpace := true
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			b.WriteByte(' ')
		}
		lastSpace = true
	}
	return strings.TrimSpace(b.String())
}
