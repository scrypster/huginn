package backend

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Residual speech is what a small local model leaves in the assistant channel
// after its tool invocations have already been lifted out and executed: wait
// placeholders ("<wait for Reggie to finish>"), playbook glue ("Once Reggie
// has finished:"), a re-typed tool object for a tool that already ran (or one
// that never existed), and a result-shaped object glued onto the answer
// ("7 times 8 is 56.{"pong_response":"PONG"}"). None of that is teammate
// speech. These helpers remove it from the visible channel without executing
// anything; fenced code that is not a tool invocation is left untouched.

var (
	// <wait for Reggie to finish>, <wait_for_threads>, <wait: reggie>, <waiting …>
	waitTagRE = regexp.MustCompile(`(?i)<\s*wait(?:ing)?(?:[\s_:-][^<>]*)?\s*>`)
	// lone "wait-for-reggie" / "wait_for_threads" / "WAIT FOR REGGIE" tokens
	waitTokenLineRE = regexp.MustCompile(`(?i)^\s*(?:\[|\()?\s*wait(?:ing)?[-_ ]for[-_ ][\w@.-]+(?:[-_ ]to[-_ ]finish)?\s*(?:\]|\))?\s*[:.]?\s*$`)
	// "Once Reggie has finished:", "After the thread is done,", "When Reggie replies:"
	glueLineRE = regexp.MustCompile(`(?i)^\s*(?:once|after|when|as soon as)\s+[^.:,]{1,80}?\s+(?:has|have|is|are)?\s*(?:finished|done|complete|completed|replied|responded|answered|returned)\s*[:,]?\s*$`)
	// "After Reggie responds with PONG:", "When Reggie replies:", "Once Reggie
	// comes back with the result:", "After Reggie responds," — a temporal
	// conjunction, a wait verb, and a colon/comma ending with no sentence in
	// between. "Once the migration has finished, the table …" has prose
	// after the comma and stays.
	waitGlueLineRE = regexp.MustCompile(`(?i)^\s*(?:once|after|when|as soon as)\s+[^.:]{0,60}?\b(?:responds?|replies|replied|replying|finish(?:es|ed)?|complet(?:es|ed)?|returns?|returned|answers?|answered|reports?|reported|comes? back|gets? back|is (?:done|back|finished|ready))\b[^.:]{0,60}?\s*[:,]\s*$`)
	// A JSON string glued onto sentence-final punctuation: 56."PONG"
	gluedStringRE = regexp.MustCompile(`([.!?])"[^"\n]{1,60}"\s*$`)
	// A separator line glued onto sentence-final punctuation: 56.---
	gluedSeparatorRE = regexp.MustCompile(`([.!?])\s*-{3,}\s*$`)
	// Runs of digits or capitals — the fragments an echo line is made of.
	echoFragmentRE = regexp.MustCompile(`[0-9]+|[A-Z]+`)
	// An echo line: no spaces, no lowercase, only fragments and punctuation.
	echoLineRE = regexp.MustCompile(`^["'` + "`" + `(\[]*(?:[0-9]+|[A-Z]+)(?:["'` + "`" + `.,;:!?)\]]*(?:[0-9]+|[A-Z]+))*["'` + "`" + `.,;:!?)\]]*$`)
	// "Then calculate:" — only stripped when it directly continues a glue chain
	glueContinuationRE = regexp.MustCompile(`(?i)^\s*(?:then|next|finally|afterwards|after that)\b[^.]{0,80}:\s*$`)
	// Generic LLM filler phrases that appear after tools have run
	// Matches lines like "How can I assist you further?", "Not currently delegating any tasks.", etc.
	fillerLineRE = regexp.MustCompile(`(?i)^(?:how can I|is there anything|how else|not currently delegating|nothing is currently delegated)\b.*[?.]?\s*$`)
	// Parenthetical stage directions: lines that are ONLY (text)
	stageDirectionLineRE = regexp.MustCompile(`^\s*\([^)]*\)\s*$`)
	// Leading glued stage paren before prose: "(7 times 8 is 56)Reggie said"
	leadingStageParenRE = regexp.MustCompile(`^\s*\([^)]{1,80}\)\s*`)
	// Leading glued bracket stage before prose:
	// "[Delegation initiated. Waiting…]Reggie replied"
	leadingBracketStageRE = regexp.MustCompile(`^\s*\[[^\]]{1,120}\]\s*`)
	// Orphan fence ticks left after unlabeled playbook fences unwrap: "``Reggie said"
	// Bracket stage directions: lines that are ONLY [text]
	bracketStageDirectionLineRE = regexp.MustCompile(`^\s*\[[^\]]*\]\s*$`)
	// Playbook format instruction lines: "use the following format:" or similar
	playbookFormatLineRE = regexp.MustCompile(`(?i)^\s*use\s+(?:the\s+)?(?:following\s+)?format\s*:\s*$`)
	// Template placeholder lines: "Reggie says: <reggie-reply>" style
	templatePlaceholderLineRE = regexp.MustCompile(`^[A-Za-z][^:]*:\s*<[^>]+>\s*$`)
	// Standalone separator lines
	separatorLineRE = regexp.MustCompile(`^\s*---+\s*$`)
	// Playbook introductions: "After ... response, use the following format:"
	playbookIntroLineRE = regexp.MustCompile(`(?i)(?:after|once|when)\s+.*\b(?:response|result|reply)\b.*,\s*use\s+(?:the\s+)?(?:following\s+)?format\s*:\s*$`)
	// Future-tense wait glue spoken as a whole sentence after tools already ran:
	// "Once Reggie has replied with PONG, I will let you know …"
	futureWaitGlueSentenceRE = regexp.MustCompile(`(?i)^(?:once|after|when|as soon as|to complete)\b.{0,160}?(?:replied|responds?|responded|finished|done|complete|completed|await|waiting)\b.{0,160}?\b(?:will|i'll|i will|proceed|multiply)\b`)
)

