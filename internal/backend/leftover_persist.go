package backend

import (
	"regexp"
	"strings"
)

var (
	hireGhostRE   = regexp.MustCompile(`(?i)^they'?re here\.?$`)
	hireTurnRE    = regexp.MustCompile(`(?i)\b(?:hire|create(?:\s+an)?\s+agent|add(?:\s+a)?\s+teammate|create(?:\s+a)?\s+teammate|create_agent)\b`)
	mentionOnlyRE = regexp.MustCompile(`(?i)@[\p{L}\p{N}_.-]+`)
	trivialPingRE = regexp.MustCompile(`(?i)^(ping|pong)[.!?…]*$`)
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
	return visible
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
