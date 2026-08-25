package claudecode

import "strings"

// AssembleSystemPrompt builds the text passed to `claude --append-system-prompt`
// for a Claude Code agent.
//
// It is rebuilt on every turn rather than at session creation, so edits to an
// agent's prompt, skills or notepad take effect on the next message instead of
// requiring a new session.
//
// It APPENDS to Claude Code's own system prompt rather than replacing it, so
// the CLI's built-in behaviour stays intact.
func AssembleSystemPrompt(agentPrompt string, skills []string, notepad string) string {
	var b strings.Builder

	if s := strings.TrimSpace(agentPrompt); s != "" {
		b.WriteString(s)
	}

	var kept []string
	for _, sk := range skills {
		if s := strings.TrimSpace(sk); s != "" {
			kept = append(kept, s)
		}
	}
	if len(kept) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("# Skills\n\n")
		b.WriteString(strings.Join(kept, "\n\n"))
	}

	if s := strings.TrimSpace(notepad); s != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("# Notepad\n\n")
		b.WriteString(s)
	}

	return b.String()
}