// StripResidualSpeech removes wait tags and playbook glue lines from
// assistant content. It never touches JSON, so a model that legitimately
// quotes a tool object mid-sentence stays intact. Fenced blocks are
// preserved verbatim. Nothing is executed.
func StripResidualSpeech(content string) string {
	return stripResidual(content, false)
}

// StripResidualSpeechAfterTools is StripResidualSpeech plus removal of
// unfenced JSON that is not speech once tools have already run this turn:
// tool-invocation objects (name / function_name [+ arguments]) for any
// name, granted or invented — they are dropped, never executed — and
// result-shaped objects (a flat object of scalar values with no tool name)
// when they sit next to real prose. A message that is only a result object
// is left alone so the user still sees something.
func StripResidualSpeechAfterTools(content string) string {
	return stripResidual(content, true)
}

func stripResidual(content string, afterTools bool) string {
	if content == "" {
		return content
	}
	parts := strings.Split(content, "```")
	changed := false
	// Track which fences are unlabeled (should have markers removed)
	unlabeledFences := make(map[int]bool)
	for i, part := range parts {
		isFenced := i%2 == 1
		out := stripResidualUnfenced(part, afterTools, isFenced)
		if out != part {
			parts[i] = out
			changed = true
		}
		// Track unlabeled fences so we know not to add markers back
		if isFenced && !fenceHasLanguageTag(part) {
			unlabeledFences[i] = true
		}
		// Drop fenced blocks that became empty/whitespace-only when afterTools=true,
		// since they were only residual speech (wait tags, glue lines, etc.).
		if afterTools && isFenced && strings.TrimSpace(out) == "" {
			changed = true
			// Mark for removal by setting to empty; rebuild will filter these.
			parts[i] = ""
		}
	}
	if !changed {
		return content
	}
	// Rebuild, skipping empty fences and removing markers from unlabeled fences.
	var sb strings.Builder
	for i, part := range parts {
		if i%2 == 0 {
			sb.WriteString(part)
		} else if part != "" {
			// Add fence markers only for labeled fences; unlabeled fences are unwrapped.
			if !unlabeledFences[i] {
				sb.WriteString("```")
				sb.WriteString(part)
				sb.WriteString("```")
			} else {
				sb.WriteString(part)
			}
		}
	}
	return strings.TrimSpace(sb.String())
}

