package backend

import (
	"regexp"
	"strings"
	"unicode"
)

// Relay-frame speech is a small local model recapping a delegated result in
// a stiff third-person wrapper — "Steve reported: DELTA." — instead of the
// answer itself attributed naturally. dropDelegatedAckSentenceRE covers the
// companion defect: "I have delegated the task to X." persisting as the
// FINAL answer even when a real result followed in the same turn.

var (
	// "Steve reported: DELTA." / "Reggie said: PONG." / "Sam returned: 56."
	// Whole-sentence frame only — "Sam reported that the build is green."
	// has no colon and is ordinary prose, left alone.
	//
	// The subject must be Capitalized (a teammate name), NOT case-insensitive:
	// "he said: no." is ordinary prose that must never become "no — via he."
	// Capitalized pronouns/determiners are excluded below for the same reason
	// at a sentence start.
	relayFrameSentenceRE = regexp.MustCompile(`^([A-Z][\w.-]*)\s+(?:reported|said|returned):\s*(.+)$`)
	// Capitalized words that can open a sentence but are never a teammate name.
	notATeammateSubject = map[string]bool{
		"he": true, "she": true, "it": true, "they": true, "we": true,
		"you": true, "i": true, "this": true, "that": true, "there": true,
		"the": true, "who": true, "someone": true, "everyone": true,
		"nobody": true, "one": true, "people": true, "users": true,
	}
	// "I have delegated the task to Steve." / "I've delegated the task to
	// Steve." Harness/model handoff narration, never the answer itself.
	delegatedAckSentenceRE = regexp.MustCompile(`(?i)^i(?:'ve| have) delegated (?:the )?task to\s+[A-Za-z][\w.-]*[?.!]*$`)
)

// rewriteRelayFrameSentence rewrites a single "X reported: Y" / "X said: Y"
// / "X returned: Y" sentence to "Y — via X.", keeping Y verbatim (only its
// own trailing sentence punctuation is trimmed before the attribution is
// appended). Sentences that don't match the frame shape are returned as-is.
func rewriteRelayFrameSentence(sent string) string {
	trim := strings.TrimSpace(sent)
	m := relayFrameSentenceRE.FindStringSubmatch(trim)
	if m == nil {
		return sent
	}
	name := m[1]
	if notATeammateSubject[strings.ToLower(name)] {
		return sent
	}
	y := strings.TrimSpace(m[2])
	y = strings.TrimRight(y, ".!?")
	y = strings.TrimSpace(y)
	if y == "" {
		return sent
	}
	return capitalizeSentenceStart(y) + " — via " + name + "."
}

// rewriteRelayFrameSentences applies rewriteRelayFrameSentence to every
// sentence on line, keeping non-matching sentences untouched. Mirrors the
// dropToolPlanNarrationSentences / dropFutureWaitGlueSentences family: split
// on sentence boundaries, transform, rejoin with the original indent.
func rewriteRelayFrameSentences(line string) string {
	trim := strings.TrimSpace(line)
	if trim == "" {
		return line
	}
	changed := false
	var kept []string
	for _, sent := range splitSentences(trim) {
		rewritten := rewriteRelayFrameSentence(sent)
		if rewritten != sent {
			changed = true
		}
		kept = append(kept, rewritten)
	}
	if !changed {
		return line
	}
	indent := len(line) - len(strings.TrimLeft(line, " \t"))
	return line[:indent] + strings.Join(kept, " ")
}

// dropDelegatedAckWhenResultFollows drops a "I have delegated the task to
// X." sentence when other real content sits alongside it in the same turn
// — the handoff narration is not the answer once a result arrived. If the
// ack is the ONLY content, it is left in place: that is honest and is all
// the user has to go on.
func dropDelegatedAckWhenResultFollows(s string) string {
	trim := strings.TrimSpace(s)
	if trim == "" {
		return s
	}
	sentences := splitSentences(trim)
	if len(sentences) < 2 {
		return s
	}
	var kept []string
	for _, sent := range sentences {
		if delegatedAckSentenceRE.MatchString(strings.TrimSpace(sent)) {
			continue
		}
		kept = append(kept, sent)
	}
	if len(kept) == 0 || len(kept) == len(sentences) {
		// Nothing left, or nothing matched — leave the original alone.
		return s
	}
	return strings.Join(kept, " ")
}

// capitalizeSentenceStart uppercases the first rune of a promoted answer so
// "MJ said: ship it." becomes "Ship it — via MJ." rather than a sentence that
// starts lowercase. Deliberately conservative: an intentionally lowercase
// identifier ("iPhone", "gRPC", "kubectl get pods") is left alone — a
// lowercase first letter immediately followed by an uppercase letter, or a
// non-letter first rune, is never touched.
func capitalizeSentenceStart(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	if !unicode.IsLower(r[0]) {
		return s
	}
	if len(r) > 1 && unicode.IsUpper(r[1]) {
		return s
	}
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
