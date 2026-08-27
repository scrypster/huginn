package agent

import (
	"context"
	"regexp"
	"strings"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// Named hire is a 14b trap: Winston will interview a form or delegate to
// Reggie instead of calling create_agent. When the human already gave a
// name and a role, skip the model and persist the teammate.

var (
	namedHireRE = regexp.MustCompile(`(?i)\b(?:create(?:\s+an)?\s+agent|hire(?:\s+a)?(?:\s+teammate)?|add(?:\s+a)?\s+teammate|create(?:\s+a)?\s+teammate)\s+(?:named|called)\s+([A-Za-z][\w.-]{0,62})\s+(?:who|that|to)\s+(.+)$`)
	hireWhoRE   = regexp.MustCompile(`(?i)\b(?:hire|add)\s+([A-Za-z][\w.-]{0,62})\s+(?:who|that|to)\s+(.+)$`)
)

// IsHireAsk reports a hire / create-agent / add-teammate turn.
func IsHireAsk(s string) bool {
	return hireAskRE.MatchString(s)
}

// ParseNamedHire extracts name+role from "create an agent named X who Y"
// and "hire a teammate named X who Y". Ambiguous "hire someone" returns false.
func ParseNamedHire(s string) (name, role string, ok bool) {
	norm := strings.TrimSpace(mentionRE.ReplaceAllString(s, " "))
	norm = strings.Join(strings.Fields(norm), " ")
	if norm == "" {
		return "", "", false
	}
	if m := namedHireRE.FindStringSubmatch(norm); len(m) == 3 {
		return cleanHireName(m[1]), cleanHireRole(m[2]), cleanHireName(m[1]) != "" && cleanHireRole(m[2]) != ""
	}
	if m := hireWhoRE.FindStringSubmatch(norm); len(m) == 3 {
		n := cleanHireName(m[1])
		if n == "" || strings.EqualFold(n, "a") || strings.EqualFold(n, "an") || strings.EqualFold(n, "someone") || strings.EqualFold(n, "teammate") {
			return "", "", false
		}
		return n, cleanHireRole(m[2]), cleanHireRole(m[2]) != ""
	}
	return "", "", false
}

func cleanHireName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"'`")
	return s
}

func cleanHireRole(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, " \t.!?…")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 160 {
		s = strings.TrimSpace(s[:160])
	}
	return s
}

func agentHasCreateAgentGrant(ag *agents.Agent) bool {
	if ag == nil {
		return false
	}
	for _, n := range ag.LocalTools {
		if n == tools.CreateAgentName {
			return true
		}
	}
	return false
}

// stripHireDelegationTools keeps create_agent and drops the 14b hire
// handoff belt (delegate / wait / consult).
func stripHireDelegationTools(schemas []backend.Tool) []backend.Tool {
	return stripTrivialAskDelegationTools(schemas)
}

func (o *Orchestrator) tryNamedHireFastPath(ctx context.Context, ag *agents.Agent, userMsg string, sess *Session, reg *tools.Registry, onToken func(string), onEvent func(backend.StreamEvent)) bool {
	if o == nil || ag == nil || reg == nil || sess == nil {
		return false
	}
	name, role, ok := ParseNamedHire(userMsg)
	if !ok || !agentHasCreateAgentGrant(ag) {
		return false
	}
	tool, exists := reg.Get(tools.CreateAgentName)
	if !exists {
		return false
	}
	if onEvent != nil {
		onEvent(backend.StreamEvent{Type: backend.StreamStatus, Content: "hiring"})
	}
	first := "On it — adding " + name + "."
	if onToken != nil {
		onToken(first)
	}
	res := tool.Execute(ctx, map[string]any{
		"name":        name,
		"description": role,
		"memory":      false,
	})
	speech := strings.TrimSpace(res.Output)
	if res.IsError {
		speech = strings.TrimSpace(res.Error)
	}
	persist := first
	if speech != "" {
		persist = first + "\n" + speech
		if onToken != nil {
			onToken("\n" + speech)
		}
	}
	appendHistoryHonoringGate(sess, userMsg, persist, nil, false)
	o.compactHistory(ctx, sess)
	return true
}