func isResidualLine(trim string) bool {
	if trim == "" {
		return true
	}
	// Comment line
	if strings.HasPrefix(trim, "//") || strings.HasPrefix(trim, "#") {
		return true
	}
	// Wait/glue/filler/stage direction patterns
	if waitTokenLineRE.MatchString(trim) || glueLineRE.MatchString(trim) ||
		waitGlueLineRE.MatchString(trim) || fillerLineRE.MatchString(trim) ||
		stageDirectionLineRE.MatchString(trim) || bracketStageDirectionLineRE.MatchString(trim) ||
		playbookFormatLineRE.MatchString(trim) || playbookIntroLineRE.MatchString(trim) ||
		templatePlaceholderLineRE.MatchString(trim) || separatorLineRE.MatchString(trim) {
		return true
	}
	// Tool-shaped JSON (name or function_name)
	if strings.HasPrefix(trim, "{") && (strings.Contains(trim, "\"name\"") || strings.Contains(trim, "\"function_name\"")) {
		return true
	}
	return false
}

func stripResidualUnfenced(s string, afterTools bool, isFenced bool) string {
	if s == "" {
		return s
	}
	// Language-tagged fences (```json, ```go, etc.) are always kept unchanged.
	if isFenced && fenceHasLanguageTag(s) {
		return s
	}
	// Only remove JSON from unfenced content; fenced content is code/samples.
	if afterTools && !isFenced {
		s = removeResidualJSONObjects(s)
	}

	lines := strings.Split(s, "\n")
	kept := make([]string, 0, len(lines))
	inGlueChain := false
	for _, line := range lines {
		hadText := strings.TrimSpace(line) != ""
		line = waitTagRE.ReplaceAllString(line, "")
		trim := strings.TrimSpace(line)
		switch {
		case trim == "" && hadText:
			// the line was only a wait tag
			inGlueChain = true
			continue
		case trim == "":
			kept = append(kept, line)
			continue
		case waitTokenLineRE.MatchString(trim), glueLineRE.MatchString(trim), waitGlueLineRE.MatchString(trim), fillerLineRE.MatchString(trim):
			inGlueChain = true
			continue
		case playbookFormatLineRE.MatchString(trim), playbookIntroLineRE.MatchString(trim), templatePlaceholderLineRE.MatchString(trim), separatorLineRE.MatchString(trim):
			inGlueChain = true
			continue
		case inGlueChain && glueContinuationRE.MatchString(trim):
			continue
		case stageDirectionLineRE.MatchString(trim), bracketStageDirectionLineRE.MatchString(trim):
			// Strip parenthetical and bracket stage directions when afterTools
			if afterTools {
				continue
			}
		case isFenced && !fenceHasLanguageTag(s):
			// For unlabeled fences, also strip comment lines and tool-shaped JSON
			if strings.HasPrefix(trim, "//") || strings.HasPrefix(trim, "#") {
				continue
			}
			if strings.HasPrefix(trim, "{") && (strings.Contains(trim, "\"name\"") || strings.Contains(trim, "\"function_name\"")) {
				continue
			}
		}
		inGlueChain = false
		if afterTools {
			line = gluedStringRE.ReplaceAllString(line, "$1")
			line = gluedSeparatorRE.ReplaceAllString(line, "$1")
			line = leadingStageParenRE.ReplaceAllString(line, "")
			line = leadingBracketStageRE.ReplaceAllString(line, "")
			line = stripOrphanFenceTicks(line)
			line = dropFutureWaitGlueSentences(line)
			if strings.TrimSpace(line) == "" {
				continue
			}
		}
		kept = append(kept, line)
	}
	if afterTools && !isFenced {
		kept = dropTrailingEchoLines(kept, s)
		kept = stripSameLineEchoFragments(kept, s)
		return collapseBlankRuns(deduplicateTeammateSentences(strings.Join(kept, "\n")))
	}
	return collapseBlankRuns(strings.Join(kept, "\n"))
}

