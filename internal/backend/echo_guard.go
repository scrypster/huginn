package backend

import (
	"strings"
	"unicode"
)

// echoAckReply is the harness-fill for a turn that is otherwise a
// near-verbatim echo of what the user just said. Short and in-voice, not a
// repeat of the user's own words.
const echoAckReply = "Noted."

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
