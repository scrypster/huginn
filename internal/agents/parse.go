package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/scrypster/huginn/internal/backend"
)

// Directive represents one agent action extracted from user input.
type Directive struct {
	AgentName string
	Action    string // "plan", "code", "reason", "chat"
	Payload   string // the task/question content
}

// ChainedDirective is a sequence of Directives to execute in order.
type ChainedDirective struct {
	Steps []Directive
}

// actionAliases maps natural language action words to canonical actions.
var actionAliases = map[string]string{
	"plan":      "plan",
	"code":      "code",
	"implement": "code",
	"refactor":  "code",
	"write":     "code",
	"build":     "code",
	"review":    "reason",
	"reason":    "reason",
	"check":     "reason",
	"analyze":   "reason",
	"analyse":   "reason",
	"think":     "reason",
	"evaluate":  "reason",
	"assess":    "reason",
	"verify":    "reason",
}

// singlePattern matches: "Have/Ask/Tell/Get <Name> [to] <action> <payload>"
var singlePattern = regexp.MustCompile(
	`(?i)^(?:have|ask|tell|get)\s+(\w+)\s+(?:to\s+)?(\w+)\s+(.+)$`,
)

// chainPattern matches: "<single> then <single>"
var chainPattern = regexp.MustCompile(
	`(?i)^(?:have|ask|tell|get)\s+(\w+)\s+(?:to\s+)?(\w+)\s+(.+?)\s+(?:then|and then|,\s*then)\s+(?:have|ask|tell|get)\s+(\w+)\s+(?:to\s+)?(\w+)\s+(.+)$`,
)

// ParseDirective parses a natural language agent directive.
// Returns nil if the input is not a recognized directive.
func ParseDirective(input string, registry *AgentRegistry) *ChainedDirective {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}

	// Try chained pattern first (more specific)
	if m := chainPattern.FindStringSubmatch(input); m != nil {
		agent1, ok1 := registry.ByName(m[1])
		agent2, ok2 := registry.ByName(m[4])
		if ok1 && ok2 {
			return &ChainedDirective{
				Steps: []Directive{
					{AgentName: agent1.Name, Action: normalizeAction(m[2]), Payload: strings.TrimSpace(m[3])},
					{AgentName: agent2.Name, Action: normalizeAction(m[5]), Payload: strings.TrimSpace(m[6])},
				},
			}
		}
	}

	// Try single pattern
	if m := singlePattern.FindStringSubmatch(input); m != nil {
		agent, ok := registry.ByName(m[1])
		if ok {
			return &ChainedDirective{
				Steps: []Directive{
					{AgentName: agent.Name, Action: normalizeAction(m[2]), Payload: strings.TrimSpace(m[3])},
				},
			}
		}
	}

	return nil
}

// ContainsAgentName returns true if the input contains any registered agent name.
func ContainsAgentName(input string, registry *AgentRegistry) bool {
	lower := strings.ToLower(input)
	for _, name := range registry.Names() {
		if strings.Contains(lower, strings.ToLower(name)) {
			return true
		}
	}
	return false
}

// normalizeAction maps raw action words to canonical actions.
func normalizeAction(raw string) string {
	lower := strings.ToLower(strings.TrimSpace(raw))
	if action, ok := actionAliases[lower]; ok {
		return action
	}
	return "chat"
}

// ParseDirectiveFallback fires a single structured LLM call to extract an agent directive
// when Tier 1 regex parsing (ParseDirective) fails but an agent name was detected.
// Returns nil if the backend is nil, the call fails, the response is malformed, or the
// agent name is not registered. Fires rarely — only when ContainsAgentName is true but
// ParseDirective returns nil.
func ParseDirectiveFallback(ctx context.Context, input string, registry *AgentRegistry, b backend.Backend, plannerModelID string) *ChainedDirective {
	if b == nil {
		return nil
	}

	names := strings.Join(registry.Names(), ", ")
	prompt := fmt.Sprintf(
		"Extract the agent directive from this input. Return JSON only, no prose.\n"+
			"Registered agents (use exact name): %s\n"+
			`{"agent": "<name>", "action": "plan|code|reason|chat", "payload": "<task>"}`+"\n"+
			"Input: %q", names, input)

	var buf strings.Builder
	_, err := b.ChatCompletion(ctx, backend.ChatRequest{
		Model: plannerModelID,
		Messages: []backend.Message{
			{Role: "user", Content: prompt},
		},
		OnToken: func(token string) { buf.WriteString(token) },
	})
	if err != nil {
		return nil
	}

	var result struct {
		Agent   string `json:"agent"`
		Action  string `json:"action"`
		Payload string `json:"payload"`
	}
	raw := strings.TrimSpace(buf.String())
	// Strip optional markdown code fences emitted by local models (e.g. ```json\n{...}\n```).
	if i := strings.Index(raw, "{"); i > 0 {
		raw = raw[i:]
	}
	if i := strings.LastIndex(raw, "}"); i >= 0 && i < len(raw)-1 {
		raw = raw[:i+1]
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil
	}

	ag, ok := registry.ByName(result.Agent)
	if !ok {
		return nil
	}

	validActions := map[string]bool{"plan": true, "code": true, "reason": true, "chat": true}
	if !validActions[result.Action] {
		return nil
	}

	return &ChainedDirective{
		Steps: []Directive{{
			AgentName: ag.Name,
			Action:    result.Action,
			Payload:   result.Payload,
		}},
	}
}
