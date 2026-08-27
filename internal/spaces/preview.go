package spaces

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"
)

const maxLastPreviewRunes = 80

// Caps so a 14b wake does not drown in a long Slack thread.
const (
	maxThreadTranscriptReplies = 16
	maxThreadLineRunes         = 400
	maxThreadTranscriptRunes   = 4000
)

var harnessToolNames = []string{
	"wait_for_threads",
	"delegate_to_agent",
	"list_team_status",
	"recall_thread_result",
	"bash",
}

// ReplySpeech returns honesty-stripped teammate/human speech (no rune cap).
// TOOL_FAIL, DELEGATE_FAIL, harness JSON, wait_for_threads, and delegation
// announcements become empty so they never enter a chip or a wake transcript.
func ReplySpeech(content string) string {
	raw := strings.TrimSpace(content)
	if raw == "" {
		return ""
	}
	if isHarnessOrFail(raw) {
		return ""
	}
	// Strip a leading tool-call JSON object if leftover speech follows.
	if stripped, ok := stripLeadingJSONObject(raw); ok {
		stripped = strings.TrimSpace(stripped)
		if stripped == "" || isHarnessOrFail(stripped) {
			return ""
		}
		raw = stripped
	}
	raw = strings.TrimSpace(stripHelpdeskClosers(raw))
	if raw == "" || isHarnessOrFail(raw) {
		return ""
	}
	return collapseWS(raw)
}

// SpeechPreview returns honesty-stripped teammate/human speech suitable for
// a Slack-style reply chip. Harness JSON, TOOL_FAIL, DELEGATE_FAIL, and
// "Delegated to @Sam" announcements become empty so they never label a chip.
func SpeechPreview(content string) string {
	return truncateRunes(ReplySpeech(content), maxLastPreviewRunes)
}

// LastSpeechPreview walks replies newest-first and returns the first
// honesty-stripped speech. Empty if every reply is harness/fail.
func LastSpeechPreview(replies []SpaceMessage) string {
	for i := len(replies) - 1; i >= 0; i-- {
		if p := SpeechPreview(replies[i].Content); p != "" {
			return p
		}
	}
	return ""
}

// threadSpeaker is the name shown in a wake transcript.
func threadSpeaker(m SpaceMessage) string {
	if name := strings.TrimSpace(m.Agent); name != "" {
		return name
	}
	if m.Role == "user" || m.Role == "" {
		return "user"
	}
	return m.Role
}

type threadLine struct {
	speaker string
	speech  string
}

func collectThreadLines(parent *SpaceMessage, replies []SpaceMessage) []threadLine {
	var lines []threadLine
	hasParent := false
	if parent != nil {
		if speech := ReplySpeech(parent.Content); speech != "" {
			lines = append(lines, threadLine{speaker: threadSpeaker(*parent), speech: speech})
			hasParent = true
		}
	}
	for _, r := range replies {
		if speech := ReplySpeech(r.Content); speech != "" {
			lines = append(lines, threadLine{speaker: threadSpeaker(r), speech: speech})
		}
	}
	if hasParent && len(lines) > 1+maxThreadTranscriptReplies {
		// Keep the parent bubble + the newest N replies.
		kept := make([]threadLine, 0, 1+maxThreadTranscriptReplies)
		kept = append(kept, lines[0])
		kept = append(kept, lines[len(lines)-maxThreadTranscriptReplies:]...)
		return kept
	}
	if !hasParent && len(lines) > maxThreadTranscriptReplies {
		return lines[len(lines)-maxThreadTranscriptReplies:]
	}
	return lines
}

// BuildThreadTranscript is a short Slack-thread script for an in-thread @ wake:
// speaker + honesty-stripped speech, oldest first. Harness / TOOL_FAIL / tool
// traces are omitted. Capped so a 14b prompt does not drown.
func BuildThreadTranscript(parent *SpaceMessage, replies []SpaceMessage) string {
	lines := collectThreadLines(parent, replies)
	if len(lines) == 0 {
		return ""
	}
	keepParent := parent != nil && ReplySpeech(parent.Content) != ""
	for {
		var b strings.Builder
		b.WriteString("[Thread]")
		n := utf8.RuneCountInString("[Thread]")
		ok := true
		for _, line := range lines {
			speech := truncateRunes(line.speech, maxThreadLineRunes)
			row := "\n" + line.speaker + ": " + speech
			n += utf8.RuneCountInString(row)
			if n > maxThreadTranscriptRunes {
				ok = false
				break
			}
			b.WriteString(row)
		}
		if ok || len(lines) <= 1 {
			if !ok && len(lines) == 1 {
				// Single oversized line: still emit a truncated parent/reply.
				speech := truncateRunes(lines[0].speech, maxThreadLineRunes)
				return "[Thread]\n" + lines[0].speaker + ": " + speech
			}
			return b.String()
		}
		// Drop the oldest non-parent line and retry.
		if keepParent && len(lines) > 1 {
			lines = append(lines[:1], lines[2:]...)
			continue
		}
		lines = lines[1:]
	}
}

