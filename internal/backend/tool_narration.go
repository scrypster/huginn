package backend

import (
	"regexp"
	"strings"
)

// Tool-plan narration is a small local model announcing its own tool use
// as if that were teammate speech: naming the literal tool ("I'll use the
// muninn_recall function"), or narrating the plan in prose ("let me check
// the memory", "I need to consult our internal memory", "searching my
// memory for..."). None of that belongs in the speech channel — the tool
// call itself (or its result) is what the user should see, never the
// harness plan for making it. This mirrors the residual_speech.go family:
// sentence-level stripping that keeps the real answer sentences intact.

var (
	// toolNameMentionRE matches a literal harness/MCP tool identifier named
	// in narration: the muninn_* family (muninn_recall, muninn_remember,
	// muninn_evolve, ...) and the named harness tools a small model can see
	// in its toolbelt (delegate_to_agent, wait_for_threads, create_agent,
	// recall_thread_result, list_team_status, consult_agent, and the local
	// tool names). The muninn_ prefix is matched generically so new muninn
	// tools never need a hardcoded addition here.
	toolNameMentionRE = regexp.MustCompile(`(?i)\b(?:muninn_[a-z][a-z0-9_]*|delegate_to_agent|wait_for_threads|create_agent|recall_thread_result|list_team_status|consult_agent|echo_tool|search_docs|generate_image|git_commit|git_status|github_list_prs|slack_post|slack_read|read_file|write_file|edit_file|list_dir)\b`)
	// toolPlanNarrationRE matches prose narrating a tool-use plan, whether
	// or not it names a literal tool: "I'll use the X function/tool",
	// "let me call/check/use/search X", "I need to consult (our|the)
	// (internal) memory", "searching my/our memory for ...".
	toolPlanNarrationRE = regexp.MustCompile(`(?i)^(?:` +
		// "I'll use/call/invoke/check the X function/tool ..."
		`(?:i'?ll|i will|i'?m going to|i am going to)\s+(?:use|call|invoke|check|search|query)\b[^.!?]*\b(?:function|tool)\b|` +
		// "Let me use/call/check/search/query the X function/tool/memory ..."
		// Requires an object naming what is being planned (function, tool,
		// or memory) so ordinary work narration ("Let me check... here are
		// the files.") is left alone.
		`let me\s+(?:use|call|invoke|check|search|query)\b[^.!?]*\b(?:function|tool|memory)\b|` +
		// "I need to consult (our|the) (internal) memory"
		`i need to consult\s+(?:our|the)(?:\s+internal)?\s+memory\b|` +
		// "Searching my/our memory for ..."
		`searching\s+(?:my|our)\s+memory\s+for\b|` +
		// "To answer your/the question, I need to ..." plan preamble
		`to answer (?:your|the) question,?\s+i\s+(?:need|will|'ll|have)\b` +
		`)`)
	// jsonFragmentLineRE matches a raw JSON object line ({, }, [, ]) or a
	// "key": value line. A tool-name identifier inside JSON structure (e.g.
	// "name": "recall_thread_result") is a payload, not narration prose —
	// leave it for removeResidualJSONObjects / stripEmbeddedHarnessToolJSON
	// to handle so a still-forming object is never torn apart mid-field.
	jsonFragmentLineRE = regexp.MustCompile(`^[{}\[\]]|^"[^"]*"\s*:`)
)

// isToolPlanNarrationSentence reports whether sent is tool-plan narration
// rather than teammate speech: it names a literal tool identifier, or it
// matches a narration phrasing pattern.
func isToolPlanNarrationSentence(sent string) bool {
	return toolNameMentionRE.MatchString(sent) || toolPlanNarrationRE.MatchString(sent) ||
		waitProseSentenceRE.MatchString(sent)
}

// waitProseSentenceRE matches prose-form wait glue spoken as a sentence:
// "Waiting for the recall task to complete." (live repro 2026-08-28, thread
// drawer). The multi-word target requires an explicit "to <finish/...>" tail
// so sentences that merely start with "Waiting for" (e.g. "Waiting for your
// sign-off before I proceed…") are not eaten.
var waitProseSentenceRE = regexp.MustCompile(`(?i)^\s*wait(?:ing)?\s+for\s+(?:[\w@.'-]+\s+){1,6}?to\s+(?:finish|complete|respond|reply|return|come\s+back)\w*\s*[.…]*\s*$`)

// dropToolPlanNarrationSentences removes tool-plan narration sentences from
// line, keeping any real answer sentences that remain. Mirrors
// dropFutureWaitGlueSentences / dropPlaybookInstructionSentences: split on
// sentence boundaries, filter, rejoin with the original indent.
func dropToolPlanNarrationSentences(line string) string {
	trim := strings.TrimSpace(line)
	if trim == "" || jsonFragmentLineRE.MatchString(trim) {
		return line
	}
	var kept []string
	dropped := false
	for _, sent := range splitSentences(trim) {
		if isToolPlanNarrationSentence(sent) {
			dropped = true
			continue
		}
		kept = append(kept, sent)
	}
	if !dropped {
		// Nothing matched — return the line exactly as given so unrelated
		// whitespace/glue (e.g. "56.\"PONG\"") is never disturbed by a
		// sentence-split/rejoin that had nothing to remove.
		return line
	}
	if len(kept) == 0 {
		return ""
	}
	indent := len(line) - len(strings.TrimLeft(line, " \t"))
	return line[:indent] + strings.Join(kept, " ")
}