// dropFutureWaitGlueSentences removes sentences that narrate waiting in the
// future tense after tools already ran ("Once Reggie has replied … I will …").
func dropFutureWaitGlueSentences(line string) string {
	trim := strings.TrimSpace(line)
	if trim == "" {
		return line
	}
	var kept []string
	for _, sent := range splitSentences(trim) {
		if futureWaitGlueSentenceRE.MatchString(sent) {
			continue
		}
		kept = append(kept, sent)
	}
	if len(kept) == 0 {
		return ""
	}
	indent := len(line) - len(strings.TrimLeft(line, " \t"))
	return line[:indent] + strings.Join(kept, " ")
}

// stripOrphanFenceTicks removes leftover backtick runs at the start/end of a
// line after unlabeled playbook fences unwrap ("“Reggie said").
func stripOrphanFenceTicks(line string) string {
	trimLeft := strings.TrimLeft(line, " \t")
	n := 0
	for n < len(trimLeft) && trimLeft[n] == '`' {
		n++
	}
	if n > 0 && n < 4 {
		indent := len(line) - len(trimLeft)
		line = line[:indent] + strings.TrimLeft(trimLeft[n:], " \t")
	}
	trimRight := strings.TrimRight(line, " \t")
	n = 0
	for n < len(trimRight) && trimRight[len(trimRight)-1-n] == '`' {
		n++
	}
	if n > 0 && n < 4 {
		line = strings.TrimRight(trimRight[:len(trimRight)-n], " \t")
	}
	return line
}

// stripSameLineEchoFragments removes echo fragments glued to the end of the last
// line when they appeared earlier in the segment (e.g., "7 times 8 is 56.PONG, 56"
// -> "7 times 8 is 56." when PONG and 56 appeared earlier). Fragments are
// recognized as digit/capital runs, and only stripped when they sit after
// sentence-final punctuation.
func stripSameLineEchoFragments(kept []string, original string) []string {
	if len(kept) == 0 {
		return kept
	}
	// Find the last non-blank line.
	lastIdx := len(kept) - 1
	for lastIdx >= 0 && strings.TrimSpace(kept[lastIdx]) == "" {
		lastIdx--
	}
	if lastIdx < 0 {
		return kept
	}
	line := kept[lastIdx]
	trim := strings.TrimSpace(line)

	// Look for sentence-final punctuation followed by fragments that appeared earlier.
	matched := false
	for {
		idx := findEndOfSentence(trim)
		if idx <= 0 || idx >= len(trim) {
			break
		}
		remainder := trim[idx:]
		// Collect all digit/capital fragments from remainder.
		frags := echoFragmentRE.FindAllString(remainder, -1)
		if len(frags) == 0 {
			break
		}
		// Check if all fragments appeared in the segment before the sentence end.
		sentenceEnd := trim[:idx]
		foundAt := strings.Index(original, sentenceEnd)
		beforeAnswer := original
		if foundAt >= 0 {
			beforeAnswer = original[:foundAt+len(sentenceEnd)]
		}
		allEarlier := true
		for _, f := range frags {
			if !strings.Contains(beforeAnswer, f) {
				allEarlier = false
				break
			}
		}
		if !allEarlier {
			break
		}
		// Strip the remainder.
		trim = sentenceEnd
		matched = true
	}
	if !matched {
		return kept
	}
	// Reconstruct the line with leading whitespace preserved.
	indent := len(line) - len(strings.TrimLeft(line, " \t"))
	kept[lastIdx] = line[:indent] + trim
	return kept
}

