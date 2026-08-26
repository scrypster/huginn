package tools

import (
	"fmt"
	"os"
	"strings"
	"unicode"
)

// expandHomeInCommand expands unquoted ~ / ~/path and $HOME / ${HOME} using
// the process home (HOME, then USERPROFILE, then os.UserHomeDir). Quoted
// tildes are left alone, matching the shell. If the command needs a home
// path and none can be resolved, it returns a loud error — never an
// empty-success listing of a literal "~".
func expandHomeInCommand(command string) (string, error) {
	home, err := resolveHomeDir()
	if err != nil {
		home = ""
	}
	return expandHomeInCommandFrom(home, command)
}

func expandHomeInCommandFrom(home, command string) (string, error) {
	if !commandNeedsHomeExpansion(command) {
		return command, nil
	}
	if strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("bash: ~ / $HOME was not expanded (HOME is unset or empty)")
	}
	return expandHomeRefs(command, home), nil
}

func resolveHomeDir() (string, error) {
	if h := strings.TrimSpace(os.Getenv("HOME")); h != "" {
		return h, nil
	}
	if h := strings.TrimSpace(os.Getenv("USERPROFILE")); h != "" {
		return h, nil
	}
	return os.UserHomeDir()
}

func commandNeedsHomeExpansion(command string) bool {
	return scanHomeRefs(command, "", true)
}

func expandHomeRefs(command, home string) string {
	var b strings.Builder
	scanHomeRefs(command, home, false, &b)
	return b.String()
}

// scanHomeRefs walks command in a quote-aware way. When dryRun is true it
// returns whether any unquoted ~ / $HOME would be expanded. Otherwise it
// writes the expanded command into b (b must be non-nil).
func scanHomeRefs(command, home string, dryRun bool, b ...*strings.Builder) bool {
	var out *strings.Builder
	if !dryRun {
		if len(b) == 0 || b[0] == nil {
			return false
		}
		out = b[0]
	}
	write := func(s string) {
		if out != nil {
			out.WriteString(s)
		}
	}
	writeByte := func(c byte) {
		if out != nil {
			out.WriteByte(c)
		}
	}

	quote := byte(0)
	atWordStart := true
	needed := false
	i := 0
	for i < len(command) {
		c := command[i]
		if quote == '\'' {
			writeByte(c)
			if c == '\'' {
				quote = 0
			}
			i++
			atWordStart = false
			continue
		}
		if quote == '"' {
			if c == '\\' && i+1 < len(command) {
				writeByte(c)
				writeByte(command[i+1])
				i += 2
				atWordStart = false
				continue
			}
			if c == '"' {
				quote = 0
				writeByte(c)
				i++
				atWordStart = false
				continue
			}
			if n := homeVarLen(command[i:]); n > 0 {
				needed = true
				if dryRun {
					return true
				}
				write(home)
				i += n
				atWordStart = false
				continue
			}
			writeByte(c)
			i++
			atWordStart = false
			continue
		}

		// unquoted
		if c == '\'' || c == '"' {
			quote = c
			writeByte(c)
			i++
			atWordStart = false
			continue
		}
		if atWordStart && c == '~' && isTildeHome(command[i:]) {
			needed = true
			if dryRun {
				return true
			}
			write(home)
			i++
			atWordStart = false
			continue
		}
		if n := homeVarLen(command[i:]); n > 0 {
			needed = true
			if dryRun {
				return true
			}
			write(home)
			i += n
			atWordStart = false
			continue
		}
		writeByte(c)
		i++
		atWordStart = isHomeWordSep(c)
	}
	return needed
}

func isTildeHome(s string) bool {
	if s == "" || s[0] != '~' {
		return false
	}
	if len(s) == 1 {
		return true
	}
	next := s[1]
	return next == '/' || isHomeWordSep(next)
}

func isHomeWordSep(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '=' || c == ':'
}

func homeVarLen(s string) int {
	if strings.HasPrefix(s, "${HOME}") {
		return len("${HOME}")
	}
	if !strings.HasPrefix(s, "$HOME") {
		return 0
	}
	rest := s[len("$HOME"):]
	if rest == "" {
		return len("$HOME")
	}
	r := rune(rest[0])
	if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
		return 0
	}
	return len("$HOME")
}
