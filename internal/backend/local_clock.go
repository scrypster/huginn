package backend

import (
	"regexp"
	"strings"
	"time"
)

const localClockTZ = "America/New_York"

var (
	localClockLineRE = regexp.MustCompile(`(?i)Local time now:\s+(.+?\s+ET)\b`)
	// Speech clock as spoken by Winston/Steve, including optional "at" and **wrap**.
	speechClockStampRE = regexp.MustCompile(`(?i)\*{0,2}((?:Monday|Tuesday|Wednesday|Thursday|Friday|Saturday|Sunday),\s+(?:January|February|March|April|May|June|July|August|September|October|November|December)\s+\d{1,2},\s+\d{4},(?:\s+at)?\s+\d{1,2}:\d{2}\s+(?:AM|PM)\s+ET)\*{0,2}`)
	timeExcuseRE       = regexp.MustCompile(`(?i)cannot directly provide the current time|cannot directly provide real-time|not having access to real-time data|i don'?t have access to real-time|i do not have access to real-time`)
	// "today's date" / "date today" are ordinary ways to ask for the date and
	// were missing here: without them stripLeadingBareClockSentence treats the
	// ask as a non-time ask and deletes the very stamp sentence that answers it.
	timeAskRE = regexp.MustCompile(`(?i)\b(?:what time|current time|time is it|time it is|what day|current date|what(?:'s| is) the date|date is it|today'?s date|date today)\b`)
	// Live Steve hallway recap around a real clock stamp. Used to rewrite
	// leftover time-ask speech to teammate "It's {clock}." without eating
	// the stamp. "contains Winston" is not enough — the post-bounce
	// one-liner still names Winston while recapping.
	leftoverTimeRecapRE = regexp.MustCompile(`(?i)i apologize(?: for the (?:confusion|inconvenience))?(?: earlier)?|it seems there was an issue with the previous response|let me clarify|the (?:first|second) response(?: from \S+)?|indicates that it is currently|cannot directly provide|real-time data|if you need the current time again|(?:assist|help) you further|winston reported|i'?ll wait for the task to complete|then provide the result|understood\.?\s+i'?ll wait`)
	// Injected LocalClockLine prefix. Never teammate speech.
	localTimeNowLabelRE = regexp.MustCompile(`(?i)\s*local time now:\s*`)
)

// nyLocation is America/New_York. Tests and inject use the same zone so the
// stamp is always labeled ET, never a guessed vault or UTC.
func nyLocation() *time.Location {
	loc, err := time.LoadLocation(localClockTZ)
	if err != nil {
		return time.FixedZone("ET", -4*3600)
	}
	return loc
}

// LocalClockStamp is the timezone-labeled clock fragment:
// "Thursday, August 27, 2026, 8:20 AM ET"
func LocalClockStamp(now time.Time) string {
	t := now.In(nyLocation())
	return t.Format("Monday, January 2, 2006, 3:04 PM") + " ET"
}

// LocalClockLine is the one-line system/context inject:
// "Local time now: Thursday, August 27, 2026, 8:20 AM ET"
func LocalClockLine(now time.Time) string {
	return "Local time now: " + LocalClockStamp(now)
}

// AppendLocalClock puts LocalClockLine(now) on s once. A stale
// "Local time now:" line is replaced so a long-lived persona or
// leftover inject cannot freeze the hallway clock.
func AppendLocalClock(s string, now time.Time) string {
	line := LocalClockLine(now)
	if localClockLineRE.MatchString(s) {
		stripped := strings.TrimSpace(localClockLineRE.ReplaceAllString(s, ""))
		for strings.Contains(stripped, "\n\n\n") {
			stripped = strings.ReplaceAll(stripped, "\n\n\n", "\n\n")
		}
		if stripped == "" {
			return line
		}
		return strings.TrimRight(stripped, "\n") + "\n" + line
	}
	if strings.TrimSpace(s) == "" {
		return line
	}
	return strings.TrimRight(s, "\n") + "\n" + line
}