// deduplicateTeammateSentences keeps the first copy of each sentence after
// playbook lines have been dropped. 14b often emits the real answer, then
// glues the same sentences onto the last line again. Paragraph breaks stay.
func deduplicateTeammateSentences(s string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	leadNL := strings.HasPrefix(s, "\n")
	trailNL := strings.HasSuffix(s, "\n")
	seen := make(map[string]struct{})
	var out []string
	for _, line := range strings.Split(s, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" {
			out = append(out, "")
			continue
		}
		kept := make([]string, 0, 4)
		for _, sent := range splitSentences(trim) {
			key := strings.ToLower(sent)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			kept = append(kept, sent)
		}
		if len(kept) == 0 {
			continue
		}
		out = append(out, strings.Join(kept, " "))
	}
	outS := collapseBlankRuns(strings.Join(out, "\n"))
	if leadNL && !strings.HasPrefix(outS, "\n") {
		outS = "\n" + outS
	}
	if trailNL && !strings.HasSuffix(outS, "\n") {
		outS = outS + "\n"
	}
	return outS
}

func splitSentences(s string) []string {
	var sentences []string
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		b.WriteByte(s[i])
		if s[i] == '.' || s[i] == '!' || s[i] == '?' {
			sent := strings.Join(strings.Fields(b.String()), " ")
			if sent != "" {
				sentences = append(sentences, sent)
			}
			b.Reset()
		}
	}
	if rest := strings.Join(strings.Fields(b.String()), " "); rest != "" {
		sentences = append(sentences, rest)
	}
	return sentences
}

func findEndOfSentence(s string) int {
	idx := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '.' || s[i] == '!' || s[i] == '?' {
			idx = i + 1
		}
	}
	return idx
}

// dropTrailingEchoLines removes trailing lines such as `56PONG` / `56` /
// `"PONG"` — result fragments a small model re-emits after its answer. A
// line only counts as an echo when it has no spaces or lowercase and every
// digit/capital run in it already appeared earlier in the original segment
// (including in glue or JSON that was stripped). A fresh number on its own
// line is an answer and stays; a message that would become empty is left.
func dropTrailingEchoLines(kept []string, original string) []string {
	end := len(kept)
	removed := false
	for end > 0 {
		trim := strings.TrimSpace(kept[end-1])
		if trim == "" {
			end--
			continue
		}
		if !removed {
			// Nothing dropped yet: keep the segment's trailing whitespace
			// (it is the newline before an adjacent code fence).
			end = len(kept)
		}
		if !echoLineRE.MatchString(trim) {
			break
		}
		idx := strings.LastIndex(original, trim)
		if idx < 0 {
			break
		}
		before := original[:idx]
		echo := true
		for _, frag := range echoFragmentRE.FindAllString(trim, -1) {
			if !strings.Contains(before, frag) {
				echo = false
				break
			}
		}
		if !echo {
			break
		}
		// Drop this line and any blank lines already skipped above it.
		for end > 0 && strings.TrimSpace(kept[end-1]) != trim {
			end--
		}
		end--
		removed = true
	}
	if !removed {
		return kept
	}
	// Never blank the whole message.
	for i := 0; i < end; i++ {
		if strings.TrimSpace(kept[i]) != "" {
			return kept[:end]
		}
	}
	return kept
}

