package threadmgr

import "strings"

// DedupMentions removes @AgentName mentions from reply that already appeared
// in userMsg, preventing duplicate thread spawns when both the user and the
// assistant reference the same agent in the same exchange.
//
// The leading separator character (space or punctuation matched by MentionRe)
// is preserved — only the "@" prefix is stripped — so sentence structure remains
// intact and downstream @mention parsing does not fire on the stripped tokens.
//
// Matching is case-insensitive: @sam in userMsg will strip @Sam from reply.
func DedupMentions(userMsg, reply string) string {
	if reply == "" {
		return reply
	}
	excluded := make(map[string]bool)
	for _, m := range MentionRe.FindAllStringSubmatch(userMsg, -1) {
		if len(m) > 1 {
			excluded[strings.ToLower(m[1])] = true
		}
	}
	if len(excluded) == 0 {
		return reply
	}
	return MentionRe.ReplaceAllStringFunc(reply, func(match string) string {
		sub := MentionRe.FindStringSubmatch(match)
		if len(sub) > 1 && excluded[strings.ToLower(sub[1])] {
			// Strip just the "@Name" portion; keep the leading separator so
			// sentence structure is preserved and the resulting token is no
			// longer an @mention.
			return strings.Replace(match, "@"+sub[1], sub[1], 1)
		}
		return match
	})
}