// BuildThreadWakePrompt is the user turn for an in-thread @ wake: prior
// thread (parent + earlier replies) then the @mention. The current mention
// is not duplicated inside the transcript.
func BuildThreadWakePrompt(parent *SpaceMessage, replies []SpaceMessage, mention string) string {
	mention = strings.TrimSpace(mention)
	trimmed := replies
	for len(trimmed) > 0 && strings.TrimSpace(trimmed[len(trimmed)-1].Content) == mention {
		trimmed = trimmed[:len(trimmed)-1]
	}
	transcript := BuildThreadTranscript(parent, trimmed)
	if transcript == "" {
		return mention
	}
	if mention == "" {
		return transcript
	}
	return transcript + "\n\n" + mention
}

func isHarnessOrFail(raw string) bool {
	upper := strings.ToUpper(raw)
	if strings.HasPrefix(upper, "TOOL_FAIL") || strings.HasPrefix(upper, "DELEGATE_FAIL") {
		return true
	}
	if strings.HasPrefix(raw, "Delegated to @") {
		return true
	}
	if strings.HasPrefix(raw, "Delegation to @") {
		return true
	}
	if strings.Contains(raw, "completed delegated work:") {
		return true
	}
	if strings.Contains(raw, " needs input") && strings.HasPrefix(raw, "@") {
		return true
	}
	for _, name := range harnessToolNames {
		if raw == name || strings.HasPrefix(raw, name+" ") {
			return true
		}
		if strings.Contains(raw, `"`+name+`"`) && looksLikeToolJSON(raw) {
			return true
		}
	}
	return false
}

func looksLikeToolJSON(raw string) bool {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "{") {
		return false
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(s), &obj); err != nil {
		// Partial / leftover JSON still counts if it names a harness tool.
		return strings.Contains(s, `"name"`)
	}
	name, _ := obj["name"].(string)
	if name == "" {
		return false
	}
	for _, h := range harnessToolNames {
		if name == h {
			return true
		}
	}
	return false
}

func stripLeadingJSONObject(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "{") {
		return raw, false
	}
	depth := 0
	inStr := false
	esc := false
	for i, r := range s {
		if inStr {
			if esc {
				esc = false
				continue
			}
			if r == '\\' {
				esc = true
				continue
			}
			if r == '"' {
				inStr = false
			}
			continue
		}
		switch r {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				rest := strings.TrimSpace(s[i+1:])
				return rest, true
			}
		}
	}
	return raw, false
}

// Live hallway closer: "The result of 7 * 8 is 56. If you have any other questions, feel free to ask!"
// Also "need further assistance" / "Is there anything else you need assistance with?"
var helpdeskCloserSentenceRE = regexp.MustCompile(`(?i)^(?:how can i (?:assist|help)(?: you(?: further)?)?[?.!]*|is there anything(?: else)?(?: i can (?:help|assist)(?: you)?(?: with)?| you need(?: (?:help|assistance)(?: with)?)?)?[?.!]*|if you have any (?:other )?questions(?: or need further assistance)?,\s*feel free to ask[?.!]*|feel free to ask(?: if you have any (?:other )?questions(?: or need further assistance)?)?[?.!]*)$`)

// 14b playbook parroting after a real Steve handoff.
var (
	waitPlaybookNameRE        = regexp.MustCompile(`(?i)(?:please\s+)?(?:use|call)\s+\x60?wait_for_threads`)
	sessionHistoryLeakRE      = regexp.MustCompile(`(?i)session history could not be loaded`)
	spawnedPlaybookSentenceRE = regexp.MustCompile(`(?i)(?:has been spawned|was spawned|spawned immediately|delegate(?:d)? task\b.{0,80}\bspawned\b)`)
)

func isPlaybookInstructionSentence(sent string) bool {
	return waitPlaybookNameRE.MatchString(sent) ||
		sessionHistoryLeakRE.MatchString(sent) ||
		spawnedPlaybookSentenceRE.MatchString(sent)
}

func stripHelpdeskClosers(s string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	var kept []string
	var b strings.Builder
	flush := func() {
		sent := strings.Join(strings.Fields(b.String()), " ")
		b.Reset()
		if sent == "" || helpdeskCloserSentenceRE.MatchString(sent) || isPlaybookInstructionSentence(sent) {
			return
		}
		kept = append(kept, sent)
	}
	for i := 0; i < len(s); i++ {
		b.WriteByte(s[i])
		if s[i] == '.' || s[i] == '!' || s[i] == '?' {
			flush()
		}
	}
	if rest := strings.Join(strings.Fields(b.String()), " "); rest != "" {
		if !helpdeskCloserSentenceRE.MatchString(rest) && !isPlaybookInstructionSentence(rest) {
			kept = append(kept, rest)
		}
	}
	return strings.Join(kept, " ")
}

func collapseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}