func lastSubmatch(re *regexp.Regexp, s string) string {
	all := re.FindAllStringSubmatch(s, -1)
	if len(all) == 0 {
		return ""
	}
	m := all[len(all)-1]
	if len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func extractClockStamp(s string) string {
	if got := lastSubmatch(localClockLineRE, s); got != "" {
		return got
	}
	if got := lastSubmatch(speechClockStampRE, s); got != "" {
		return got
	}
	return ""
}

func IsTimeAsk(s string) bool {
	return isTimeAsk(s)
}

func isTimeAsk(s string) bool {
	return timeAskRE.MatchString(s)
}

func isTimeExcuseSentence(sent string) bool {
	return timeExcuseRE.MatchString(sent)
}

// dropTimeExcuseSentences removes leftover 14b excuses about having no
// real-time clock. Only called when the user/task is a time ask.
func dropTimeExcuseSentences(s string) string {
	trim := strings.TrimSpace(s)
	if trim == "" {
		return s
	}
	var kept []string
	for _, sent := range splitSentences(trim) {
		// Recap wrappers around a real stamp become teammate clock speech.
		// Never drop the stamp itself.
		if stamp := extractClockStamp(sent); stamp != "" {
			if leftoverTimeRecapRE.MatchString(sent) || hasHarnessClockLabel(sent) {
				kept = append(kept, "It's "+stamp+".")
				continue
			}
			kept = append(kept, sent)
			continue
		}
		if isTimeExcuseSentence(sent) || leftoverTimeRecapRE.MatchString(sent) {
			continue
		}
		kept = append(kept, sent)
	}
	if len(kept) == 0 {
		return ""
	}
	indent := len(s) - len(strings.TrimLeft(s, " \t"))
	if indent > len(s) {
		indent = 0
	}
	return s[:indent] + strings.Join(kept, " ")
}

// teammateTimeFailRewrite turns leftover-empty or leftover-recap time-ask
// speech into teammate "It's {clock}." when a stamp is in leftover/ask or
// the machine clock is available. Does not invent a vault. Does not eat
// a stamp already in leftover — recap wrappers are dropped, the clock stays.
func teammateTimeFailRewrite(stripped, original, userAsk string) string {
	// Only the human/task ask gates a rewrite. Leftover recap that merely
	// mentions "current time" is not itself a time-ask.
	if !isTimeAsk(userAsk) {
		return ""
	}
	stamp := extractClockStamp(stripped)
	if stamp == "" {
		stamp = extractClockStamp(original)
	}
	if stamp == "" {
		stamp = extractClockStamp(userAsk)
	}
	empty := strings.TrimSpace(stripped) == ""
	if stamp == "" {
		if !empty {
			return ""
		}
		stamp = LocalClockStamp(time.Now())
	}
	if stamp == "" {
		return ""
	}
	if empty || leftoverTimeRecapRE.MatchString(stripped) || leftoverTimeRecapRE.MatchString(original) || isBareClockStamp(stripped) || hasHarnessClockLabel(stripped) || hasHarnessClockLabel(original) {
		return "It's " + stamp + "."
	}
	return ""
}

// hasHarnessClockLabel reports the injected LocalClockLine prefix leaking
// into speech ("local time now:" / "Local time now:"). Not teammate talk.
func hasHarnessClockLabel(s string) bool {
	return strings.Contains(strings.ToLower(s), "local time now")
}

func isBareClockStamp(s string) bool {
	if extractClockStamp(s) == "" {
		return false
	}
	rest := speechClockStampRE.ReplaceAllString(s, "")
	rest = strings.ReplaceAll(rest, "Local time now:", "")
	rest = strings.ReplaceAll(rest, "local time now:", "")
	rest = strings.TrimSpace(rest)
	for _, cut := range []string{"*", ".", "-", ":", "—", "–"} {
		rest = strings.Trim(rest, cut)
		rest = strings.TrimSpace(rest)
	}
	return rest == ""
}

// stripLeadingBareClockSentence drops a leading sentence that is ONLY a
// speech clock stamp ("Friday, August 28, 2026, 8:57 AM ET.") glued onto a
// delegated-recap answer ("Steve reported: DELTA.") when the human ask was
// not a time ask. Nobody asked for the clock; the answer sentence(s) that
// follow are returned untouched for downstream rewrites (relay-frame). A
// stamp embedded in prose (not its own leading sentence) is left alone —
// that is the existing embedded-clock behavior. If the stamp is the only
// content, it is left in place so existing leftover handling (e.g.
// dropLeftoverClockWhenNotTimeAsk) still applies.
func stripLeadingBareClockSentence(visible, userAsk string) string {
	if isTimeAsk(userAsk) {
		return visible
	}
	trim := strings.TrimSpace(visible)
	if trim == "" {
		return visible
	}
	sentences := splitSentences(trim)
	if len(sentences) < 2 {
		return visible
	}
	if !isBareClockStamp(sentences[0]) {
		return visible
	}
	rest := strings.TrimSpace(strings.Join(sentences[1:], " "))
	if rest == "" {
		return visible
	}
	return rest
}

// stripHarnessClockLabel removes the injected LocalClockLine prefix from
// persist/speech. A bare stamp left behind becomes teammate "It's {clock}."
// Prose around the stamp stays; only the harness label goes.
func stripHarnessClockLabel(s string) string {
	if !hasHarnessClockLabel(s) {
		return s
	}
	stamp := extractClockStamp(s)
	out := localTimeNowLabelRE.ReplaceAllString(s, " ")
	out = collapseBlankRuns(strings.TrimSpace(out))
	// Collapse doubled spaces left where the label sat mid-sentence.
	for strings.Contains(out, "  ") {
		out = strings.ReplaceAll(out, "  ", " ")
	}
	if stamp == "" {
		return strings.TrimSpace(out)
	}
	if isBareClockStamp(out) || strings.EqualFold(strings.TrimSpace(out), stamp) {
		return "It's " + stamp + "."
	}
	return strings.TrimSpace(out)
}
