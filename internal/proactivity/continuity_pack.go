package proactivity

import (
	"strings"
)

// ContinuityMode controls how much context is injected.
type ContinuityMode string

const (
	// ContinuityModeConversational includes broad orientation signals.
	ContinuityModeConversational ContinuityMode = "conversational"
	// ContinuityModeDeterministic limits context to task-scoped snippets.
	ContinuityModeDeterministic ContinuityMode = "deterministic"
)

// ContinuityPackInput is the raw data used to build a continuity block.
type ContinuityPackInput struct {
	Mode               ContinuityMode
	UserMessage        string
	WhereLeftOffOutput string
	RecallOutput       string
	MaxCommitments     int
	MaxTaskLines       int
}

// AssembleContinuityPack builds a deterministic continuity block for prompt injection.
func AssembleContinuityPack(in ContinuityPackInput) string {
	mode := in.Mode
	if mode == "" {
		mode = ContinuityModeConversational
	}
	maxCommitments := in.MaxCommitments
	if maxCommitments <= 0 {
		maxCommitments = 4
	}
	maxTaskLines := in.MaxTaskLines
	if maxTaskLines <= 0 {
		maxTaskLines = 4
	}

	wlo := strings.TrimSpace(in.WhereLeftOffOutput)
	recall := strings.TrimSpace(in.RecallOutput)
	commitments := ExtractCommitments(maxCommitments, wlo, recall)
	if wlo == "" && recall == "" && len(commitments) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Continuity Pack\n\n")

	switch mode {
	case ContinuityModeDeterministic:
		taskLines := taskScopedLines(recall, in.UserMessage, maxTaskLines)
		if len(taskLines) > 0 {
			b.WriteString("### Task Scope\n")
			for _, line := range taskLines {
				b.WriteString("- ")
				b.WriteString(line)
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
	default:
		if wlo != "" {
			b.WriteString("### Recent Orientation\n")
			b.WriteString(wlo)
			b.WriteString("\n\n")
		}
		if recall != "" {
			b.WriteString("### Relevant Memory\n")
			b.WriteString(recall)
			b.WriteString("\n\n")
		}
	}

	if len(commitments) > 0 {
		b.WriteString("### Open Commitments\n")
		for _, c := range commitments {
			b.WriteString("- ")
			b.WriteString(c)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

var commitmentSignals = []string{
	"- [ ]",
	"todo",
	"pending",
	"next:",
	"follow up",
	"follow-up",
	"need to",
	"needs to",
	"action required",
	"will ",
}

// ExtractCommitments scans text blocks for likely commitments and returns unique lines.
func ExtractCommitments(limit int, blocks ...string) []string {
	if limit <= 0 {
		limit = 4
	}
	seen := map[string]bool{}
	out := make([]string, 0, limit)
	for _, block := range blocks {
		for _, raw := range strings.Split(block, "\n") {
			line := normalizeLine(raw)
			if line == "" {
				continue
			}
			lower := strings.ToLower(line)
			if !containsSignal(lower) {
				continue
			}
			key := strings.ToLower(line)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, line)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func containsSignal(s string) bool {
	for _, sig := range commitmentSignals {
		if strings.Contains(s, sig) {
			return true
		}
	}
	return false
}

func normalizeLine(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "- [ ]")
	s = strings.TrimPrefix(s, "- ")
	s = strings.TrimPrefix(s, "* ")
	s = strings.TrimSpace(s)
	if len(s) > 180 {
		return strings.TrimSpace(s[:177]) + "..."
	}
	return s
}

var keywordStopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "that": true, "this": true, "from": true, "into": true,
	"your": true, "about": true, "have": true, "what": true, "when": true, "where": true, "please": true, "need": true,
}

func taskScopedLines(recallOutput, userMsg string, maxLines int) []string {
	recallOutput = strings.TrimSpace(recallOutput)
	if recallOutput == "" {
		return nil
	}
	lines := strings.Split(recallOutput, "\n")
	keywords := userKeywords(userMsg)
	out := make([]string, 0, maxLines)
	for _, raw := range lines {
		line := normalizeLine(raw)
		if line == "" {
			continue
		}
		if len(keywords) == 0 || lineMatchesKeywords(line, keywords) {
			out = append(out, line)
		}
		if len(out) >= maxLines {
			return out
		}
	}
	// Fallback when nothing matched user terms: return first lines to avoid empty context.
	if len(out) == 0 {
		for _, raw := range lines {
			line := normalizeLine(raw)
			if line == "" {
				continue
			}
			out = append(out, line)
			if len(out) >= maxLines {
				break
			}
		}
	}
	return out
}

func lineMatchesKeywords(line string, keywords []string) bool {
	lower := strings.ToLower(line)
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func userKeywords(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	seen := map[string]bool{}
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) < 4 || keywordStopWords[f] {
			continue
		}
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}