// removeResidualJSONObjects drops unfenced JSON objects that are tool
// invocations (name / function_name [+ arguments]) and flat result-shaped
// objects — the latter only if prose remains around them.
func removeResidualJSONObjects(s string) string {
	var b strings.Builder
	prose := strings.Builder{}
	type span struct{ start, end int }
	var results []span
	i := 0
	for i < len(s) {
		if s[i] != '{' {
			b.WriteByte(s[i])
			prose.WriteByte(s[i])
			i++
			continue
		}
		rest := s[i:]
		obj, after, ok := readJSONObjectLenient(rest)
		if !ok {
			b.WriteByte(s[i])
			prose.WriteByte(s[i])
			i++
			continue
		}
		consumed := len(rest) - len(after)
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(obj), &raw); err != nil {
			b.WriteString(s[i : i+consumed])
			prose.WriteString(s[i : i+consumed])
			i += consumed
			continue
		}
		if _, isCall := toolCallFromRaw(raw); isCall {
			i += consumed
			i = skipOneSeparator(s, i)
			continue
		}
		if isResultShapedJSON(raw) {
			start := b.Len()
			b.WriteString(s[i : i+consumed])
			results = append(results, span{start, b.Len()})
			i += consumed
			continue
		}
		b.WriteString(s[i : i+consumed])
		prose.WriteString(s[i : i+consumed])
		i += consumed
	}
	out := b.String()
	if len(results) == 0 || strings.TrimSpace(prose.String()) == "" {
		return out
	}
	// Cut result-shaped spans back-to-front so earlier offsets stay valid.
	for k := len(results) - 1; k >= 0; k-- {
		sp := results[k]
		end := skipOneSeparator(out, sp.end)
		out = out[:sp.start] + out[end:]
	}
	return out
}

// isResultShapedJSON reports whether raw is a flat object whose values are
// all scalars — the shape of an echoed tool result. Objects carrying a tool
// name are handled by toolCallFromRaw; nested objects/arrays are not
// stripped because they may be a deliberate sample.
func isResultShapedJSON(raw map[string]json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	if _, ok := raw["name"]; ok {
		return false
	}
	if _, ok := raw["function_name"]; ok {
		return false
	}
	for _, v := range raw {
		t := strings.TrimSpace(string(v))
		if t == "" || t[0] == '{' || t[0] == '[' {
			return false
		}
	}
	return true
}

func skipOneSeparator(s string, i int) int {
	if i < len(s) && (s[i] == '\n' || s[i] == ' ' || s[i] == '\t' || s[i] == '\r') {
		if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
			return i + 2
		}
		return i + 1
	}
	return i
}

// collapseBlankRuns squeezes three or more consecutive newlines (left behind
// by removed lines) down to a paragraph break.
func collapseBlankRuns(s string) string {
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return s
}

// fenceHasLanguageTag checks if a fence content starts with a language tag.
// Language tags are non-empty first lines that are single words (no spaces).
// Examples: json, go, python, ts, bash, html, xml, etc.
func fenceHasLanguageTag(s string) bool {
	lines := strings.Split(s, "\n")
	if len(lines) == 0 {
		return false
	}
	tag := strings.TrimSpace(lines[0])
	return tag != "" && !strings.ContainsAny(tag, " \t")
}

// readJSONObjectLenient is readJSONObject that also accepts an object whose
// only defect is // line comments outside strings ("thread-12345" // Replace
// with the actual thread ID) or <placeholder> tokens that stand in for values.
// Used only for stripping: promotion keeps the strict parser so a commented
// placeholder never executes.
func readJSONObjectLenient(s string) (obj, after string, ok bool) {
	if obj, after, ok = readJSONObject(s); ok {
		return obj, after, true
	}
	end, found := jsonObjectEnd(s)
	if !found {
		return "", "", false
	}
	raw := stripJSONLineComments(s[:end])
	if !json.Valid([]byte(raw)) {
		// Try replacing <placeholders> with quoted strings so the JSON is valid.
		raw = replacePlaceholders(raw)
		if !json.Valid([]byte(raw)) {
			return "", "", false
		}
	}
	trim := strings.TrimSpace(raw)
	if trim == "" || trim[0] != '{' {
		return "", "", false
	}
	return trim, s[end:], true
}

// replacePlaceholders replaces <...> placeholders with quoted strings
// to make JSON with placeholders valid for parsing.
func replacePlaceholders(s string) string {
	return regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, `"<placeholder>"`)
}

// stripJSONLineComments removes // comments that sit outside string literals.
func stripJSONLineComments(s string) string {
	var b strings.Builder
	inStr, escape := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			b.WriteByte(c)
			if escape {
				escape = false
			} else if c == '\\' {
				escape = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			b.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '/' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			if i < len(s) {
				b.WriteByte('\n')
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
