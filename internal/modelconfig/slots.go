package modelconfig

import (
	"fmt"
	"regexp"
	"strings"
)

// Models holds the active fallback model name used when no named agent is selected.
type Models struct {
	Reasoner string
}

// DefaultModels returns production defaults.
func DefaultModels() *Models {
	return &Models{
		Reasoner: "qwen2.5-coder:14b",
	}
}

var nlPattern = regexp.MustCompile(
	`(?i)use\s+(\S+)\s+for\s+(reasoning|reasoner)`,
)

// ParseModelCommand parses "use <model> for reasoning" and returns the model name.
func ParseModelCommand(input string) (string, error) {
	m := nlPattern.FindStringSubmatch(input)
	if m == nil {
		return "", fmt.Errorf("cannot parse model command: %q", input)
	}
	return strings.TrimSpace(m[1]), nil
}
