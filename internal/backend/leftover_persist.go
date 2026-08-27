package backend

import (
	"regexp"
	"strings"
)

var (
	hireGhostRE             = regexp.MustCompile(`(?i)^they'?re here\.?$`)
	leftoverDelegatedHireRE = regexp.MustCompile(`(?i)^delegated to @?[A-Za-z][\w.-]*(:|$)`)
	hireTurnRE              = regexp.MustCompile(`(?i)\b(?:hire|create(?:\s+an)?\s+agent|add(?:\s+a)?\s+teammate|create(?:\s+a)?\s+teammate|create_agent)\b`)
	mentionOnlyRE           = regexp.MustCompile(`(?i)@[\p{L}\p{N}_.-]+`)
	trivialPingRE           = regexp.MustCompile(`(?i)^(ping|pong)[.!?…]*$`)
	leftoverDateLeadRE      = regexp.MustCompile(`(?i)^it'?s\s+(?:Monday|Tuesday|Wednesday|Thursday|Friday|Saturday|Sunday),\s+(?:January|February|March|April|May|June|July|August|September|October|November|December)\s+\d{1,2},\s+\d{4},(?:\s+and\s+)?`)
	leftoverTimeLeadRE      = regexp.MustCompile(`(?i)^(?:it'?s\s+)?\d{1,2}:\d{2}(?:\s*[ap]m)?(?:\s+et)?\.?\s+`)
)

// dropLeftoverClockWhenNotTimeAsk drops persist when the only sayable
// remainder is leftover clock speech ("Local time now: …", a bare leftover
// timestamp, or teammate "It's {stamp}.") and the human ask is not a time
// ask. Time asks keep the ET clock without the harness label.
func dropLeftoverClockWhenNotTimeAsk(visible, userAsk string) string {
	if strings.TrimSpace(visible) == "" {
		return visible
	}
	if isTimeAsk(userAsk) {
		return visible
	}
	if isLeftoverClockOnly(visible) {
		return ""
	}
	var kept []string
	for _, line := range strings.Split(visible, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" {
			kept = append(kept, "")
			continue
		}
		if isLeftoverClockOnly(trim) {
			continue
		}
		kept = append(kept, line)
	}
	out := collapseBlankRuns(strings.TrimSpace(strings.Join(kept, "\n")))
	if leftoverDateLeadRE.MatchString(out) && !isLeftoverClockOnly(out) {
		out = strings.TrimSpace(leftoverDateLeadRE.ReplaceAllString(out, ""))
		if len(out) > 0 {
			out = strings.ToUpper(out[:1]) + out[1:]
		}
	}
	if leftoverTimeLeadRE.MatchString(out) && !isLeftoverClockOnly(out) {
		out = strings.TrimSpace(leftoverTimeLeadRE.ReplaceAllString(out, ""))
		if len(out) > 0 {
			out = strings.ToUpper(out[:1]) + out[1:]
		}
	}
	return out
}

func isLeftoverClockOnly(s string) bool {
	trim := strings.TrimSpace(s)
	if trim == "" {
		return false
	}
	if hasHarnessClockLabel(trim) {
		rest := strings.TrimSpace(stripHarnessClockLabel(trim))
		if rest == "" {
			return true
		}
		return isLeftoverClockOnly(rest)
	}
	if isBareClockStamp(trim) {
		return true
	}
	stamp := extractClockStamp(trim)
	if stamp == "" {
		return false
	}
	for _, want := range []string{
		"It's " + stamp + ".",
		"It's " + stamp,
		"The current time is " + stamp + ".",
		"The current time is " + stamp,
	} {
		if strings.EqualFold(trim, want) {
			return true
		}
	}
	return false
}

// dropLeftoverHireGhost drops leftover hire-ghost "They're here." on
// turns that are not a hire/create/add-teammate ask.

// dropLeftoverDelegatedHire drops 14b hire handoff speech
// ("Delegated to @Reggie: Create an agent…") on a hire turn.
func dropLeftoverDelegatedHire(visible, userAsk string) string {
	if !hireTurnRE.MatchString(userAsk) {
		return visible
	}
	var kept []string
	dropped := false
	for _, line := range strings.Split(visible, "\n") {
		trim := strings.TrimSpace(line)
		if leftoverDelegatedHireRE.MatchString(trim) {
			dropped = true
			continue
		}
		kept = append(kept, line)
	}
	if !dropped {
		return visible
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func dropLeftoverHireGhost(visible, userAsk string) string {
	if hireTurnRE.MatchString(userAsk) {
		return visible
	}
	if hireGhostRE.MatchString(strings.TrimSpace(visible)) {
		return ""
	}
	return visible
}

// fillTrivialPingPersist harness-fills persist `Pong.` when leftover
// drop emptied a trivial ping/pong ask. Does not fill headcount, hire,
// thanks, or who-is-here.
func fillTrivialPingPersist(visible, userAsk string) string {
	if strings.TrimSpace(visible) != "" {
		return visible
	}
	if isTrivialPingAsk(userAsk) {
		return "Pong."
	}
	return visible
}

func isTrivialPingAsk(s string) bool {
	norm := mentionOnlyRE.ReplaceAllString(s, " ")
	norm = strings.ToLower(strings.TrimSpace(norm))
	norm = strings.Join(strings.Fields(norm), " ")
	return trivialPingRE.MatchString(norm)
}

var trivialHeadcountAskRE = regexp.MustCompile(`(?i)\b(?:how many people(?: are(?: in this channel| here)?)?|who(?:'?s| is) in this channel)\b`)

func isTrivialHeadcountAsk(s string) bool {
	norm := mentionOnlyRE.ReplaceAllString(s, " ")
	norm = strings.ToLower(strings.TrimSpace(norm))
	norm = strings.Join(strings.Fields(norm), " ")
	return trivialHeadcountAskRE.MatchString(norm)
}

// FillTrivialHeadcountPersist harness-fills a roster sentence when leftover
// clock drop emptied a headcount ask. names are this space's members.
func FillTrivialHeadcountPersist(visible, userAsk string, names []string) string {
	if strings.TrimSpace(visible) != "" {
		return visible
	}
	if !isTrivialHeadcountAsk(userAsk) {
		return visible
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
		return visible
	}
	return formatHeadcountSpeech(uniq)
}

func formatHeadcountSpeech(names []string) string {
	n := len(names)
	list := ""
	switch n {
	case 1:
		list = names[0]
	case 2:
		list = names[0] + " and " + names[1]
	default:
		list = strings.Join(names[:n-1], ", ") + ", and " + names[n-1]
	}
	noun := "people"
	if n == 1 {
		noun = "person"
	}
	return "There are " + itoa(n) + " " + noun + " in this channel: " + list + "."
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// IsLeftoverClockSpeech reports persist that is only a leftover clock stamp.
func IsLeftoverClockSpeech(s string) bool {
	return isLeftoverClockOnly(s)
}

// PendingHarnessClockPrefix is a partial "Local time now:" that must not
// stream until the stamp arrives and can be rewritten.
func PendingHarnessClockPrefix(s string) bool {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" {
		return false
	}
	want := "local time now:"
	if len(t) >= len(want) {
		return false
	}
	return strings.HasPrefix(want, t)
}

// IsHarnessFillAsk reports a ping/headcount turn whose harness fill
// should persist even when a newer request superseded the run.
func IsHarnessFillAsk(s string) bool {
	return isTrivialPingAsk(s) || isTrivialHeadcountAsk(s)
}
