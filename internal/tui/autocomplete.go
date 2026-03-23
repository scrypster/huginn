package tui

import (
	"strings"
	"unicode"
)

// ExtractAtPrefix returns the text typed after the last `@` that qualifies as
// an agent-mention trigger. A qualifying `@` must be:
//   - at the start of the input (index 0), OR
//   - immediately preceded by a whitespace character
//
// AND the text after `@` must be at least 2 characters long.
//
// Returns "" if no qualifying `@` is active (e.g. email addresses, too short).
func ExtractAtPrefix(input string) string {
	idx := strings.LastIndex(input, "@")
	if idx < 0 {
		return ""
	}
	// Must be at start or preceded by whitespace.
	if idx > 0 {
		prev := rune(input[idx-1])
		if !unicode.IsSpace(prev) {
			return ""
		}
	}
	prefix := input[idx+1:]
	if len(prefix) < 2 {
		return ""
	}
	return prefix
}

// FilterAgentNames returns all names from the provided slice that have the
// given prefix (case-insensitive). Returns all names if prefix is empty.
func FilterAgentNames(names []string, prefix string) []string {
	lower := strings.ToLower(prefix)
	var result []string
	for _, n := range names {
		if prefix == "" || strings.HasPrefix(strings.ToLower(n), lower) {
			result = append(result, n)
		}
	}
	return result
}
