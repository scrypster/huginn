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
	// Runs of digits or capitals — the fragments an echo line is made of.
	echoFragmentRE = regexp.MustCompile(`[0-9]+|[A-Z]+`)
	// An echo line: no spaces, no lowercase, only fragments and punctuation.
	echoLineRE = regexp.MustCompile(`^["'` + "`" + `(\[]*(?:[0-9]+|[A-Z]+)(?:["'` + "`" + `.,;:!?)\]]*(?:[0-9]+|[A-Z]+))*["'` + "`" + `.,;:!?)\]]*$`)
	// "Then calculate:" — only stripped when it directly continues a glue chain
	glueContinuationRE = regexp.MustCompile(`(?i)^\s*(?:then|next|finally|afterwards|after that)\b[^.]{0,80}:\s*$`)
	// Generic LLM filler phrases that appear after tools have run
	// Matches lines like "How can I assist you further?", "Not currently delegating any tasks.", etc.
	fillerLineRE = regexp.MustCompile(`(?i)^(?:how can I|is there anything|how else|not currently delegating|nothing is currently delegated)\b.*[?.]?\s*$`)
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
	for i, part := range parts {
		if i%2 == 1 {
			continue // inside a fence
		}
		out := stripResidualUnfenced(part, afterTools)
		if out != part {
			parts[i] = out
			changed = true
		}
	}
	if !changed {
		return content
	}
	return strings.TrimSpace(strings.Join(parts, "```"))
}

func stripResidualUnfenced(s string, afterTools bool) string {
	if s == "" {
		return s
	}
	if afterTools {
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
		case inGlueChain && glueContinuationRE.MatchString(trim):
			continue
		}
		inGlueChain = false
		if afterTools {
			line = gluedStringRE.ReplaceAllString(line, "$1")
		}
		kept = append(kept, line)
	}
	if afterTools {
		kept = dropTrailingEchoLines(kept, s)
	}
	return collapseBlankRuns(strings.Join(kept, "\n"))
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

// readJSONObjectLenient is readJSONObject that also accepts an object whose
// only defect is // line comments outside strings ("thread-12345" // Replace
// with the actual thread ID). Used only for stripping: promotion keeps the
// strict parser so a commented placeholder never executes.
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
		return "", "", false
	}
	trim := strings.TrimSpace(raw)
	if trim == "" || trim[0] != '{' {
		return "", "", false
	}
	return trim, s[end:], true
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
